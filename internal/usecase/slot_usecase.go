package usecase

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/gateway/netns"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
)

const slaacWaitDuration = 5 * time.Second

type SlotUseCase struct {
	Log         *logrus.Logger
	Validate    *validator.Validate
	Discovery   *netns.Discovery
	Provisioner *netns.Provisioner
	Interface   string
	DNS64Server string
	slots       map[string]*entity.Slot
	mu          sync.RWMutex
}

func NewSlotUseCase(log *logrus.Logger, validate *validator.Validate, discovery *netns.Discovery, provisioner *netns.Provisioner, iface string, dns64 string) *SlotUseCase {
	return &SlotUseCase{
		Log:         log,
		Validate:    validate,
		Discovery:   discovery,
		Provisioner: provisioner,
		Interface:   iface,
		DNS64Server: dns64,
		slots:       make(map[string]*entity.Slot),
	}
}

func (c *SlotUseCase) UpdateSlots(discovered []*entity.Slot) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().UnixMilli()

	seen := make(map[string]bool)
	for _, s := range discovered {
		seen[s.Name] = true
		s.LastCheckedAt = now

		if existing, ok := c.slots[s.Name]; ok {
			s.ActiveConnections = atomic.LoadInt64(&existing.ActiveConnections)
		}
		c.slots[s.Name] = s
	}

	for name, slot := range c.slots {
		if !seen[name] {
			slot.Status = entity.SlotStatusUnhealthy
			slot.LastCheckedAt = now
		}
	}
}

func (c *SlotUseCase) SelectRandom() (*entity.Slot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	healthy := make([]*entity.Slot, 0)
	for _, s := range c.slots {
		if s.Status == entity.SlotStatusHealthy {
			healthy = append(healthy, s)
		}
	}

	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy slots available")
	}

	return healthy[rand.Intn(len(healthy))], nil
}

func (c *SlotUseCase) SelectByName(name string) (*entity.Slot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	slot, ok := c.slots[name]
	if !ok {
		return nil, fmt.Errorf("slot %s not found", name)
	}

	if slot.Status != entity.SlotStatusHealthy {
		return nil, fmt.Errorf("slot %s is %s", name, slot.Status)
	}

	return slot, nil
}

func (c *SlotUseCase) ListAll() []*entity.Slot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*entity.Slot, 0, len(c.slots))
	for _, s := range c.slots {
		result = append(result, s)
	}
	return result
}

func (c *SlotUseCase) GetByName(request *model.GetSlotRequest) (*model.SlotResponse, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	slot, ok := c.slots[request.SlotName]
	if !ok {
		return nil, fmt.Errorf("slot %s not found", request.SlotName)
	}

	return converter.SlotToResponse(slot), nil
}

func (c *SlotUseCase) GetStats() *model.StatsResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := &model.StatsResponse{
		SlotStats: make([]model.SlotResponse, 0, len(c.slots)),
	}

	for _, s := range c.slots {
		stats.TotalSlots++
		if s.Status == entity.SlotStatusHealthy {
			stats.HealthySlots++
		} else {
			stats.UnhealthySlots++
		}
		stats.ActiveConnections += atomic.LoadInt64(&s.ActiveConnections)
		stats.SlotStats = append(stats.SlotStats, *converter.SlotToResponse(s))
	}

	return stats
}

func (c *SlotUseCase) GetHealth() *model.HealthResponse {
	stats := c.GetStats()
	status := "healthy"
	if stats.HealthySlots == 0 {
		status = "unhealthy"
	}
	return &model.HealthResponse{
		Status:       status,
		HealthySlots: stats.HealthySlots,
		TotalSlots:   stats.TotalSlots,
	}
}

func (c *SlotUseCase) IncrementConnections(slotName string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if slot, ok := c.slots[slotName]; ok {
		atomic.AddInt64(&slot.ActiveConnections, 1)
	}
}

func (c *SlotUseCase) DecrementConnections(slotName string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if slot, ok := c.slots[slotName]; ok {
		atomic.AddInt64(&slot.ActiveConnections, -1)
	}
}

func (c *SlotUseCase) RecycleSlot(request *model.ChangeIPRequest) (*model.SlotResponse, error) {
	if c.Validate != nil {
		if err := c.Validate.Struct(request); err != nil {
			return nil, err
		}
	}

	// Parse slot index from name (e.g., "slot3" -> 3)
	indexStr := strings.TrimPrefix(request.SlotName, "slot")
	slotIndex, err := strconv.Atoi(indexStr)
	if err != nil {
		return nil, fmt.Errorf("invalid slot name %s: cannot parse index", request.SlotName)
	}

	// Lock: check slot exists, no active connections, mark as discovering
	c.mu.Lock()
	slot, ok := c.slots[request.SlotName]
	if !ok {
		c.mu.Unlock()
		return nil, model.ErrSlotNotFound
	}
	if atomic.LoadInt64(&slot.ActiveConnections) > 0 {
		c.mu.Unlock()
		return nil, model.ErrSlotBusy
	}
	slot.Status = entity.SlotStatusDiscovering
	slot.PublicIPv4 = ""
	slot.IPv6Address = ""
	c.mu.Unlock()

	if c.Log != nil {
		c.Log.Infof("recycling slot %s (index %d)", request.SlotName, slotIndex)
	}

	// Destroy old namespace
	if err := c.Provisioner.DestroySlot(request.SlotName); err != nil {
		c.mu.Lock()
		if s, ok := c.slots[request.SlotName]; ok {
			s.Status = entity.SlotStatusUnhealthy
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("destroy slot %s: %w", request.SlotName, err)
	}

	// Recreate namespace with same index
	if err := c.Provisioner.CreateSlot(slotIndex, c.Interface, c.DNS64Server); err != nil {
		c.mu.Lock()
		if s, ok := c.slots[request.SlotName]; ok {
			s.Status = entity.SlotStatusUnhealthy
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("recreate slot %s: %w", request.SlotName, err)
	}

	// Wait for SLAAC address assignment
	time.Sleep(slaacWaitDuration)

	// Resolve new IPs
	ipv4, ipv4Err := c.Discovery.ResolveSlotIP(request.SlotName)

	// Lock: update slot with new IP data
	c.mu.Lock()
	slot = c.slots[request.SlotName]
	if ipv4Err != nil {
		if c.Log != nil {
			c.Log.Warnf("slot %s: IPv4 resolution failed after recycle: %v", request.SlotName, ipv4Err)
		}
		slot.Status = entity.SlotStatusUnhealthy
	} else {
		ipv6, _ := c.Discovery.ResolveSlotIPv6(request.SlotName)
		slot.PublicIPv4 = ipv4
		slot.IPv6Address = ipv6
		slot.Status = entity.SlotStatusHealthy
	}
	slot.LastCheckedAt = time.Now().UnixMilli()
	response := converter.SlotToResponse(slot)
	c.mu.Unlock()

	if c.Log != nil {
		c.Log.Infof("slot %s recycled: IPv4=%s status=%s", request.SlotName, response.PublicIPv4, response.Status)
	}

	return response, nil
}
