//go:build linux

package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/elazarl/goproxy"
	"github.com/sirupsen/logrus"
	"github.com/things-go/go-socks5"

	"github.com/riakgu/moxy/internal/usecase"
)

// PortBasedHandler manages per-slot proxy listeners on sequential ports.
// Each slot gets two ports: one SOCKS5 (via go-socks5) and one HTTP (via goproxy).
// No authentication required — the port number determines the slot.
type PortBasedHandler struct {
	Log         *logrus.Logger
	proxyUC     *usecase.ProxyUseCase
	sem         chan struct{}
	socks5Start int
	httpStart   int
	mu          sync.Mutex
	slots       map[string]*portSlot
}

// portSlot holds the per-slot SOCKS5 and HTTP servers.
type portSlot struct {
	slotName     string
	socks5Port   int
	httpPort     int
	socks5Server *socks5.Server
	socks5Ln     net.Listener
	socks5Wg     sync.WaitGroup
	socks5Ctx    context.Context
	socks5Cancel context.CancelFunc
	httpServer   *http.Server
	httpLn       net.Listener
}

// NewPortBasedHandler creates a new port-based handler.
func NewPortBasedHandler(
	log *logrus.Logger,
	proxyUC *usecase.ProxyUseCase,
	sem chan struct{},
	socks5Start int,
	httpStart int,
) *PortBasedHandler {
	return &PortBasedHandler{
		Log:         log,
		proxyUC:     proxyUC,
		sem:         sem,
		socks5Start: socks5Start,
		httpStart:   httpStart,
		slots:       make(map[string]*portSlot),
	}
}

// SyncSlots starts/stops port listeners to match the current set of slots.
func (c *PortBasedHandler) SyncSlots(slotNames []string) {
	if c.socks5Start <= 0 && c.httpStart <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Build set of desired slots
	desired := make(map[string]bool)
	for _, name := range slotNames {
		desired[name] = true
	}

	// Stop listeners for removed slots
	for name, ps := range c.slots {
		if !desired[name] {
			c.Log.Infof("port-based: stopping listeners for %s", name)
			c.stopSlot(ps)
			delete(c.slots, name)
		}
	}

	// Start listeners for new slots
	for _, name := range slotNames {
		if _, exists := c.slots[name]; exists {
			continue
		}

		slotIndex := extractSlotIndex(name)
		if slotIndex < 0 {
			continue
		}

		ps := c.startSlot(name, slotIndex)
		if ps != nil {
			c.slots[name] = ps
		}
	}
}

// startSlot creates and starts SOCKS5 + HTTP servers for a single slot.
func (c *PortBasedHandler) startSlot(slotName string, slotIndex int) *portSlot {
	ps := &portSlot{slotName: slotName}

	// SOCKS5 server
	if c.socks5Start > 0 {
		ps.socks5Port = c.socks5Start + slotIndex
		socks5Ctx, socks5Cancel := context.WithCancel(context.Background())
		ps.socks5Ctx = socks5Ctx
		ps.socks5Cancel = socks5Cancel

		ps.socks5Server = socks5.NewServer(
			socks5.WithDial(func(ctx context.Context, network, addr string) (net.Conn, error) {
				return c.proxyUC.Connect(slotName, addr)
			}),
			socks5.WithAuthMethods([]socks5.Authenticator{
				socks5.NoAuthAuthenticator{},
			}),
			socks5.WithLogger(nopLogger{}),
		)

		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", ps.socks5Port))
		if err != nil {
			c.Log.WithError(err).Warnf("port-based: failed to listen SOCKS5 on port %d for %s", ps.socks5Port, slotName)
		} else {
			ps.socks5Ln = ln
			c.Log.Infof("port-based: %s → SOCKS5 port %d", slotName, ps.socks5Port)
			go c.socks5AcceptLoop(ps)
		}
	}

	// HTTP server (goproxy)
	if c.httpStart > 0 {
		ps.httpPort = c.httpStart + slotIndex

		proxy := goproxy.NewProxyHttpServer()
		proxy.Verbose = false

		proxy.ConnectDial = func(network, addr string) (net.Conn, error) {
			return c.proxyUC.Connect(slotName, addr)
		}
		proxy.Tr = &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return c.proxyUC.Connect(slotName, addr)
			},
		}

		// Concurrency gate
		proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
			select {
			case c.sem <- struct{}{}:
				ctx.UserData = c.sem
				return req, nil
			default:
				return req, goproxy.NewResponse(req, goproxy.ContentTypeText, http.StatusServiceUnavailable, "Service Unavailable")
			}
		})
		proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
			if ctx.UserData != nil {
				<-c.sem
			}
			return resp
		})

		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", ps.httpPort))
		if err != nil {
			c.Log.WithError(err).Warnf("port-based: failed to listen HTTP on port %d for %s", ps.httpPort, slotName)
		} else {
			ps.httpLn = ln
			ps.httpServer = &http.Server{Handler: proxy}
			c.Log.Infof("port-based: %s → HTTP port %d", slotName, ps.httpPort)
			go func() {
				if err := ps.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
					c.Log.WithError(err).Debugf("port-based: HTTP server ended for %s", slotName)
				}
			}()
		}
	}

	// At least one listener must have started
	if ps.socks5Ln == nil && ps.httpLn == nil {
		return nil
	}
	return ps
}

// socks5AcceptLoop runs the accept loop for a SOCKS5 port with concurrency control.
func (c *PortBasedHandler) socks5AcceptLoop(ps *portSlot) {
	for {
		conn, err := ps.socks5Ln.Accept()
		if err != nil {
			select {
			case <-ps.socks5Ctx.Done():
				return
			default:
			}
			continue
		}

		select {
		case c.sem <- struct{}{}:
			ps.socks5Wg.Add(1)
			go func() {
				defer ps.socks5Wg.Done()
				defer func() { <-c.sem }()
				if err := ps.socks5Server.ServeConn(conn); err != nil {
					c.Log.WithError(err).Debug("port-based: SOCKS5 connection ended with error")
				}
			}()
		default:
			conn.Close()
		}
	}
}

// stopSlot shuts down both servers for a slot.
func (c *PortBasedHandler) stopSlot(ps *portSlot) {
	// Stop SOCKS5
	if ps.socks5Cancel != nil {
		ps.socks5Cancel()
	}
	if ps.socks5Ln != nil {
		ps.socks5Ln.Close()
		ps.socks5Wg.Wait()
	}

	// Stop HTTP
	if ps.httpServer != nil {
		ps.httpServer.Close()
	}
}

// GetPortMappings returns current slot → port mappings for the dashboard.
func (c *PortBasedHandler) GetPortMappings() map[string]map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()

	mappings := make(map[string]map[string]int, len(c.slots))
	for name, ps := range c.slots {
		m := make(map[string]int)
		if ps.socks5Ln != nil {
			m["socks5"] = ps.socks5Port
		}
		if ps.httpLn != nil {
			m["http"] = ps.httpPort
		}
		mappings[name] = m
	}
	return mappings
}

// Shutdown stops all port-based listeners and drains connections.
func (c *PortBasedHandler) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	for _, ps := range c.slots {
		c.stopSlot(ps)
	}
	c.mu.Unlock()
	return nil
}

// extractSlotIndex parses "dev1_slot0" → 0, "dev2_slot5" → 5, etc.
func extractSlotIndex(name string) int {
	idx := strings.LastIndex(name, "_slot")
	if idx >= 0 {
		n, err := strconv.Atoi(name[idx+5:])
		if err != nil {
			return -1
		}
		return n
	}
	if len(name) >= 5 && name[:4] == "slot" {
		n, err := strconv.Atoi(name[4:])
		if err != nil {
			return -1
		}
		return n
	}
	return -1
}
