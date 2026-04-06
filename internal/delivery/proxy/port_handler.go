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

// PortBasedHandler manages three tiers of mux proxy listeners:
// - Shared (load-balanced across all devices)
// - Device-level (load-balanced across one device's slots)
// - Per-slot (specific IP)
type PortBasedHandler struct {
	Log           *logrus.Logger
	proxyUC       *usecase.ProxyUseCase
	proxyPort     int // shared + device-level base port
	slotStart     int // per-slot base port
	ipv6Port      int // IPv6 shared + device-level base port
	ipv6SlotStart int // IPv6 per-slot base port
	mu            sync.Mutex
	shared        *MuxHandler
	devices       map[string]*MuxHandler // alias → handler
	slots         map[string]*MuxHandler // slotName → handler
	ipv6Shared    *MuxHandler
	ipv6Devices   map[string]*MuxHandler
	ipv6Slots     map[string]*MuxHandler
}

// NewPortBasedHandler creates a new port-based handler.
func NewPortBasedHandler(
	log *logrus.Logger,
	proxyUC *usecase.ProxyUseCase,
	proxyPort int,
	slotStart int,
	ipv6Port int,
	ipv6SlotStart int,
) *PortBasedHandler {
	return &PortBasedHandler{
		Log:           log,
		proxyUC:       proxyUC,
		proxyPort:     proxyPort,
		slotStart:     slotStart,
		ipv6Port:      ipv6Port,
		ipv6SlotStart: ipv6SlotStart,
		devices:       make(map[string]*MuxHandler),
		slots:         make(map[string]*MuxHandler),
		ipv6Devices:   make(map[string]*MuxHandler),
		ipv6Slots:     make(map[string]*MuxHandler),
	}
}

// StartShared starts the shared proxy port (load-balanced across all devices).
func (c *PortBasedHandler) StartShared() {
	if c.proxyPort <= 0 {
		return
	}
	addr := fmt.Sprintf(":%d", c.proxyPort)
	connect := func(ctx context.Context, targetAddr string) (net.Conn, error) {
		slots := c.proxyUC.SlotRepo.ListHealthy()
		slot, err := c.proxyUC.SelectSlot(slots)
		if err != nil {
			return nil, err
		}
		return c.proxyUC.Connect(slot.Name, targetAddr)
	}
	c.shared = NewMuxHandler(c.Log, connect)
	if err := c.shared.Listen(addr); err != nil {
		c.Log.WithError(err).Errorf("shared proxy failed to bind %s", addr)
		return
	}
	go func() {
		if err := c.shared.Serve(); err != nil {
			c.Log.WithError(err).Errorf("shared proxy serve failed on %s", addr)
		}
	}()
}

// StartSharedIPv6 starts the shared IPv6-preferred proxy port.
func (c *PortBasedHandler) StartSharedIPv6() {
	if c.ipv6Port <= 0 {
		return
	}
	addr := fmt.Sprintf(":%d", c.ipv6Port)
	connect := func(ctx context.Context, targetAddr string) (net.Conn, error) {
		slots := c.proxyUC.SlotRepo.ListHealthy()
		slot, err := c.proxyUC.SelectSlot(slots)
		if err != nil {
			return nil, err
		}
		return c.proxyUC.ConnectIPv6(slot.Name, targetAddr)
	}
	c.ipv6Shared = NewMuxHandler(c.Log, connect)
	if err := c.ipv6Shared.Listen(addr); err != nil {
		c.Log.WithError(err).Errorf("shared IPv6 proxy failed to bind %s", addr)
		return
	}
	go func() {
		if err := c.ipv6Shared.Serve(); err != nil {
			c.Log.WithError(err).Errorf("shared IPv6 proxy serve failed on %s", addr)
		}
	}()
}

// SyncDevices starts/stops device-level mux handlers.
func (c *PortBasedHandler) SyncDevices(aliases []string) {
	if c.proxyPort <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	desired := make(map[string]bool)
	for _, alias := range aliases {
		desired[alias] = true
	}

	// Stop removed devices
	for alias, handler := range c.devices {
		if !desired[alias] {
			c.Log.Infof("stopping device proxy for %s", alias)
			handler.Shutdown(context.Background())
			delete(c.devices, alias)
		}
	}

	// Start new devices
	for _, alias := range aliases {
		if _, exists := c.devices[alias]; exists {
			continue
		}
		devIdx := deviceIndex(alias)
		if devIdx <= 0 {
			continue
		}
		port := c.proxyPort + devIdx
		addr := fmt.Sprintf(":%d", port)

		deviceAlias := alias // capture for closure
		connect := func(ctx context.Context, targetAddr string) (net.Conn, error) {
			slots := c.proxyUC.SlotRepo.ListHealthyForDevice(deviceAlias)
			slot, err := c.proxyUC.SelectSlot(slots)
			if err != nil {
				return nil, err
			}
			return c.proxyUC.Connect(slot.Name, targetAddr)
		}

		handler := NewMuxHandler(c.Log, connect)
		if err := handler.Listen(addr); err != nil {
			c.Log.WithError(err).Warnf("device proxy for %s failed to bind %s", deviceAlias, addr)
			continue
		}
		go func() {
			if err := handler.Serve(); err != nil {
				c.Log.WithError(err).Warnf("device proxy for %s serve failed on %s", deviceAlias, addr)
			}
		}()

		c.devices[alias] = handler
		c.Log.Infof("device proxy: %s → port %d", alias, port)
	}
}

// SyncDevicesIPv6 starts/stops device-level IPv6 mux handlers.
func (c *PortBasedHandler) SyncDevicesIPv6(aliases []string) {
	if c.ipv6Port <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	desired := make(map[string]bool)
	for _, alias := range aliases {
		desired[alias] = true
	}

	for alias, handler := range c.ipv6Devices {
		if !desired[alias] {
			c.Log.Infof("stopping IPv6 device proxy for %s", alias)
			handler.Shutdown(context.Background())
			delete(c.ipv6Devices, alias)
		}
	}

	for _, alias := range aliases {
		if _, exists := c.ipv6Devices[alias]; exists {
			continue
		}
		devIdx := deviceIndex(alias)
		if devIdx <= 0 {
			continue
		}
		port := c.ipv6Port + devIdx
		addr := fmt.Sprintf(":%d", port)

		deviceAlias := alias
		connect := func(ctx context.Context, targetAddr string) (net.Conn, error) {
			slots := c.proxyUC.SlotRepo.ListHealthyForDevice(deviceAlias)
			slot, err := c.proxyUC.SelectSlot(slots)
			if err != nil {
				return nil, err
			}
			return c.proxyUC.ConnectIPv6(slot.Name, targetAddr)
		}

		handler := NewMuxHandler(c.Log, connect)
		if err := handler.Listen(addr); err != nil {
			c.Log.WithError(err).Warnf("IPv6 device proxy for %s failed to bind %s", deviceAlias, addr)
			continue
		}
		go func() {
			if err := handler.Serve(); err != nil {
				c.Log.WithError(err).Warnf("IPv6 device proxy for %s serve failed on %s", deviceAlias, addr)
			}
		}()

		c.ipv6Devices[alias] = handler
		c.Log.Infof("IPv6 device proxy: %s → port %d", alias, port)
	}
}

// SyncSlots starts/stops per-slot mux handlers.
func (c *PortBasedHandler) SyncSlots(slotNames []string) {
	if c.slotStart <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	desired := make(map[string]bool)
	for _, name := range slotNames {
		desired[name] = true
	}

	// Stop removed slots
	for name, handler := range c.slots {
		if !desired[name] {
			c.Log.Infof("stopping slot proxy for %s", name)
			handler.Shutdown(context.Background())
			delete(c.slots, name)
		}
	}

	// Start new slots
	for _, name := range slotNames {
		if _, exists := c.slots[name]; exists {
			continue
		}

		slotIndex := extractSlotIndex(name)
		if slotIndex < 0 {
			continue
		}

		port := c.slotStart + slotIndex
		addr := fmt.Sprintf(":%d", port)

		slotName := name // capture for closure
		connect := func(ctx context.Context, targetAddr string) (net.Conn, error) {
			return c.proxyUC.Connect(slotName, targetAddr)
		}

		handler := NewMuxHandler(c.Log, connect)
		if err := handler.Listen(addr); err != nil {
			c.Log.WithError(err).Warnf("slot proxy for %s failed to bind %s", slotName, addr)
			continue
		}
		go func() {
			if err := handler.Serve(); err != nil {
				c.Log.WithError(err).Warnf("slot proxy for %s serve failed on %s", slotName, addr)
			}
		}()

		c.slots[name] = handler
		c.Log.Infof("slot proxy: %s → port %d", name, port)
	}
}

// SyncSlotsIPv6 starts/stops per-slot IPv6 mux handlers.
func (c *PortBasedHandler) SyncSlotsIPv6(slotNames []string) {
	if c.ipv6SlotStart <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	desired := make(map[string]bool)
	for _, name := range slotNames {
		desired[name] = true
	}

	for name, handler := range c.ipv6Slots {
		if !desired[name] {
			c.Log.Infof("stopping IPv6 slot proxy for %s", name)
			handler.Shutdown(context.Background())
			delete(c.ipv6Slots, name)
		}
	}

	for _, name := range slotNames {
		if _, exists := c.ipv6Slots[name]; exists {
			continue
		}

		slotIndex := extractSlotIndex(name)
		if slotIndex < 0 {
			continue
		}

		port := c.ipv6SlotStart + slotIndex
		addr := fmt.Sprintf(":%d", port)

		slotName := name
		connect := func(ctx context.Context, targetAddr string) (net.Conn, error) {
			return c.proxyUC.ConnectIPv6(slotName, targetAddr)
		}

		handler := NewMuxHandler(c.Log, connect)
		if err := handler.Listen(addr); err != nil {
			c.Log.WithError(err).Warnf("IPv6 slot proxy for %s failed to bind %s", slotName, addr)
			continue
		}
		go func() {
			if err := handler.Serve(); err != nil {
				c.Log.WithError(err).Warnf("IPv6 slot proxy for %s serve failed on %s", slotName, addr)
			}
		}()

		c.ipv6Slots[name] = handler
		c.Log.Infof("IPv6 slot proxy: %s → port %d", name, port)
	}
}

// GetPortMappings returns current slot → port mappings.
func (c *PortBasedHandler) GetPortMappings() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	mappings := make(map[string]int, len(c.slots))
	for name := range c.slots {
		idx := extractSlotIndex(name)
		if idx >= 0 {
			mappings[name] = c.slotStart + idx
		}
	}
	return mappings
}

// Shutdown stops all listeners.
func (c *PortBasedHandler) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	if c.shared != nil {
		c.shared.Shutdown(ctx)
	}
	if c.ipv6Shared != nil {
		c.ipv6Shared.Shutdown(ctx)
	}
	for _, h := range c.devices {
		h.Shutdown(ctx)
	}
	for _, h := range c.ipv6Devices {
		h.Shutdown(ctx)
	}
	for _, h := range c.slots {
		h.Shutdown(ctx)
	}
	for _, h := range c.ipv6Slots {
		h.Shutdown(ctx)
	}
	c.mu.Unlock()
	return nil
}

// extractSlotIndex parses "slot0" → 0, "slot5" → 5, etc.
func extractSlotIndex(name string) int {
	if strings.HasPrefix(name, "slot") {
		n, err := strconv.Atoi(name[4:])
		if err != nil {
			return -1
		}
		return n
	}
	return -1
}

// deviceIndex extracts the numeric part from "dev1" → 1, "dev2" → 2.
func deviceIndex(alias string) int {
	if strings.HasPrefix(alias, "dev") {
		n, err := strconv.Atoi(alias[3:])
		if err != nil {
			return -1
		}
		return n
	}
	return -1
}
