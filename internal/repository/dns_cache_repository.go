package repository

import (
	"container/list"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/riakgu/moxy/internal/entity"
)

const (
	NegativeEntry    = "NXAAAA"
	NegativeCacheTTL = 60 * time.Second
)

type dnsCacheKey struct {
	Nameserver  string
	NAT64Prefix string
}

type deviceCache struct {
	entries map[string]*list.Element
	order   *list.List
	hits    int64
	misses  int64
}

type dnsEntry struct {
	hostname  string
	ipv6      string
	expiresAt time.Time
}

type DNSCacheRepository struct {
	mu                  sync.Mutex
	caches              map[dnsCacheKey]*deviceCache
	maxEntriesPerDevice int
	log                 *slog.Logger
}

func NewDNSCacheRepository(log *slog.Logger, maxEntriesPerDevice int) *DNSCacheRepository {
	if maxEntriesPerDevice <= 0 {
		maxEntriesPerDevice = 10000
	}
	return &DNSCacheRepository{
		log:                 log,
		maxEntriesPerDevice: maxEntriesPerDevice,
		caches:              make(map[dnsCacheKey]*deviceCache),
	}
}

func (r *DNSCacheRepository) Lookup(nameserver, nat64Prefix, hostname string) (string, bool) {
	key := dnsCacheKey{Nameserver: nameserver, NAT64Prefix: nat64Prefix}

	r.mu.Lock()
	defer r.mu.Unlock()

	dc := r.caches[key]
	if dc == nil {
		dc = &deviceCache{
			entries: make(map[string]*list.Element),
			order:   list.New(),
		}
		r.caches[key] = dc
		atomic.AddInt64(&dc.misses, 1)
		return "", false
	}

	elem, ok := dc.entries[hostname]
	if !ok {
		atomic.AddInt64(&dc.misses, 1)
		return "", false
	}

	entry := elem.Value.(*dnsEntry)
	if time.Now().After(entry.expiresAt) {
		dc.order.Remove(elem)
		delete(dc.entries, hostname)
		atomic.AddInt64(&dc.misses, 1)
		return "", false
	}

	dc.order.MoveToFront(elem)
	atomic.AddInt64(&dc.hits, 1)
	return entry.ipv6, true
}

func (r *DNSCacheRepository) Store(nameserver, nat64Prefix, hostname, ipv6 string, expiresAt time.Time) {
	key := dnsCacheKey{Nameserver: nameserver, NAT64Prefix: nat64Prefix}

	r.mu.Lock()
	defer r.mu.Unlock()

	dc := r.caches[key]
	if dc == nil {
		dc = &deviceCache{
			entries: make(map[string]*list.Element),
			order:   list.New(),
		}
		r.caches[key] = dc
	}

	if elem, ok := dc.entries[hostname]; ok {
		dc.order.Remove(elem)
		delete(dc.entries, hostname)
	}

	entry := &dnsEntry{
		hostname:  hostname,
		ipv6:      ipv6,
		expiresAt: expiresAt,
	}
	elem := dc.order.PushFront(entry)
	dc.entries[hostname] = elem

	for dc.order.Len() > r.maxEntriesPerDevice {
		oldest := dc.order.Back()
		if oldest != nil {
			oldEntry := oldest.Value.(*dnsEntry)
			dc.order.Remove(oldest)
			delete(dc.entries, oldEntry.hostname)
		}
	}
}

func (r *DNSCacheRepository) Stats() []entity.DNSCacheStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	stats := make([]entity.DNSCacheStats, 0, len(r.caches))
	for key, dc := range r.caches {
		stats = append(stats, entity.DNSCacheStats{
			Nameserver:  key.Nameserver,
			NAT64Prefix: key.NAT64Prefix,
			Entries:     dc.order.Len(),
			Hits:        atomic.LoadInt64(&dc.hits),
			Misses:      atomic.LoadInt64(&dc.misses),
		})
	}
	return stats
}
