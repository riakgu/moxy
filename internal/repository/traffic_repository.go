//go:build linux

package repository

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
)

type TrafficRepository struct {
	Log        *slog.Logger
	mu         sync.RWMutex
	entries    map[entity.TrafficKey]*entity.TrafficEntry
	maxEntries int
}

func NewTrafficRepository(log *slog.Logger, maxEntries int) *TrafficRepository {
	if maxEntries <= 0 {
		maxEntries = 5000
	}
	return &TrafficRepository{
		Log:        log,
		entries:    make(map[entity.TrafficKey]*entity.TrafficEntry),
		maxEntries: maxEntries,
	}
}

// SetMaxEntries updates the max tracked entries limit at runtime.
func (r *TrafficRepository) SetMaxEntries(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxEntries = n
}

func (r *TrafficRepository) Record(key entity.TrafficKey) *entity.TrafficEntry {
	now := time.Now().UnixMilli()

	r.mu.RLock()
	entry, exists := r.entries[key]
	r.mu.RUnlock()

	if exists {
		atomic.AddInt64(&entry.ConnectionCount, 1)
		atomic.StoreInt64(&entry.LastSeenAt, now)
		return entry
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if entry, exists = r.entries[key]; exists {
		atomic.AddInt64(&entry.ConnectionCount, 1)
		atomic.StoreInt64(&entry.LastSeenAt, now)
		return entry
	}

	// Evict lowest connection count entry if at capacity
	if len(r.entries) >= r.maxEntries {
		r.evictLowest()
	}

	entry = &entity.TrafficEntry{
		TrafficKey:      key,
		ConnectionCount: 1,
		FirstSeenAt:     now,
		LastSeenAt:      now,
	}
	r.entries[key] = entry
	return entry
}

func (r *TrafficRepository) List() []*entity.TrafficEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*entity.TrafficEntry, 0, len(r.entries))
	for _, e := range r.entries {
		// Snapshot the entry to avoid races on the response
		snapshot := &entity.TrafficEntry{
			TrafficKey:        e.TrafficKey,
			ConnectionCount:   atomic.LoadInt64(&e.ConnectionCount),
			ActiveConnections: atomic.LoadInt64(&e.ActiveConnections),
			TxBytes:           atomic.LoadUint64(&e.TxBytes),
			RxBytes:           atomic.LoadUint64(&e.RxBytes),
			FirstSeenAt:       atomic.LoadInt64(&e.FirstSeenAt),
			LastSeenAt:        atomic.LoadInt64(&e.LastSeenAt),
		}
		result = append(result, snapshot)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ConnectionCount > result[j].ConnectionCount
	})

	return result
}

func (r *TrafficRepository) TotalByDevice(alias string) (rx, tx uint64) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, e := range r.entries {
		if e.DeviceAlias == alias {
			rx += atomic.LoadUint64(&e.RxBytes)
			tx += atomic.LoadUint64(&e.TxBytes)
		}
	}
	return rx, tx
}

func (r *TrafficRepository) evictLowest() {
	var lowestKey entity.TrafficKey
	var lowestCount int64 = -1

	for key, e := range r.entries {
		count := atomic.LoadInt64(&e.ConnectionCount)
		if lowestCount < 0 || count < lowestCount {
			lowestKey = key
			lowestCount = count
		}
	}

	if lowestCount >= 0 {
		delete(r.entries, lowestKey)
		r.Log.Debug("entry evicted", "domain", lowestKey.Domain, "port", lowestKey.Port, "connections", lowestCount)
	}
}
