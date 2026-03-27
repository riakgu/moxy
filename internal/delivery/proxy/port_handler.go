package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/gateway/netns"
	"github.com/riakgu/moxy/internal/usecase"
)

// PortBasedHandler manages per-slot SOCKS5 listeners on sequential ports.
// Port portBase+0 → slot0, portBase+1 → slot1, etc.
// No authentication required — the port number determines the slot.
type PortBasedHandler struct {
	Log         *logrus.Logger
	ProxyUC     *usecase.ProxyUseCase
	sem         chan struct{}
	idleTimeout time.Duration
	portBase    int
	mu          sync.Mutex
	listeners   map[string]*portListener
}

type portListener struct {
	slotName string
	port     int
	ln       net.Listener
	wg       sync.WaitGroup
	closeCh  chan struct{}
}

func NewPortBasedHandler(log *logrus.Logger, proxyUC *usecase.ProxyUseCase, sem chan struct{}, idleTimeout time.Duration, portBase int) *PortBasedHandler {
	return &PortBasedHandler{
		Log:         log,
		ProxyUC:     proxyUC,
		sem:         sem,
		idleTimeout: idleTimeout,
		portBase:    portBase,
		listeners:   make(map[string]*portListener),
	}
}

// SyncSlots starts/stops port listeners to match the current set of slots.
// Call this after slot discovery.
func (h *PortBasedHandler) SyncSlots(slotNames []string) {
	if h.portBase <= 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Build set of desired slots
	desired := make(map[string]bool)
	for _, name := range slotNames {
		desired[name] = true
	}

	// Stop listeners for removed slots
	for name, pl := range h.listeners {
		if !desired[name] {
			h.Log.Infof("port-based: stopping listener for %s on port %d", name, pl.port)
			close(pl.closeCh)
			pl.ln.Close()
			delete(h.listeners, name)
		}
	}

	// Start listeners for new slots
	for _, name := range slotNames {
		if _, exists := h.listeners[name]; exists {
			continue
		}

		slotIndex := extractSlotIndex(name)
		if slotIndex < 0 {
			continue
		}

		port := h.portBase + slotIndex
		pl := &portListener{
			slotName: name,
			port:     port,
			closeCh:  make(chan struct{}),
		}

		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			h.Log.WithError(err).Warnf("port-based: failed to listen on port %d for %s", port, name)
			continue
		}
		pl.ln = ln

		h.listeners[name] = pl
		h.Log.Infof("port-based: %s → port %d", name, port)

		go h.acceptLoop(pl)
	}
}

// GetPortMappings returns current port → slot mappings for the dashboard.
func (h *PortBasedHandler) GetPortMappings() map[string]int {
	h.mu.Lock()
	defer h.mu.Unlock()

	mappings := make(map[string]int, len(h.listeners))
	for name, pl := range h.listeners {
		mappings[name] = pl.port
	}
	return mappings
}

func (h *PortBasedHandler) acceptLoop(pl *portListener) {
	for {
		conn, err := pl.ln.Accept()
		if err != nil {
			select {
			case <-pl.closeCh:
				return
			default:
				h.Log.WithError(err).Debug("port-based: accept error")
				continue
			}
		}

		select {
		case h.sem <- struct{}{}:
			pl.wg.Add(1)
			go func() {
				defer pl.wg.Done()
				defer func() { <-h.sem }()
				h.handleConnection(conn, pl.slotName)
			}()
		default:
			conn.Close()
		}
	}
}

// handleConnection does a minimal SOCKS5 handshake with NO AUTH, then relays.
func (h *PortBasedHandler) handleConnection(conn net.Conn, slotName string) {
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// SOCKS5 greeting
	buf := make([]byte, 258)
	if _, err := conn.Read(buf[:2]); err != nil || buf[0] != 0x05 {
		return
	}
	nmethods := int(buf[1])
	if _, err := conn.Read(buf[:nmethods]); err != nil {
		return
	}

	// Reply: no auth required
	conn.Write([]byte{0x05, 0x00})

	// SOCKS5 request
	if _, err := conn.Read(buf[:4]); err != nil {
		return
	}
	if buf[1] != 0x01 { // only CONNECT
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var targetAddr string
	switch buf[3] {
	case 0x01: // IPv4
		if _, err := conn.Read(buf[:6]); err != nil {
			return
		}
		ip := net.IP(buf[:4])
		port := binary.BigEndian.Uint16(buf[4:6])
		targetAddr = net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
	case 0x03: // Domain
		if _, err := conn.Read(buf[:1]); err != nil {
			return
		}
		domainLen := int(buf[0])
		if _, err := conn.Read(buf[:domainLen+2]); err != nil {
			return
		}
		domain := string(buf[:domainLen])
		port := binary.BigEndian.Uint16(buf[domainLen : domainLen+2])
		targetAddr = net.JoinHostPort(domain, strconv.Itoa(int(port)))
	case 0x04: // IPv6
		if _, err := conn.Read(buf[:18]); err != nil {
			return
		}
		ip := net.IP(buf[:16])
		port := binary.BigEndian.Uint16(buf[16:18])
		targetAddr = net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	// Dial via the assigned slot
	remote, err := h.ProxyUC.Connect(slotName, targetAddr)
	if err != nil {
		h.Log.WithError(err).Warnf("port-based dial failed: %s via %s", targetAddr, slotName)
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()

	// Success reply
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// Clear deadline before bridge
	conn.SetDeadline(time.Time{})

	// Bridge
	sent, received := netns.BridgeWithTimeout(conn, remote, h.idleTimeout)
	h.ProxyUC.AddTraffic(slotName, sent, received)
	h.ProxyUC.RecordDestination(targetAddr, sent, received)
}

// Shutdown stops all port-based listeners and drains connections.
func (h *PortBasedHandler) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	for _, pl := range h.listeners {
		close(pl.closeCh)
		pl.ln.Close()
	}
	h.mu.Unlock()

	// Wait for all connections to drain
	done := make(chan struct{})
	go func() {
		h.mu.Lock()
		for _, pl := range h.listeners {
			pl.wg.Wait()
		}
		h.mu.Unlock()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// extractSlotIndex parses "slot0" → 0, "slot5" → 5, etc.
func extractSlotIndex(name string) int {
	if len(name) < 5 || name[:4] != "slot" {
		return -1
	}
	idx, err := strconv.Atoi(name[4:])
	if err != nil {
		return -1
	}
	return idx
}
