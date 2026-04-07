//go:build linux

package netns

import (
	"container/list"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"log/slog"

	"github.com/miekg/dns"
)

// CacheConfig holds DNS cache configuration.
type CacheConfig struct {
	MaxEntriesPerDevice int           // LRU cap per device cache (default: 10000)
	MinTTL              time.Duration // Floor for DNS TTLs (default: 30s)
	MaxTTL              time.Duration // Ceiling for DNS TTLs (default: 300s)
}

// DeviceCacheStats holds statistics for a single device's DNS cache.
type DeviceCacheStats struct {
	Nameserver  string
	NAT64Prefix string
	Entries     int
	Hits        int64
	Misses      int64
}

// CachingResolver caches DNS64 resolutions per-device.
// Keyed by (nameserver, NAT64Prefix) so devices sharing the same ISP config share a cache.
// Thread-safe. Must be called while the OS thread is inside the target network namespace.
type CachingResolver struct {
	log    *slog.Logger
	config CacheConfig
	mu     sync.Mutex
	caches map[deviceCacheKey]*deviceCache
}

type deviceCacheKey struct {
	Nameserver  string
	NAT64Prefix string
}

type deviceCache struct {
	entries map[string]*list.Element // hostname → list element
	order   *list.List              // front = most recently used
	hits    int64
	misses  int64
}

type dnsEntry struct {
	hostname  string
	ipv6      string
	expiresAt time.Time
}

// negativeEntry is stored as ipv6 value to indicate "no native AAAA exists"
const negativeEntry = "NXAAAA"

// negativeCacheTTL is how long to cache "no native AAAA" results
const negativeCacheTTL = 60 * time.Second

// NewCachingResolver creates a new CachingResolver with the given config.
// Zero-value config fields are replaced with sensible defaults.
func NewCachingResolver(log *slog.Logger, config CacheConfig) *CachingResolver {
	if config.MaxEntriesPerDevice <= 0 {
		config.MaxEntriesPerDevice = 10000
	}
	if config.MinTTL <= 0 {
		config.MinTTL = 30 * time.Second
	}
	if config.MaxTTL <= 0 {
		config.MaxTTL = 300 * time.Second
	}
	return &CachingResolver{
		log:    log,
		config: config,
		caches: make(map[deviceCacheKey]*deviceCache),
	}
}

// Resolve resolves a hostname to an IPv6 address via DNS64, using a per-device cache.
// Must be called while the calling OS thread is inside the target network namespace
// (the DNS64 nameserver is only reachable through the slot's network).
//
// On cache hit (and not expired), returns the cached IPv6 address immediately.
// On cache miss or expiry, queries the DNS64 server via miekg/dns, caches the result
// with the response TTL (clamped to [MinTTL, MaxTTL]), and returns the IPv6 address.
func (cr *CachingResolver) Resolve(hostname, nameserver, nat64Prefix string) (string, error) {
	key := deviceCacheKey{Nameserver: nameserver, NAT64Prefix: nat64Prefix}

	cr.mu.Lock()
	dc := cr.caches[key]
	if dc == nil {
		dc = &deviceCache{
			entries: make(map[string]*list.Element),
			order:   list.New(),
		}
		cr.caches[key] = dc
	}

	// Check cache
	if elem, ok := dc.entries[hostname]; ok {
		entry := elem.Value.(*dnsEntry)
		if time.Now().Before(entry.expiresAt) {
			// Cache hit — move to front (most recently used)
			dc.order.MoveToFront(elem)
			atomic.AddInt64(&dc.hits, 1)
			ipv6 := entry.ipv6
			cr.mu.Unlock()
			return ipv6, nil
		}
		// Expired — remove from cache
		dc.order.Remove(elem)
		delete(dc.entries, hostname)
	}
	atomic.AddInt64(&dc.misses, 1)
	cr.mu.Unlock()

	// Cache miss — resolve via DNS64
	ipv6, ttl, err := cr.resolveDNS(hostname, nameserver, nat64Prefix)
	if err != nil {
		return "", err
	}

	// Clamp TTL
	if ttl < cr.config.MinTTL {
		ttl = cr.config.MinTTL
	}
	if ttl > cr.config.MaxTTL {
		ttl = cr.config.MaxTTL
	}

	// Store in cache
	cr.mu.Lock()
	// Re-check: the device cache should still exist (we created it above)
	dc = cr.caches[key]
	if dc != nil {
		// Remove old entry if it was re-added concurrently
		if elem, ok := dc.entries[hostname]; ok {
			dc.order.Remove(elem)
			delete(dc.entries, hostname)
		}

		entry := &dnsEntry{
			hostname:  hostname,
			ipv6:      ipv6,
			expiresAt: time.Now().Add(ttl),
		}
		elem := dc.order.PushFront(entry)
		dc.entries[hostname] = elem

		// LRU eviction
		for dc.order.Len() > cr.config.MaxEntriesPerDevice {
			oldest := dc.order.Back()
			if oldest != nil {
				oldEntry := oldest.Value.(*dnsEntry)
				dc.order.Remove(oldest)
				delete(dc.entries, oldEntry.hostname)
			}
		}
	}
	cr.mu.Unlock()

	return ipv6, nil
}

// Stats returns cache statistics for all device caches.
func (cr *CachingResolver) Stats() []DeviceCacheStats {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	stats := make([]DeviceCacheStats, 0, len(cr.caches))
	for key, dc := range cr.caches {
		stats = append(stats, DeviceCacheStats{
			Nameserver:  key.Nameserver,
			NAT64Prefix: key.NAT64Prefix,
			Entries:     dc.order.Len(),
			Hits:        atomic.LoadInt64(&dc.hits),
			Misses:      atomic.LoadInt64(&dc.misses),
		})
	}
	return stats
}

// resolveDNS queries the DNS64 nameserver for AAAA records using miekg/dns.
// Returns the first IPv6 address and the response TTL.
// Must be called while inside the target network namespace.
func (cr *CachingResolver) resolveDNS(hostname, nameserver, nat64Prefix string) (string, time.Duration, error) {
	// Ensure hostname is FQDN for DNS wire format
	fqdn := dns.Fqdn(hostname)

	msg := new(dns.Msg)
	msg.SetQuestion(fqdn, dns.TypeAAAA)
	msg.RecursionDesired = true

	client := &dns.Client{
		Net:     "udp6",
		Timeout: 5 * time.Second,
	}

	serverAddr := net.JoinHostPort(nameserver, "53")
	resp, _, err := client.Exchange(msg, serverAddr)
	if err != nil {
		return "", 0, fmt.Errorf("DNS query %s via %s: %w", hostname, nameserver, err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		return "", 0, fmt.Errorf("DNS query %s via %s: rcode %s", hostname, nameserver, dns.RcodeToString[resp.Rcode])
	}

	// Find first AAAA answer
	for _, ans := range resp.Answer {
		if aaaa, ok := ans.(*dns.AAAA); ok {
			ip := aaaa.AAAA.String()
			if strings.Contains(ip, ":") {
				ttl := time.Duration(ans.Header().Ttl) * time.Second
				if ttl == 0 {
					ttl = cr.config.MinTTL // fallback for 0-TTL responses
				}
				return ip, ttl, nil
			}
		}
	}

	// Fallback: carrier DNS64 failed to synthesize AAAA — query A record and manually synthesize NAT64 address
	msgA := new(dns.Msg)
	msgA.SetQuestion(fqdn, dns.TypeA)
	msgA.RecursionDesired = true

	respA, _, errA := client.Exchange(msgA, serverAddr)
	if errA == nil && respA.Rcode == dns.RcodeSuccess {
		for _, ans := range respA.Answer {
			if a, ok := ans.(*dns.A); ok {
				v4 := a.A.To4()
				if v4 != nil {
					synthesized := fmt.Sprintf("%s%02x%02x:%02x%02x", nat64Prefix, v4[0], v4[1], v4[2], v4[3])
					ttl := time.Duration(ans.Header().Ttl) * time.Second
					if ttl == 0 {
						ttl = cr.config.MinTTL
					}
					cr.log.Debug("dns64 fallback synthesized", "hostname", hostname, "ipv6", synthesized, "nameserver", nameserver)
					return synthesized, ttl, nil
				}
			}
		}
	}

	return "", 0, fmt.Errorf("no AAAA or A record for %s via %s", hostname, nameserver)
}

// ResolveNative resolves a hostname to a genuine IPv6 address (not DNS64-synthesized).
// Uses a separate cache key space ("native") to avoid collisions with DNS64 results.
// Caches negative results (no native AAAA) for 60s to avoid repeated failed lookups.
// Must be called while inside the target network namespace.
func (cr *CachingResolver) ResolveNative(hostname, nameserver, nat64Prefix string) (string, error) {
	key := deviceCacheKey{Nameserver: nameserver, NAT64Prefix: "native"}

	cr.mu.Lock()
	dc := cr.caches[key]
	if dc == nil {
		dc = &deviceCache{
			entries: make(map[string]*list.Element),
			order:   list.New(),
		}
		cr.caches[key] = dc
	}

	// Check cache
	if elem, ok := dc.entries[hostname]; ok {
		entry := elem.Value.(*dnsEntry)
		if time.Now().Before(entry.expiresAt) {
			dc.order.MoveToFront(elem)
			atomic.AddInt64(&dc.hits, 1)
			ipv6 := entry.ipv6
			cr.mu.Unlock()
			if ipv6 == negativeEntry {
				return "", fmt.Errorf("no native AAAA for %s (cached)", hostname)
			}
			return ipv6, nil
		}
		dc.order.Remove(elem)
		delete(dc.entries, hostname)
	}
	atomic.AddInt64(&dc.misses, 1)
	cr.mu.Unlock()

	// Cache miss — resolve natively
	ipv6, ttl, err := cr.resolveDNSNative(hostname, nameserver, nat64Prefix)

	// Determine what to cache
	cacheValue := ipv6
	if err != nil {
		// Negative cache: remember "no native AAAA" to avoid repeated lookups
		cacheValue = negativeEntry
		ttl = negativeCacheTTL
	} else {
		// Clamp TTL for positive results
		if ttl < cr.config.MinTTL {
			ttl = cr.config.MinTTL
		}
		if ttl > cr.config.MaxTTL {
			ttl = cr.config.MaxTTL
		}
	}

	// Store in cache
	cr.mu.Lock()
	dc = cr.caches[key]
	if dc != nil {
		if elem, ok := dc.entries[hostname]; ok {
			dc.order.Remove(elem)
			delete(dc.entries, hostname)
		}

		entry := &dnsEntry{
			hostname:  hostname,
			ipv6:      cacheValue,
			expiresAt: time.Now().Add(ttl),
		}
		elem := dc.order.PushFront(entry)
		dc.entries[hostname] = elem

		for dc.order.Len() > cr.config.MaxEntriesPerDevice {
			oldest := dc.order.Back()
			if oldest != nil {
				oldEntry := oldest.Value.(*dnsEntry)
				dc.order.Remove(oldest)
				delete(dc.entries, oldEntry.hostname)
			}
		}
	}
	cr.mu.Unlock()

	if err != nil {
		return "", err
	}
	return ipv6, nil
}

// resolveDNSNative queries for real AAAA records (not DNS64-synthesized).
// Filters out any AAAA starting with the NAT64 prefix.
// Returns error if no genuine AAAA found.
func (cr *CachingResolver) resolveDNSNative(hostname, nameserver, nat64Prefix string) (string, time.Duration, error) {
	fqdn := dns.Fqdn(hostname)

	msg := new(dns.Msg)
	msg.SetQuestion(fqdn, dns.TypeAAAA)
	msg.RecursionDesired = true

	client := &dns.Client{
		Net:     "udp6",
		Timeout: 5 * time.Second,
	}

	serverAddr := net.JoinHostPort(nameserver, "53")
	resp, _, err := client.Exchange(msg, serverAddr)
	if err != nil {
		return "", 0, fmt.Errorf("native DNS query %s via %s: %w", hostname, nameserver, err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		return "", 0, fmt.Errorf("native DNS query %s via %s: rcode %s", hostname, nameserver, dns.RcodeToString[resp.Rcode])
	}

	// Find first AAAA that is NOT a DNS64-synthesized address
	for _, ans := range resp.Answer {
		if aaaa, ok := ans.(*dns.AAAA); ok {
			ip := aaaa.AAAA.String()
			if strings.Contains(ip, ":") && !strings.HasPrefix(ip, nat64Prefix) {
				ttl := time.Duration(ans.Header().Ttl) * time.Second
				if ttl == 0 {
					ttl = cr.config.MinTTL
				}
				return ip, ttl, nil
			}
		}
	}

	return "", 0, fmt.Errorf("no native AAAA record for %s (all results are DNS64-synthesized or empty)", hostname)
}
