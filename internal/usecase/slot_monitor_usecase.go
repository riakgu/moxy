//go:build linux

package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type SlotMonitorConfig struct {
	FastInterval       time.Duration
	SteadyInterval     time.Duration
	RecoveryInterval   time.Duration
	FastTicks          int
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
	fastTicks := c.Config.FastTicks
	consecutiveFails := 0
	threshold := c.Config.UnhealthyThreshold
	if threshold <= 0 {
		threshold = 3
	}

	// Rotation verification state (goroutine-local)
	var pendingOldIPs []string 
	absenceCount := 0      

	c.burstDetect(name)

	for {
		interval := c.Config.SteadyInterval
		state := "steady"
		if fastTicks > 0 {
			interval = c.Config.FastInterval
			state = "fast"
			fastTicks--
		}

		slot, ok := c.SlotRepo.Get(name)
		if ok {
			if slot.Status == entity.SlotStatusUnhealthy {
				interval = c.Config.RecoveryInterval
				state = "recovery"
				fastTicks = c.Config.FastTicks
			}
			slot.NextCheckAt = time.Now().Add(interval).UnixMilli()
			slot.MonitorState = state
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}

		resolveReq := &model.ResolveSlotRequest{SlotName: name}
		ip, err := c.Discovery.ResolveSlotIP(resolveReq)
		if err != nil {
			if slot, ok := c.SlotRepo.Get(name); ok {
				if slot.Status == entity.SlotStatusSuspended {
					continue
				}
				consecutiveFails++
				slot.LastCheckedAt = time.Now().UnixMilli()
				slot.NextCheckAt = time.Now().Add(interval).UnixMilli()
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
		slot.LastCheckedAt = time.Now().UnixMilli()
		slot.Status = entity.SlotStatusHealthy

		if !containsIP(slot.PublicIPv4s, ip) {
			if pendingOldIPs == nil {
				pendingOldIPs = make([]string, len(slot.PublicIPv4s))
				copy(pendingOldIPs, slot.PublicIPv4s)
				absenceCount = 0
			}
			slot.PublicIPv4s = append(slot.PublicIPv4s, ip)
			c.Log.Info("pool ip added", "slot", name, "ip", ip, "pool", poolKey(slot.PublicIPv4s))
			fastTicks = c.Config.FastTicks
		} else if pendingOldIPs != nil {
			if containsIP(pendingOldIPs, ip) {
				c.Log.Info("pool expansion confirmed", "slot", name, "pool", poolKey(slot.PublicIPv4s))
				pendingOldIPs = nil
				absenceCount = 0
			} else {
				absenceCount++
				c.Log.Debug("rotation verification", "slot", name, "absence", fmt.Sprintf("%d/%d", absenceCount, c.Config.FastTicks))
				if absenceCount >= c.Config.FastTicks {
					oldPool := poolKey(pendingOldIPs)
					slot.PublicIPv4s = removeIPs(slot.PublicIPv4s, pendingOldIPs)
					slot.IPChangeCount++
					slot.IPChangedAt = time.Now().UnixMilli()
					c.Log.Info("pool rotated", "slot", name, "old", oldPool, "new", poolKey(slot.PublicIPv4s))
					pendingOldIPs = nil
					absenceCount = 0
					c.burstDetect(name)
				}
			}
		}

		slot.NextCheckAt = time.Now().Add(interval).UnixMilli()
		if c.EventPub != nil {
			c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
		}

		// IPv6 rarely changes but keep it fresh
		newIPv6, _ := c.Discovery.ResolveSlotIPv6(resolveReq)
		c.updateIPv6(name, newIPv6)
	}
}

func (c *SlotMonitorUseCase) burstDetect(name string) {
	seen := make(map[string]bool)
	var ips []string
	var city, asn, org, rtt string

	resolveReq := &model.ResolveSlotRequest{SlotName: name}
	for i := 0; i < 5; i++ {
		info, err := c.Discovery.ResolveSlotIPInfo(resolveReq)
		if err != nil {
			c.Log.Warn("burst check failed", "slot", name, "attempt", i+1, "error", err)
			continue
		}
		if city == "" {
			city = info.City
			asn = info.ASN
			org = info.Org
			rtt = info.RTT
		}
		if !seen[info.IP] {
			seen[info.IP] = true
			ips = append(ips, info.IP)
		}
	}

	ipv6, _ := c.Discovery.ResolveSlotIPv6(resolveReq)

	slot, ok := c.SlotRepo.Get(name)
	if !ok {
		return
	}

	now := time.Now().UnixMilli()
	slot.LastCheckedAt = now

	if len(ips) == 0 {
		slot.Status = entity.SlotStatusUnhealthy
		c.Log.Warn("pool detection failed", "slot", name)
		return
	}

	if len(slot.PublicIPv4s) == 0 {
		slot.PublicIPv4s = ips
		slot.IPChangedAt = now
		c.Log.Info("pool discovered", "slot", name, "pool", poolKey(ips))
	} else {
		for _, ip := range ips {
			if !containsIP(slot.PublicIPv4s, ip) {
				slot.PublicIPv4s = append(slot.PublicIPv4s, ip)
			}
		}
	}

	slot.City = city
	slot.ASN = asn
	slot.Org = org
	slot.RTT = rtt
	slot.Status = entity.SlotStatusHealthy

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

func containsIP(pair []string, ip string) bool {
	for _, p := range pair {
		if p == ip {
			return true
		}
	}
	return false
}

// poolKey returns a sorted, comma-separated string of IPs for logging.
func poolKey(ips []string) string {
	if len(ips) == 0 {
		return ""
	}
	sorted := make([]string, len(ips))
	copy(sorted, ips)
	sort.Strings(sorted)
	return strings.Join(sorted, ", ")
}

// removeIPs returns a new slice with all IPs in 'remove' excluded from 'pool'.
func removeIPs(pool, remove []string) []string {
	removeSet := make(map[string]bool, len(remove))
	for _, ip := range remove {
		removeSet[ip] = true
	}
	var result []string
	for _, ip := range pool {
		if !removeSet[ip] {
			result = append(result, ip)
		}
	}
	return result
}
