//go:build linux

package usecase

import (
	"context"
	"sync"
	"time"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type SlotMonitorConfig struct {
	SteadyInterval     time.Duration
	RecoveryInterval   time.Duration
	UnhealthyThreshold int
}

type SlotMonitorUseCase struct {
	Log         *slog.Logger
	SlotRepo    *repository.SlotRepository
	Discovery   SlotDiscovery
	Provisioner SlotProvisioner
	Config      SlotMonitorConfig
	EventPub    EventPublisher

	mu    sync.Mutex
	slots map[string]context.CancelFunc
}

func NewSlotMonitorUseCase(
	log *slog.Logger,
	slotRepo *repository.SlotRepository,
	discovery SlotDiscovery,
	provisioner SlotProvisioner,
	config SlotMonitorConfig,
) *SlotMonitorUseCase {
	if config.SteadyInterval == 0 {
		config.SteadyInterval = 60 * time.Second
	}
	if config.RecoveryInterval == 0 {
		config.RecoveryInterval = 15 * time.Second
	}
	if config.UnhealthyThreshold == 0 {
		config.UnhealthyThreshold = 3
	}
	return &SlotMonitorUseCase{
		Log:         log,
		SlotRepo:    slotRepo,
		Discovery:   discovery,
		Provisioner: provisioner,
		Config:      config,
		slots:       make(map[string]context.CancelFunc),
	}
}

// Idempotent.
func (c *SlotMonitorUseCase) StartSlot(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.slots[name]; ok {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.slots[name] = cancel

	go c.monitorSlot(ctx, name)
	c.Log.Debug("monitor started", "slot", name)
}

func (c *SlotMonitorUseCase) StopSlot(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if cancel, ok := c.slots[name]; ok {
		cancel()
		delete(c.slots, name)
		c.Log.Debug("monitor stopped", "slot", name)
	}
}

func (c *SlotMonitorUseCase) StopAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, cancel := range c.slots {
		cancel()
		delete(c.slots, name)
	}
	c.Log.Info("all monitors stopped")
}

func (c *SlotMonitorUseCase) monitorSlot(ctx context.Context, name string) {
	consecutiveFails := 0
	threshold := c.Config.UnhealthyThreshold

	// Initial discovery: single IP info + IPv6
	c.initialDiscovery(name)

	for {
		slot, ok := c.SlotRepo.Get(name)

		// Determine interval and state
		interval := c.Config.SteadyInterval
		state := "monitoring"
		if ok && slot.Status == entity.SlotStatusUnhealthy {
			interval = c.Config.RecoveryInterval
			state = "recovery"
		} else if consecutiveFails > 0 {
			interval = c.Config.RecoveryInterval
			state = "degraded"
		}

		if ok {
			slot.MonitorState = state
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}

		nameserver := ""
		if s, ok := c.SlotRepo.Get(name); ok {
			nameserver = s.Nameserver
		}
		resolveReq := &model.ResolveSlotRequest{SlotName: name, Nameserver: nameserver}
		ip, err := c.Discovery.ResolveSlotIP(resolveReq)
		if err != nil {
			if slot, ok := c.SlotRepo.Get(name); ok {
				if slot.Status == entity.SlotStatusSuspended {
					continue
				}
				consecutiveFails++
				if consecutiveFails >= threshold {
					slot.Status = entity.SlotStatusUnhealthy
					c.Log.Warn("slot unhealthy", "slot", name, "consecutive_failures", consecutiveFails)
					if c.EventPub != nil {
						c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
					}
				} else {
					c.Log.Debug("check failed", "slot", name, "failures", consecutiveFails, "threshold", threshold, "error", err)
				}
			}
			continue
		}

		slot, ok = c.SlotRepo.Get(name)
		if !ok {
			continue
		}
		if slot.Status == entity.SlotStatusSuspended {
			continue
		}

		consecutiveFails = 0
		slot.Status = entity.SlotStatusHealthy

		if ip != slot.IPv4Address {
			slot.IPv4Address = ip
			c.Log.Info("ip changed", "slot", name, "ip", ip)

			// Refresh geo info on IP change
			if info, err := c.Discovery.ResolveSlotIPInfo(resolveReq); err == nil {
				slot.City = info.City
				slot.ASN = info.ASN
				slot.Org = info.Org
				slot.RTT = info.RTT
			}
		}

		if c.EventPub != nil {
			c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
		}

		// IPv6 rarely changes but keep it fresh
		newIPv6, _ := c.Discovery.ResolveSlotIPv6(resolveReq)
		c.updateIPv6(name, newIPv6)
	}
}

func (c *SlotMonitorUseCase) initialDiscovery(name string) {
	slot, ok := c.SlotRepo.Get(name)
	if !ok {
		return
	}

	resolveReq := &model.ResolveSlotRequest{SlotName: name, Nameserver: slot.Nameserver}

	info, err := c.Discovery.ResolveSlotIPInfo(resolveReq)
	if err != nil {
		c.Log.Warn("initial discovery failed", "slot", name, "error", err)
		slot.Status = entity.SlotStatusUnhealthy
		return
	}

	slot.IPv4Address = info.IP
	slot.City = info.City
	slot.ASN = info.ASN
	slot.Org = info.Org
	slot.RTT = info.RTT
	slot.Status = entity.SlotStatusHealthy
	c.Log.Info("ip discovered", "slot", name, "ip", info.IP)

	ipv6, _ := c.Discovery.ResolveSlotIPv6(resolveReq)
	c.updateIPv6Helper(slot, ipv6)

	if c.EventPub != nil {
		c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
	}
}

func (c *SlotMonitorUseCase) updateIPv6(name string, ipv6 string) {
	if slot, ok := c.SlotRepo.Get(name); ok {
		c.updateIPv6Helper(slot, ipv6)
	}
}

func (c *SlotMonitorUseCase) updateIPv6Helper(slot *entity.Slot, ipv6 string) {
	if ipv6 == "" {
		return
	}
	oldIPv6 := slot.IPv6Address
	slot.IPv6Address = ipv6

	if ipv6 != oldIPv6 && slot.Interface != "" {
		if oldIPv6 != "" {
			if err := c.Provisioner.RemoveNDPProxyEntry(&model.NDPProxyEntryRequest{IPv6: oldIPv6, Interface: slot.Interface}); err != nil {
				c.Log.Warn("failed to remove old ndp proxy", "slot", slot.Name, "ipv6", oldIPv6, "error", err)
			}
		}
		if err := c.Provisioner.AddNDPProxyEntry(&model.NDPProxyEntryRequest{IPv6: ipv6, Interface: slot.Interface}); err != nil {
			c.Log.Warn("ndp proxy failed", "slot", slot.Name, "error", err)
		}
	}
}
