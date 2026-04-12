//go:build linux

package netns

import (
	"fmt"
	"net"
	"strings"
	"time"
	"log/slog"

	"github.com/riakgu/moxy/internal/repository"
	"github.com/miekg/dns"
)

type CacheConfig struct {
	MinTTL time.Duration
	MaxTTL time.Duration
}

type DNSCache interface {
	Lookup(nameserver, nat64Prefix, hostname string) (string, bool)
	Store(nameserver, nat64Prefix, hostname, ipv6 string, expiresAt time.Time)
}

type CachingResolver struct {
	log    *slog.Logger
	config CacheConfig
	cache  DNSCache
}

func NewCachingResolver(log *slog.Logger, cache DNSCache, config CacheConfig) *CachingResolver {
	if config.MinTTL <= 0 {
		config.MinTTL = 30 * time.Second
	}
	if config.MaxTTL <= 0 {
		config.MaxTTL = 300 * time.Second
	}
	return &CachingResolver{
		log:    log,
		config: config,
		cache:  cache,
	}
}

func (cr *CachingResolver) Resolve(hostname, nameserver, nat64Prefix string) (string, error) {
	if ipv6, ok := cr.cache.Lookup(nameserver, nat64Prefix, hostname); ok {
		return ipv6, nil
	}

	ipv6, ttl, err := cr.resolveDNS(hostname, nameserver, nat64Prefix)
	if err != nil {
		return "", err
	}

	if ttl < cr.config.MinTTL {
		ttl = cr.config.MinTTL
	}
	if ttl > cr.config.MaxTTL {
		ttl = cr.config.MaxTTL
	}

	cr.cache.Store(nameserver, nat64Prefix, hostname, ipv6, time.Now().Add(ttl))
	return ipv6, nil
}

func (cr *CachingResolver) ResolveNative(hostname, nameserver, nat64Prefix string) (string, error) {
	if ipv6, ok := cr.cache.Lookup(nameserver, "native", hostname); ok {
		if ipv6 == repository.NegativeEntry {
			return "", fmt.Errorf("no native AAAA for %s (cached)", hostname)
		}
		return ipv6, nil
	}

	ipv6, ttl, err := cr.resolveDNSNative(hostname, nameserver, nat64Prefix)

	cacheValue := ipv6
	if err != nil {
		cacheValue = repository.NegativeEntry
		ttl = repository.NegativeCacheTTL
	} else {
		if ttl < cr.config.MinTTL {
			ttl = cr.config.MinTTL
		}
		if ttl > cr.config.MaxTTL {
			ttl = cr.config.MaxTTL
		}
	}

	cr.cache.Store(nameserver, "native", hostname, cacheValue, time.Now().Add(ttl))

	if err != nil {
		return "", err
	}
	return ipv6, nil
}

func (cr *CachingResolver) resolveDNS(hostname, nameserver, nat64Prefix string) (string, time.Duration, error) {
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

	for _, ans := range resp.Answer {
		if aaaa, ok := ans.(*dns.AAAA); ok {
			ip := aaaa.AAAA.String()
			if strings.Contains(ip, ":") {
				ttl := time.Duration(ans.Header().Ttl) * time.Second
				if ttl == 0 {
					ttl = cr.config.MinTTL
				}
				return ip, ttl, nil
			}
		}
	}

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
