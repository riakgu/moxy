package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
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
	portStart   int
	portEnd     int
	mu          sync.Mutex
	listeners   map[string]*portListener
}

type portListener struct {
	slotName string
	port     int
	ln       net.Listener
	wg       sync.WaitGroup
	closeCh  chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewPortBasedHandler(log *logrus.Logger, proxyUC *usecase.ProxyUseCase, sem chan struct{}, idleTimeout time.Duration, portStart, portEnd int) *PortBasedHandler {
	return &PortBasedHandler{
		Log:         log,
		ProxyUC:     proxyUC,
		sem:         sem,
		idleTimeout: idleTimeout,
		portStart:   portStart,
		portEnd:     portEnd,
		listeners:   make(map[string]*portListener),
	}
}

// SyncSlots starts/stops port listeners to match the current set of slots.
// Call this after slot discovery.
func (h *PortBasedHandler) SyncSlots(slotNames []string) {
	if h.portStart <= 0 {
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
			pl.cancel()
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

		port := h.portStart + slotIndex
		if h.portEnd > 0 && port > h.portEnd {
			h.Log.Warnf("port-based: %s skipped — port %d exceeds range end %d", name, port, h.portEnd)
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		pl := &portListener{
			slotName: name,
			port:     port,
			closeCh:  make(chan struct{}),
			ctx:      ctx,
			cancel:   cancel,
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
				h.handleConnection(conn, pl)
			}()
		default:
			conn.Close()
		}
	}
}

// handleConnection detects the protocol (SOCKS5 or HTTP) and dispatches.
// No authentication — the port number determines the slot.
func (h *PortBasedHandler) handleConnection(conn net.Conn, pl *portListener) {
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Peek first byte to detect protocol
	reader := bufio.NewReader(conn)
	firstByte, err := reader.Peek(1)
	if err != nil {
		return
	}

	if firstByte[0] == 0x05 {
		h.handleSocks5(conn, reader, pl)
	} else {
		h.handleHTTP(conn, reader, pl)
	}
}

// handleSocks5 handles SOCKS5 connections with no authentication.
func (h *PortBasedHandler) handleSocks5(conn net.Conn, reader *bufio.Reader, pl *portListener) {
	slotName := pl.slotName
	buf := make([]byte, 258)

	// 1. Greeting: version + auth methods
	n, err := reader.Read(buf)
	if err != nil || n < 3 || buf[0] != 0x05 {
		return
	}

	// Accept no-auth (0x00)
	conn.Write([]byte{0x05, 0x00})

	// 2. CONNECT request
	n, err = reader.Read(buf)
	if err != nil || n < 7 || buf[0] != 0x05 || buf[1] != 0x01 {
		if n >= 4 {
			conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		}
		return
	}

	// Parse target address
	var targetAddr string
	switch buf[3] {
	case 0x01: // IPv4
		if n < 10 {
			return
		}
		ip := net.IP(buf[4:8])
		port := binary.BigEndian.Uint16(buf[8:10])
		targetAddr = net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
	case 0x03: // Domain
		domainLen := int(buf[4])
		if 5+domainLen+2 > n {
			return
		}
		domain := string(buf[5 : 5+domainLen])
		port := binary.BigEndian.Uint16(buf[5+domainLen : 5+domainLen+2])
		targetAddr = net.JoinHostPort(domain, strconv.Itoa(int(port)))
	case 0x04: // IPv6
		if n < 22 {
			return
		}
		ip := net.IP(buf[4:20])
		port := binary.BigEndian.Uint16(buf[20:22])
		targetAddr = net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	// 3. Dial target via namespace
	remote, err := h.ProxyUC.Connect(slotName, targetAddr)
	if err != nil {
		h.Log.WithError(err).Warnf("port-based socks5 dial failed: %s via %s", targetAddr, slotName)
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()

	// 4. Success reply
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	conn.SetDeadline(time.Time{})

	// 5. Bridge
	netns.BridgeWithTimeout(pl.ctx, conn, remote, h.idleTimeout)
}

// handleHTTP handles HTTP proxy requests (CONNECT + plain HTTP).
func (h *PortBasedHandler) handleHTTP(conn net.Conn, reader *bufio.Reader, pl *portListener) {
	slotName := pl.slotName

	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	if req.Method == http.MethodConnect {
		targetAddr := req.Host
		if !strings.Contains(targetAddr, ":") {
			targetAddr = targetAddr + ":443"
		}

		remote, err := h.ProxyUC.Connect(slotName, targetAddr)
		if err != nil {
			h.Log.WithError(err).Warnf("port-based CONNECT failed: %s via %s", targetAddr, slotName)
			conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			return
		}
		defer remote.Close()

		conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		conn.SetDeadline(time.Time{})

		netns.BridgeWithTimeout(pl.ctx, conn, remote, h.idleTimeout)
	} else {
		targetAddr := req.Host
		if !strings.Contains(targetAddr, ":") {
			targetAddr = targetAddr + ":80"
		}

		remote, err := h.ProxyUC.Connect(slotName, targetAddr)
		if err != nil {
			h.Log.WithError(err).Warnf("port-based HTTP failed: %s via %s", targetAddr, slotName)
			conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			return
		}
		defer remote.Close()

		req.Header.Del("Proxy-Connection")
		req.RequestURI = ""
		req.Write(remote)

		conn.SetDeadline(time.Time{})

		netns.BridgeWithTimeout(pl.ctx, conn, remote, h.idleTimeout)
	}
}

// Shutdown stops all port-based listeners and drains connections.
func (h *PortBasedHandler) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	for _, pl := range h.listeners {
		pl.cancel()
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

// extractSlotIndex parses "dev1_slot0" → 0, "dev2_slot5" → 5, etc.
// Also handles legacy format "slot0" → 0.
func extractSlotIndex(name string) int {
	// Find "_slot" for device-prefixed names like "dev1_slot3"
	idx := strings.LastIndex(name, "_slot")
	if idx >= 0 {
		n, err := strconv.Atoi(name[idx+5:])
		if err != nil {
			return -1
		}
		return n
	}
	// Legacy format "slot0"
	if len(name) >= 5 && name[:4] == "slot" {
		n, err := strconv.Atoi(name[4:])
		if err != nil {
			return -1
		}
		return n
	}
	return -1
}
