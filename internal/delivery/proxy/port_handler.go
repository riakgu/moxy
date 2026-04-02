//go:build linux

package proxy

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/usecase"
)

// PortBasedHandler manages per-slot proxy listeners on sequential ports.
// Each slot gets two ports: one SOCKS5 (via Socks5Handler) and one HTTP (via HttpProxyHandler).
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

// portSlot holds the per-slot handler instances.
type portSlot struct {
	slotName string
	socks5   *Socks5Handler
	http     *HttpProxyHandler
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

// startSlot creates and starts SOCKS5 + HTTP handlers for a single slot.
func (c *PortBasedHandler) startSlot(slotName string, slotIndex int) *portSlot {
	// Fixed-slot connect function — always routes through this specific slot
	connect := func(ctx context.Context, addr string) (net.Conn, error) {
		return c.proxyUC.Connect(slotName, addr)
	}

	ps := &portSlot{slotName: slotName}
	started := false

	// SOCKS5 handler
	if c.socks5Start > 0 {
		port := c.socks5Start + slotIndex
		addr := fmt.Sprintf(":%d", port)

		handler := NewSocks5Handler(c.Log, connect, c.sem)
		go func() {
			if err := handler.ListenAndServe(addr); err != nil {
				c.Log.WithError(err).Warnf("port-based: SOCKS5 failed on port %d for %s", port, slotName)
			}
		}()

		ps.socks5 = handler
		c.Log.Infof("port-based: %s → SOCKS5 port %d", slotName, port)
		started = true
	}

	// HTTP handler
	if c.httpStart > 0 {
		port := c.httpStart + slotIndex
		addr := fmt.Sprintf(":%d", port)

		handler := NewHttpProxyHandler(c.Log, connect, c.sem)
		go func() {
			if err := handler.ListenAndServe(addr); err != nil {
				c.Log.WithError(err).Warnf("port-based: HTTP failed on port %d for %s", port, slotName)
			}
		}()

		ps.http = handler
		c.Log.Infof("port-based: %s → HTTP port %d", slotName, port)
		started = true
	}

	if !started {
		return nil
	}
	return ps
}

// stopSlot shuts down both handlers for a slot.
func (c *PortBasedHandler) stopSlot(ps *portSlot) {
	ctx := context.Background()
	if ps.socks5 != nil {
		ps.socks5.Shutdown(ctx)
	}
	if ps.http != nil {
		ps.http.Shutdown(ctx)
	}
}

// GetPortMappings returns current slot → port mappings for the dashboard.
func (c *PortBasedHandler) GetPortMappings() map[string]map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()

	mappings := make(map[string]map[string]int, len(c.slots))
	for name := range c.slots {
		slotIndex := extractSlotIndex(name)
		if slotIndex < 0 {
			continue
		}
		m := make(map[string]int)
		if c.socks5Start > 0 {
			m["socks5"] = c.socks5Start + slotIndex
		}
		if c.httpStart > 0 {
			m["http"] = c.httpStart + slotIndex
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
