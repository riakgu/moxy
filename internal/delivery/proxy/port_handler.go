package proxy

import (
	"bufio"
	"context"
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

// handleConnection handles HTTP proxy requests (CONNECT + plain HTTP).
// No authentication — the port number determines the slot.
func (h *PortBasedHandler) handleConnection(conn net.Conn, pl *portListener) {
	defer conn.Close()
	slotName := pl.slotName

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	if req.Method == http.MethodConnect {
		// HTTPS tunneling
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

		sent, received := netns.BridgeWithTimeout(pl.ctx, conn, remote, h.idleTimeout)
		h.ProxyUC.AddTraffic(slotName, sent, received)
		h.ProxyUC.RecordDestination(targetAddr, sent, received)
	} else {
		// Plain HTTP forwarding
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

		// Forward the original request
		req.Header.Del("Proxy-Connection")
		req.RequestURI = ""
		req.Write(remote)

		conn.SetDeadline(time.Time{})

		// Relay response back
		sent, received := netns.BridgeWithTimeout(pl.ctx, conn, remote, h.idleTimeout)
		h.ProxyUC.AddTraffic(slotName, sent, received)
		h.ProxyUC.RecordDestination(targetAddr, sent, received)
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
