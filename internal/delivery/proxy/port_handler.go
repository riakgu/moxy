//go:build linux

package proxy

import (
	"context"
	"fmt"
	"net"
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
	ipv6Shared    *MuxHandler
	devices       []*MuxHandler
	ipv6Devices   []*MuxHandler
	slots         []*MuxHandler
	ipv6Slots     []*MuxHandler
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
	}
}

// StartAll pre-binds all ports at startup. Connections to non-existent slots/devices
// are rejected at the proxy usecase level.
func (c *PortBasedHandler) StartAll(maxDevices, maxSlots int) {
	// Shared IPv4
	if c.proxyPort > 0 {
		c.shared = c.startListener(fmt.Sprintf(":%d", c.proxyPort), "shared-ipv4",
			func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
				slotName, err := c.proxyUC.PickSlot()
				if err != nil {
					return nil, err
				}
				if network == "udp" {
					return c.proxyUC.ConnectUDP(slotName, targetAddr)
				}
				return c.proxyUC.Connect(slotName, targetAddr)
			})
	}

	// Shared IPv6
	if c.ipv6Port > 0 {
		c.ipv6Shared = c.startListener(fmt.Sprintf(":%d", c.ipv6Port), "shared-ipv6",
			func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
				slotName, err := c.proxyUC.PickSlot()
				if err != nil {
					return nil, err
				}
				if network == "udp" {
					return c.proxyUC.ConnectIPv6UDP(slotName, targetAddr)
				}
				return c.proxyUC.ConnectIPv6(slotName, targetAddr)
			})
	}

	// Per-device IPv4
	c.devices = make([]*MuxHandler, maxDevices)
	for i := 1; i <= maxDevices; i++ {
		if c.proxyPort <= 0 {
			break
		}
		port := c.proxyPort + i
		deviceAlias := fmt.Sprintf("dev%d", i)
		c.devices[i-1] = c.startListener(fmt.Sprintf(":%d", port), fmt.Sprintf("device-%s-ipv4", deviceAlias),
			func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
				slotName, err := c.proxyUC.PickSlotForDevice(deviceAlias)
				if err != nil {
					return nil, err
				}
				if network == "udp" {
					return c.proxyUC.ConnectUDP(slotName, targetAddr)
				}
				return c.proxyUC.Connect(slotName, targetAddr)
			})
	}

	// Per-device IPv6
	c.ipv6Devices = make([]*MuxHandler, maxDevices)
	for i := 1; i <= maxDevices; i++ {
		if c.ipv6Port <= 0 {
			break
		}
		port := c.ipv6Port + i
		deviceAlias := fmt.Sprintf("dev%d", i)
		c.ipv6Devices[i-1] = c.startListener(fmt.Sprintf(":%d", port), fmt.Sprintf("device-%s-ipv6", deviceAlias),
			func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
				slotName, err := c.proxyUC.PickSlotForDevice(deviceAlias)
				if err != nil {
					return nil, err
				}
				if network == "udp" {
					return c.proxyUC.ConnectIPv6UDP(slotName, targetAddr)
				}
				return c.proxyUC.ConnectIPv6(slotName, targetAddr)
			})
	}

	// Per-slot IPv4
	c.slots = make([]*MuxHandler, maxSlots)
	for i := 1; i <= maxSlots; i++ {
		if c.slotStart <= 0 {
			break
		}
		port := c.slotStart + i
		slotName := fmt.Sprintf("slot%d", i)
		c.slots[i-1] = c.startListener(fmt.Sprintf(":%d", port), fmt.Sprintf("slot-%s-ipv4", slotName),
			func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
				if network == "udp" {
					return c.proxyUC.ConnectUDP(slotName, targetAddr)
				}
				return c.proxyUC.Connect(slotName, targetAddr)
			})
	}

	// Per-slot IPv6
	c.ipv6Slots = make([]*MuxHandler, maxSlots)
	for i := 1; i <= maxSlots; i++ {
		if c.ipv6SlotStart <= 0 {
			break
		}
		port := c.ipv6SlotStart + i
		slotName := fmt.Sprintf("slot%d", i)
		c.ipv6Slots[i-1] = c.startListener(fmt.Sprintf(":%d", port), fmt.Sprintf("slot-%s-ipv6", slotName),
			func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
				if network == "udp" {
					return c.proxyUC.ConnectIPv6UDP(slotName, targetAddr)
				}
				return c.proxyUC.ConnectIPv6(slotName, targetAddr)
			})
	}

	c.Log.Info("all ports pre-bound",
		"shared_ipv4", c.proxyPort > 0,
		"shared_ipv6", c.ipv6Port > 0,
		"devices", maxDevices,
		"slots", maxSlots,
	)
}

func (c *PortBasedHandler) startListener(addr, label string, connect func(ctx context.Context, network, targetAddr string) (net.Conn, error)) *MuxHandler {
	handler := NewMuxHandler(c.Log, connect)
	if err := handler.Listen(addr); err != nil {
		c.Log.Error("proxy bind failed", "label", label, "addr", addr, "error", err)
		return nil
	}
	go func() {
		if err := handler.Serve(); err != nil {
			c.Log.Warn("proxy serve ended", "label", label, "addr", addr, "error", err)
		}
	}()
	return handler
}

func (c *PortBasedHandler) GetPortMappings() map[string]int {
	mappings := make(map[string]int)
	for i, h := range c.slots {
		if h != nil {
			name := fmt.Sprintf("slot%d", i+1)
			mappings[name] = c.slotStart + i + 1
		}
	}
	return mappings
}

func (c *PortBasedHandler) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	shutdownHandler := func(h *MuxHandler, label string) {
		if h == nil {
			return
		}
		if err := h.Shutdown(ctx); err != nil {
			c.Log.Warn("proxy shutdown failed", "label", label, "error", err)
		}
	}

	shutdownHandler(c.shared, "shared-ipv4")
	shutdownHandler(c.ipv6Shared, "shared-ipv6")

	for i, h := range c.devices {
		shutdownHandler(h, fmt.Sprintf("device-dev%d-ipv4", i+1))
	}
	for i, h := range c.ipv6Devices {
		shutdownHandler(h, fmt.Sprintf("device-dev%d-ipv6", i+1))
	}
	for i, h := range c.slots {
		shutdownHandler(h, fmt.Sprintf("slot%d-ipv4", i+1))
	}
	for i, h := range c.ipv6Slots {
		shutdownHandler(h, fmt.Sprintf("slot%d-ipv6", i+1))
	}

	return nil
}
