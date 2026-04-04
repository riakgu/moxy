//go:build linux

package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/repository"
)

type SlotMonitorConfig struct {
	FastInterval     time.Duration
	SteadyInterval   time.Duration
	RecoveryInterval time.Duration
	FastTicks        int
}

type SlotMonitorUseCase struct {
	Log         *logrus.Logger
	SlotRepo    *repository.SlotRepository
	Discovery   SlotDiscovery
	Provisioner SlotProvisioner
	Config      SlotMonitorConfig

	mu    sync.Mutex
	slots map[string]context.CancelFunc
}

func NewSlotMonitorUseCase(
	log *logrus.Logger,
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
	c.Log.Debugf("monitor: started for %s", name)
}

// StopSlot cancels the monitor goroutine for the given slot.
func (c *SlotMonitorUseCase) StopSlot(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if cancel, ok := c.slots[name]; ok {
		cancel()
		delete(c.slots, name)
		c.Log.Debugf("monitor: stopped for %s", name)
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
	c.Log.Info("monitor: all slot monitors stopped")
}

func (c *SlotMonitorUseCase) monitorSlot(ctx context.Context, name string) {
	fastTicks := c.Config.FastTicks

	// Initial burst: detect IP pair with metadata
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

		// Steady-state: single lightweight check (plain text, just IP)
		ip, err := c.Discovery.ResolveSlotIP(name)
		if err != nil {
			c.updateSlotStatus(name, entity.SlotStatusUnhealthy)
			c.Log.Warnf("monitor: %s check failed: %v", name, err)
			continue
		}

		// Check if IP is in known pair
		if slot, ok := c.SlotRepo.Get(name); ok {
			if !containsIP(slot.PublicIPv4s, ip) {
				// Unknown IP — re-burst with full metadata
				c.Log.Infof("monitor: %s unknown IP %s (not in pair %v) — re-detecting",
					name, ip, slot.PublicIPv4s)
				c.burstDetect(name)
				fastTicks = c.Config.FastTicks
			} else {
				// Known IP — just update status
				slot.LastCheckedAt = time.Now().UnixMilli()
				slot.Status = entity.SlotStatusHealthy
			}
		}

		// Update IPv6 (rarely changes but keep fresh)
		newIPv6, _ := c.Discovery.ResolveSlotIPv6(name)
		c.updateIPv6(name, newIPv6)
	}
}

// burstDetect makes up to 5 rapid IP checks using the JSON endpoint
// to discover the CGNAT pair and collect metadata (city, ASN, RTT).
// Stops early when it sees a repeated IP.
func (c *SlotMonitorUseCase) burstDetect(name string) {
	seen := make(map[string]bool)
	var ips []string
	var city, asn, org, rtt string

	for i := 0; i < 5; i++ {
		gotIP, gotCity, gotASN, gotOrg, gotRTT, err := c.Discovery.ResolveSlotIPInfo(name)
		if err != nil {
			c.Log.Warnf("monitor: %s burst check %d failed: %v", name, i+1, err)
			continue
		}
		// Store metadata from first successful response
		if city == "" {
			city = gotCity
			asn = gotASN
			org = gotOrg
			rtt = gotRTT
		}
		if seen[gotIP] {
			break // repeat seen — pair complete
		}
		seen[gotIP] = true
		ips = append(ips, gotIP)
	}

	// Update IPv6 too
	ipv6, _ := c.Discovery.ResolveSlotIPv6(name)

	slot, ok := c.SlotRepo.Get(name)
	if !ok {
		return
	}

	now := time.Now().UnixMilli()
	slot.LastCheckedAt = now

	if len(ips) == 0 {
		slot.Status = entity.SlotStatusUnhealthy
		c.Log.Warnf("monitor: %s pair detection failed", name)
		return
	}

	// Log pair changes
	oldPair := pairKey(slot.PublicIPv4s)
	newPair := pairKey(ips)
	if oldPair != "" && oldPair != newPair {
		c.Log.Infof("monitor: %s pair rebuilt [%s] → [%s]", name, oldPair, newPair)
	} else if oldPair == "" {
		c.Log.Infof("monitor: %s pair discovered [%s]", name, newPair)
	}

	slot.PublicIPv4s = ips
	slot.City = city
	slot.ASN = asn
	slot.Org = org
	slot.RTT = rtt
	slot.Status = entity.SlotStatusHealthy

	// Update IPv6
	c.updateIPv6Helper(slot, ipv6)
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
			c.Log.Warnf("monitor: NDP proxy for %s failed: %v", slot.Name, err)
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

func pairKey(ips []string) string {
	if len(ips) == 0 {
		return ""
	}
	sorted := make([]string, len(ips))
	copy(sorted, ips)
	sort.Strings(sorted)
	return fmt.Sprintf("%s", strings.Join(sorted, ", "))
}
