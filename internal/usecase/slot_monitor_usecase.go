//go:build linux

package usecase

import (
	"context"
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

	// Already monitoring
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

	for {
		// 1. Check IP
		ipv4, err := c.Discovery.ResolveSlotIP(name)
		ipv6, _ := c.Discovery.ResolveSlotIPv6(name)

		// 2. Update entity
		c.updateSlot(name, ipv4, ipv6, err)

		// 3. Determine interval
		interval := c.Config.SteadyInterval
		state := "steady"
		if err != nil {
			interval = c.Config.RecoveryInterval
			state = "recovery"
			fastTicks = c.Config.FastTicks // reset on failure
		} else if fastTicks > 0 {
			interval = c.Config.FastInterval
			state = "fast"
			fastTicks--
		}

		// 4. Update next check info
		if slot, ok := c.SlotRepo.Get(name); ok {
			slot.NextCheckAt = time.Now().Add(interval).UnixMilli()
			slot.MonitorState = state
		}

		// 5. Sleep or exit
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (c *SlotMonitorUseCase) updateSlot(name, ipv4, ipv6 string, checkErr error) {
	slot, ok := c.SlotRepo.Get(name)
	if !ok {
		return
	}

	now := time.Now().UnixMilli()
	slot.LastCheckedAt = now

	if checkErr != nil {
		slot.Status = entity.SlotStatusUnhealthy
		c.Log.Warnf("monitor: %s check failed: %v", name, checkErr)
		return
	}

	// Log genuine IP changes (not just initial discovery)
	if slot.PublicIPv4 != "" && slot.PublicIPv4 != ipv4 && ipv4 != "" {
		c.Log.Infof("monitor: %s IPv4 changed %s → %s", name, slot.PublicIPv4, ipv4)
	}

	slot.PublicIPv4 = ipv4
	slot.Status = entity.SlotStatusHealthy

	// Update IPv6 and refresh NDP proxy if changed
	oldIPv6 := slot.IPv6Address
	if ipv6 != "" {
		slot.IPv6Address = ipv6
	}

	if ipv6 != "" && ipv6 != oldIPv6 && slot.Interface != "" {
		if oldIPv6 != "" {
			c.Provisioner.RemoveNDPProxyEntry(oldIPv6, slot.Interface)
		}
		if err := c.Provisioner.AddNDPProxyEntry(ipv6, slot.Interface); err != nil {
			c.Log.Warnf("monitor: NDP proxy for %s failed: %v", name, err)
		}
	}
}
