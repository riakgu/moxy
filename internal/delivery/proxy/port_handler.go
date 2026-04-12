//go:build linux

package proxy

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"log/slog"

	"github.com/riakgu/moxy/internal/usecase"
)

type PortBasedHandler struct {
	Log           *slog.Logger
	proxyUC       *usecase.ProxyUseCase
	proxyPort     int 
	slotStart     int 
	ipv6Port      int 
	ipv6SlotStart int 
	mu            sync.Mutex
	shared        *MuxHandler
	devices       map[string]*MuxHandler
	slots         map[string]*MuxHandler 
	ipv6Shared    *MuxHandler
	ipv6Devices   map[string]*MuxHandler
	ipv6Slots     map[string]*MuxHandler
}

func NewPortBasedHandler(
	log *slog.Logger,
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

func (c *PortBasedHandler) StartShared() {
	if c.proxyPort <= 0 {
		return
	}
	addr := fmt.Sprintf(":%d", c.proxyPort)
	connect := func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
		slotName, err := c.proxyUC.PickSlot()
		if err != nil {
			return nil, err
		}
		if network == "udp" {
			return c.proxyUC.ConnectUDP(slotName, targetAddr)
		}
		return c.proxyUC.Connect(slotName, targetAddr)
	}
	c.shared = NewMuxHandler(c.Log, connect)
	if err := c.shared.Listen(addr); err != nil {
		c.Log.Error("shared proxy bind failed", "addr", addr, "error", err)
		return
	}
	go func() {
		if err := c.shared.Serve(); err != nil {
			c.Log.Error("shared proxy serve failed", "addr", addr, "error", err)
		}
	}()
}

func (c *PortBasedHandler) StartSharedIPv6() {
	if c.ipv6Port <= 0 {
		return
	}
	addr := fmt.Sprintf(":%d", c.ipv6Port)
	connect := func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
		slotName, err := c.proxyUC.PickSlot()
		if err != nil {
			return nil, err
		}
		if network == "udp" {
			return c.proxyUC.ConnectIPv6UDP(slotName, targetAddr)
		}
		return c.proxyUC.ConnectIPv6(slotName, targetAddr)
	}
	c.ipv6Shared = NewMuxHandler(c.Log, connect)
	if err := c.ipv6Shared.Listen(addr); err != nil {
		c.Log.Error("shared ipv6 proxy bind failed", "addr", addr, "error", err)
		return
	}
	go func() {
		if err := c.ipv6Shared.Serve(); err != nil {
			c.Log.Error("shared ipv6 proxy serve failed", "addr", addr, "error", err)
		}
	}()
}

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

	for alias, handler := range c.devices {
		if !desired[alias] {
			c.Log.Info("device proxy stopped", "device", alias)
			if err := handler.Shutdown(context.Background()); err != nil {
				c.Log.Warn("device proxy shutdown failed", "device", alias, "error", err)
			}
			delete(c.devices, alias)
		}
	}

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
		connect := func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
			slotName, err := c.proxyUC.PickSlotForDevice(deviceAlias)
			if err != nil {
				return nil, err
			}
			if network == "udp" {
				return c.proxyUC.ConnectUDP(slotName, targetAddr)
			}
			return c.proxyUC.Connect(slotName, targetAddr)
		}

		handler := NewMuxHandler(c.Log, connect)
		if err := handler.Listen(addr); err != nil {
			c.Log.Warn("device proxy bind failed", "device", deviceAlias, "addr", addr, "error", err)
			continue
		}
		go func() {
			if err := handler.Serve(); err != nil {
				c.Log.Warn("device proxy serve failed", "device", deviceAlias, "addr", addr, "error", err)
			}
		}()

		c.devices[alias] = handler
		c.Log.Info("device proxy started", "device", alias, "port", port)
	}
}

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
			c.Log.Info("ipv6 device proxy stopped", "device", alias)
			if err := handler.Shutdown(context.Background()); err != nil {
				c.Log.Warn("ipv6 device proxy shutdown failed", "device", alias, "error", err)
			}
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
		connect := func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
			slotName, err := c.proxyUC.PickSlotForDevice(deviceAlias)
			if err != nil {
				return nil, err
			}
			if network == "udp" {
				return c.proxyUC.ConnectIPv6UDP(slotName, targetAddr)
			}
			return c.proxyUC.ConnectIPv6(slotName, targetAddr)
		}

		handler := NewMuxHandler(c.Log, connect)
		if err := handler.Listen(addr); err != nil {
			c.Log.Warn("ipv6 device proxy bind failed", "device", deviceAlias, "addr", addr, "error", err)
			continue
		}
		go func() {
			if err := handler.Serve(); err != nil {
				c.Log.Warn("ipv6 device proxy serve failed", "device", deviceAlias, "addr", addr, "error", err)
			}
		}()

		c.ipv6Devices[alias] = handler
		c.Log.Info("ipv6 device proxy started", "device", alias, "port", port)
	}
}

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

	for name, handler := range c.slots {
		if !desired[name] {
			c.Log.Info("slot proxy stopped", "slot", name)
			if err := handler.Shutdown(context.Background()); err != nil {
				c.Log.Warn("slot proxy shutdown failed", "slot", name, "error", err)
			}
			delete(c.slots, name)
		}
	}

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
		connect := func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
			if network == "udp" {
				return c.proxyUC.ConnectUDP(slotName, targetAddr)
			}
			return c.proxyUC.Connect(slotName, targetAddr)
		}

		handler := NewMuxHandler(c.Log, connect)
		if err := handler.Listen(addr); err != nil {
			c.Log.Warn("slot proxy bind failed", "slot", slotName, "addr", addr, "error", err)
			continue
		}
		go func() {
			if err := handler.Serve(); err != nil {
				c.Log.Warn("slot proxy serve failed", "slot", slotName, "addr", addr, "error", err)
			}
		}()

		c.slots[name] = handler
		c.Log.Info("slot proxy started", "slot", name, "port", port)
	}
}

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
			c.Log.Info("ipv6 slot proxy stopped", "slot", name)
			if err := handler.Shutdown(context.Background()); err != nil {
				c.Log.Warn("ipv6 slot proxy shutdown failed", "slot", name, "error", err)
			}
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
		connect := func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
			if network == "udp" {
				return c.proxyUC.ConnectIPv6UDP(slotName, targetAddr)
			}
			return c.proxyUC.ConnectIPv6(slotName, targetAddr)
		}

		handler := NewMuxHandler(c.Log, connect)
		if err := handler.Listen(addr); err != nil {
			c.Log.Warn("ipv6 slot proxy bind failed", "slot", slotName, "addr", addr, "error", err)
			continue
		}
		go func() {
			if err := handler.Serve(); err != nil {
				c.Log.Warn("ipv6 slot proxy serve failed", "slot", slotName, "addr", addr, "error", err)
			}
		}()

		c.ipv6Slots[name] = handler
		c.Log.Info("ipv6 slot proxy started", "slot", name, "port", port)
	}
}

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

func (c *PortBasedHandler) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	if c.shared != nil {
		if err := c.shared.Shutdown(ctx); err != nil {
			c.Log.Warn("shared proxy shutdown failed", "error", err)
		}
	}
	if c.ipv6Shared != nil {
		if err := c.ipv6Shared.Shutdown(ctx); err != nil {
			c.Log.Warn("ipv6 shared proxy shutdown failed", "error", err)
		}
	}
	for alias, h := range c.devices {
		if err := h.Shutdown(ctx); err != nil {
			c.Log.Warn("device proxy shutdown failed", "device", alias, "error", err)
		}
	}
	for alias, h := range c.ipv6Devices {
		if err := h.Shutdown(ctx); err != nil {
			c.Log.Warn("ipv6 device proxy shutdown failed", "device", alias, "error", err)
		}
	}
	for name, h := range c.slots {
		if err := h.Shutdown(ctx); err != nil {
			c.Log.Warn("slot proxy shutdown failed", "slot", name, "error", err)
		}
	}
	for name, h := range c.ipv6Slots {
		if err := h.Shutdown(ctx); err != nil {
			c.Log.Warn("ipv6 slot proxy shutdown failed", "slot", name, "error", err)
		}
	}
	c.mu.Unlock()
	return nil
}

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
