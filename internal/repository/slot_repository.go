//go:build linux

package repository

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
)

// SlotRepository is an in-memory, thread-safe store for slot entities.
// It acts as the repository layer for slots (which have no database persistence).
type SlotRepository struct {
	Log     *logrus.Logger
	mu      sync.RWMutex
	slots   map[string]*entity.Slot
	slotSeq uint64
}

// NewSlotRepository creates a new in-memory slot repository.
func NewSlotRepository(log *logrus.Logger) *SlotRepository {
	return &SlotRepository{
		Log:   log,
		slots: make(map[string]*entity.Slot),
	}
}

// NextSlotIndex returns a globally unique slot index (0, 1, 2, ...).
func (r *SlotRepository) NextSlotIndex() int {
	return int(atomic.AddUint64(&r.slotSeq, 1) - 1)
}

// Put inserts or replaces a slot in the store.
func (r *SlotRepository) Put(slot *entity.Slot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.slots[slot.Name] = slot
}

// Get returns a slot by name and whether it exists.
func (r *SlotRepository) Get(name string) (*entity.Slot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	slot, ok := r.slots[name]
	return slot, ok
}

// Delete removes a slot by name.
func (r *SlotRepository) Delete(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.slots, name)
}

// ListAll returns a snapshot of all slots.
func (r *SlotRepository) ListAll() []*entity.Slot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*entity.Slot, 0, len(r.slots))
	for _, s := range r.slots {
		result = append(result, s)
	}
	return result
}

// ListNames returns the names of all slots.
func (r *SlotRepository) ListNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.slots))
	for name := range r.slots {
		names = append(names, name)
	}
	return names
}

// ListHealthy returns all slots with status "healthy".
func (r *SlotRepository) ListHealthy() []*entity.Slot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var healthy []*entity.Slot
	for _, s := range r.slots {
		if s.Status == entity.SlotStatusHealthy {
			healthy = append(healthy, s)
		}
	}
	return healthy
}

// DeleteByDevice removes all slots whose name starts with "<deviceAlias>_slot"
// and returns the count of removed slots.
func (r *SlotRepository) DeleteByDevice(deviceAlias string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := deviceAlias + "_slot"
	removed := 0
	for name := range r.slots {
		if strings.HasPrefix(name, prefix) {
			delete(r.slots, name)
			removed++
		}
	}
	return removed
}

// CountByDevice returns the number of slots belonging to a device.
func (r *SlotRepository) CountByDevice(deviceAlias string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	prefix := deviceAlias + "_slot"
	count := 0
	for name := range r.slots {
		if strings.HasPrefix(name, prefix) {
			count++
		}
	}
	return count
}

// IncrementConnections atomically increments the active connection count for a slot.
func (r *SlotRepository) IncrementConnections(name string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if slot, ok := r.slots[name]; ok {
		atomic.AddInt64(&slot.ActiveConnections, 1)
	}
}

// DecrementConnections atomically decrements the active connection count for a slot.
func (r *SlotRepository) DecrementConnections(name string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if slot, ok := r.slots[name]; ok {
		atomic.AddInt64(&slot.ActiveConnections, -1)
	}
}
