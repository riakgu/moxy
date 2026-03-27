package usecase

import (
	"net"
	"sort"
	"sync"
	"time"

	"github.com/riakgu/moxy/internal/model"
)

type destinationEntry struct {
	Domain        string
	Connections   int64
	BytesSent     int64
	BytesReceived int64
	LastAccessed  int64
}

// DestinationTracker tracks which domains are accessed through the proxy.
// It stores aggregated stats per domain (across all slots).
// Thread-safe for concurrent use from proxy handlers.
type DestinationTracker struct {
	mu      sync.RWMutex
	domains map[string]*destinationEntry
	maxSize int
}

func NewDestinationTracker(maxSize int) *DestinationTracker {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &DestinationTracker{
		domains: make(map[string]*destinationEntry),
		maxSize: maxSize,
	}
}

// Record stores a completed connection's stats.
func (t *DestinationTracker) Record(targetAddr string, bytesSent, bytesReceived int64) {
	domain := extractDomain(targetAddr)
	if domain == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.domains[domain]
	if !ok {
		// Evict oldest if at capacity
		if len(t.domains) >= t.maxSize {
			t.evictOldest()
		}
		entry = &destinationEntry{Domain: domain}
		t.domains[domain] = entry
	}

	entry.Connections++
	entry.BytesSent += bytesSent
	entry.BytesReceived += bytesReceived
	entry.LastAccessed = time.Now().Unix()
}

// GetStats returns destination stats sorted by connection count (descending).
func (t *DestinationTracker) GetStats(limit int) *model.DestinationStatsResponse {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := make([]model.DestinationStat, 0, len(t.domains))
	for _, e := range t.domains {
		stats = append(stats, model.DestinationStat{
			Domain:        e.Domain,
			Connections:   e.Connections,
			BytesSent:     e.BytesSent,
			BytesReceived: e.BytesReceived,
			LastAccessed:  e.LastAccessed,
		})
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Connections > stats[j].Connections
	})

	if limit > 0 && len(stats) > limit {
		stats = stats[:limit]
	}

	return &model.DestinationStatsResponse{
		TotalDomains: len(t.domains),
		Destinations: stats,
	}
}

func (t *DestinationTracker) evictOldest() {
	var oldestKey string
	var oldestTime int64 = 1<<63 - 1

	for k, e := range t.domains {
		if e.LastAccessed < oldestTime {
			oldestTime = e.LastAccessed
			oldestKey = k
		}
	}

	if oldestKey != "" {
		delete(t.domains, oldestKey)
	}
}

// extractDomain extracts the hostname from a "host:port" address.
func extractDomain(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
