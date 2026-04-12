//go:build linux

package repository

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
)

type SlotRepository struct {
	Log      *slog.Logger
	mu       sync.RWMutex
	slots    map[string]*entity.Slot
	maxSlots int
	freeList []int
	usedSet  map[int]bool
}

func NewSlotRepository(log *slog.Logger, maxSlots int) *SlotRepository {
	if maxSlots <= 0 {
		maxSlots = 1000
	}
	freeList := make([]int, maxSlots)
	for i := 0; i < maxSlots; i++ {
		freeList[i] = maxSlots - i // [maxSlots, ..., 2, 1] — pop gives 1 first
	}
	return &SlotRepository{
		Log:      log,
		slots:    make(map[string]*entity.Slot),
		maxSlots: maxSlots,
		freeList: freeList,
		usedSet:  make(map[int]bool),
	}
}

func (r *SlotRepository) NextSlotIndex() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.freeList) == 0 {
		return 0, fmt.Errorf("slot pool exhausted (max %d)", r.maxSlots)
	}
	idx := r.freeList[len(r.freeList)-1]
	r.freeList = r.freeList[:len(r.freeList)-1]
	r.usedSet[idx] = true
	return idx, nil
}

func (r *SlotRepository) releaseIndex(idx int) {
	if !r.usedSet[idx] {
		return
	}
	delete(r.usedSet, idx)
	r.freeList = append(r.freeList, idx)
}

func parseSlotIdx(name string) (int, bool) {
	if !strings.HasPrefix(name, "slot") {
		return 0, false
	}
	idx, err := strconv.Atoi(name[4:])
	if err != nil {
		return 0, false
	}
	return idx, true
}

func (r *SlotRepository) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.slots)
}

func (r *SlotRepository) Put(slot *entity.Slot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.slots[slot.Name] = slot
}

func (r *SlotRepository) Get(name string) (*entity.Slot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	slot, ok := r.slots[name]
	return slot, ok
}

func (r *SlotRepository) Delete(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.slots[name]; ok {
		delete(r.slots, name)
		if idx, ok := parseSlotIdx(name); ok {
			r.releaseIndex(idx)
		}
	}
}

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
			if idx, ok := parseSlotIdx(name); ok {
				r.releaseIndex(idx)
			}
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

// UniqueIPsByDevice returns the count of distinct IP pairs (exit points)
// across all healthy slots belonging to a device. Each slot's sorted
// PublicIPv4s is treated as one exit point — CGNAT dual-BIB counts as 1.
func (r *SlotRepository) UniqueIPsByDevice(deviceAlias string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[string]bool)
	for _, slot := range r.slots {
		if slot.DeviceAlias == deviceAlias && slot.Status == entity.SlotStatusHealthy {
			key := pairKey(slot.PublicIPv4s)
			if key != "" {
				seen[key] = true
			}
		}
	}
	return len(seen)
}

// pairKey returns a canonical string key for a set of IPs (sorted, comma-joined).
func pairKey(ips []string) string {
	filtered := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip != "" {
			filtered = append(filtered, ip)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	sorted := make([]string, len(filtered))
	copy(sorted, filtered)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

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

func (r *SlotRepository) IncrementConnections(name string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if slot, ok := r.slots[name]; ok {
		atomic.AddInt64(&slot.ActiveConnections, 1)
	}
}

func (r *SlotRepository) DecrementConnections(name string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if slot, ok := r.slots[name]; ok {
		atomic.AddInt64(&slot.ActiveConnections, -1)
	}
}

func (r *SlotRepository) ListAllNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.slots))
	for name := range r.slots {
		names = append(names, name)
	}
	return names
}

