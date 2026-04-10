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
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type SlotMonitorConfig struct {
	FastInterval       time.Duration
	SteadyInterval     time.Duration
	RecoveryInterval   time.Duration
	FastTicks          int
	UnhealthyThreshold int // consecutive failures before marking unhealthy (default 3)
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

// StartSlot spawns a monitor goroutine for the given slot. Idempotent.
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

// StopSlot cancels the monitor goroutine for the given slot.
func (c *SlotMonitorUseCase) StopSlot(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if cancel, ok := c.slots[name]; ok {
		cancel()
		delete(c.slots, name)
		c.Log.Debug("monitor stopped", "slot", name)
	}
}

// StopAll cancels all monitor goroutines.
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
	var pendingOldIPs []string // snapshot of pool before unknown IP appeared (nil = no verification)
	absenceCount := 0          // consecutive fast checks where no pendingOldIP was seen

	// Initial burst: discover pool IPs + metadata
	c.burstDetect(name)

	for {
		// Determine interval
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

		// Sleep or exit
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}

		// Single lightweight check (plain text, just IP)
		ip, err := c.Discovery.ResolveSlotIP(name)
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
			// Unknown IP — add to pool and start/continue rotation verification
			if pendingOldIPs == nil {
				// First unknown IP: snapshot the current pool before mutation
				pendingOldIPs = make([]string, len(slot.PublicIPv4s))
				copy(pendingOldIPs, slot.PublicIPv4s)
				absenceCount = 0
			}
			slot.PublicIPv4s = append(slot.PublicIPv4s, ip)
			c.Log.Info("pool ip added", "slot", name, "ip", ip, "pool", poolKey(slot.PublicIPv4s))
			fastTicks = c.Config.FastTicks
		} else if pendingOldIPs != nil {
			// Verification in progress — check if this IP is from the old pool
			if containsIP(pendingOldIPs, ip) {
				// Old IP still active — pool expansion, not rotation
				c.Log.Info("pool expansion confirmed", "slot", name, "pool", poolKey(slot.PublicIPv4s))
				pendingOldIPs = nil
				absenceCount = 0
			} else {
				// IP is known but not from old pool — old IPs still absent
				absenceCount++
				c.Log.Debug("rotation verification", "slot", name, "absence", fmt.Sprintf("%d/%d", absenceCount, c.Config.FastTicks))
				if absenceCount >= c.Config.FastTicks {
					// Confirmed rotation: old IPs never reappeared
					oldPool := poolKey(pendingOldIPs)
					slot.PublicIPv4s = removeIPs(slot.PublicIPv4s, pendingOldIPs)
					slot.IPChangeCount++
					slot.IPChangedAt = time.Now().UnixMilli()
					c.Log.Info("pool rotated", "slot", name, "old", oldPool, "new", poolKey(slot.PublicIPv4s))
					pendingOldIPs = nil
					absenceCount = 0
					// Refresh metadata for the new pool
					c.burstDetect(name)
				}
			}
		}

		slot.NextCheckAt = time.Now().Add(interval).UnixMilli()
		if c.EventPub != nil {
			c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
		}

		// Update IPv6 (rarely changes but keep fresh)
		newIPv6, _ := c.Discovery.ResolveSlotIPv6(name)
		c.updateIPv6(name, newIPv6)
	}
}

// burstDetect makes up to 5 rapid IP checks using the JSON endpoint
// to discover pool IPs and collect metadata (city, ASN, RTT).
// On initial discovery, sets the pool. On subsequent calls (post-rotation),
// merges new IPs into the existing pool. No classification logic —
// rotation detection is handled by the sustained absence verification
// in monitorSlot.
func (c *SlotMonitorUseCase) burstDetect(name string) {
	seen := make(map[string]bool)
	var ips []string
	var city, asn, org, rtt string

	for i := 0; i < 5; i++ {
		gotIP, gotCity, gotASN, gotOrg, gotRTT, err := c.Discovery.ResolveSlotIPInfo(name)
		if err != nil {
			c.Log.Warn("burst check failed", "slot", name, "attempt", i+1, "error", err)
			continue
		}
		if city == "" {
			city = gotCity
			asn = gotASN
			org = gotOrg
			rtt = gotRTT
		}
		if !seen[gotIP] {
			seen[gotIP] = true
			ips = append(ips, gotIP)
		}
	}

	ipv6, _ := c.Discovery.ResolveSlotIPv6(name)

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
		// Initial discovery
		slot.PublicIPv4s = ips
		slot.IPChangedAt = now
		c.Log.Info("pool discovered", "slot", name, "pool", poolKey(ips))
	} else {
		// Merge any new IPs from burst into existing pool
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

func (c *SlotMonitorUseCase) updateSlotStatus(name string, status string) {
	if slot, ok := c.SlotRepo.Get(name); ok {
		slot.Status = status
		slot.LastCheckedAt = time.Now().UnixMilli()
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
			c.Provisioner.RemoveNDPProxyEntry(oldIPv6, slot.Interface)
		}
		if err := c.Provisioner.AddNDPProxyEntry(ipv6, slot.Interface); err != nil {
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
