//go:build linux

package repository

import (
	"sync"
	"sync/atomic"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
)

// SlotRepository is an in-memory, thread-safe store for slot entities.
// It acts as the repository layer for slots (which have no database persistence).
type SlotRepository struct {
	Log     *slog.Logger
	mu      sync.RWMutex
	slots   map[string]*entity.Slot
	slotSeq uint64
}

// NewSlotRepository creates a new in-memory slot repository.
func NewSlotRepository(log *slog.Logger) *SlotRepository {
	return &SlotRepository{
		Log:   log,
		slots: make(map[string]*entity.Slot),
	}
}

// NextSlotIndex returns a globally unique slot index (1, 2, 3, ...).
func (r *SlotRepository) NextSlotIndex() int {
	return int(atomic.AddUint64(&r.slotSeq, 1))
}

// Put inserts or replaces a slot in the store.
func (r *SlotRepository) Put(slot *entity.Slot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.slots[slot.Name] = slot
}

// Get returns a slot by name and whether it exists. The returned pointer
// references the internal map entry — callers MUST NOT mutate fields
// without appropriate synchronization. Use Put() or SetStatus() to
// persist changes.
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

// SetStatus atomically updates a slot's status.
func (r *SlotRepository) SetStatus(name string, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if slot, ok := r.slots[name]; ok {
		slot.Status = status
	}
}

// CompareAndSetStatus atomically updates status only if it matches expected.
func (r *SlotRepository) CompareAndSetStatus(name, expected, newStatus string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if slot, ok := r.slots[name]; ok && slot.Status == expected {
		slot.Status = newStatus
		return true
	}
	return false
}

// ListAll returns a snapshot of all slots. The returned pointers reference
// the internal map entries — callers MUST NOT mutate fields without
// appropriate synchronization. Use Put() or SetStatus() to persist changes.
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

// DeleteByDevice removes all slots belonging to a device and returns count removed.
func (r *SlotRepository) DeleteByDevice(deviceAlias string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for name, slot := range r.slots {
		if slot.DeviceAlias == deviceAlias {
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
	count := 0
	for _, slot := range r.slots {
		if slot.DeviceAlias == deviceAlias {
			count++
		}
	}
	return count
}

// UniqueIPsByDevice returns the count of distinct public IPv4 addresses
// across all healthy slots belonging to a device.
func (r *SlotRepository) UniqueIPsByDevice(deviceAlias string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[string]bool)
	for _, slot := range r.slots {
		if slot.DeviceAlias == deviceAlias && slot.Status == entity.SlotStatusHealthy {
			for _, ip := range slot.PublicIPv4s {
				if ip != "" {
					seen[ip] = true
				}
			}
		}
	}
	return len(seen)
}

// ListHealthyForDevice returns healthy slots belonging to a specific device.
func (r *SlotRepository) ListHealthyForDevice(deviceAlias string) []*entity.Slot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*entity.Slot
	for _, s := range r.slots {
		if s.DeviceAlias == deviceAlias && s.Status == entity.SlotStatusHealthy {
			result = append(result, s)
		}
	}
	return result
}

// ListNamesForDevice returns slot names belonging to a specific device.
func (r *SlotRepository) ListNamesForDevice(deviceAlias string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for _, s := range r.slots {
		if s.DeviceAlias == deviceAlias {
			names = append(names, s.Name)
		}
	}
	return names
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

// ListAllNames returns the names of all slots currently tracked in memory.
func (r *SlotRepository) ListAllNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.slots))
	for name := range r.slots {
		names = append(names, name)
	}
	return names
}

