package usecase

import (
	"fmt"
	"math/rand"
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

type SlotUseCase struct {
	Log       *logrus.Logger
	Validate  *validator.Validate
	Discovery *netns.Discovery
	slots     map[string]*entity.Slot
	mu        sync.RWMutex
}

func NewSlotUseCase(log *logrus.Logger, validate *validator.Validate, discovery *netns.Discovery) *SlotUseCase {
	return &SlotUseCase{
		Log:       log,
		Validate:  validate,
		Discovery: discovery,
		slots:     make(map[string]*entity.Slot),
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
