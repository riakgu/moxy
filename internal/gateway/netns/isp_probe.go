//go:build linux

package netns

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
)

const (
	rfc7050Timeout    = 5 * time.Second
	rfc7050Host       = "ipv4only.arpa"
	rfc7050WellKnown1 = "192.0.0.170"
	rfc7050WellKnown2 = "192.0.0.171"
)

// Public DNS64 fallback servers. Tried when ADB DNS servers don't support DNS64.
var dns64Fallbacks = []string{
	"2001:4860:4860::64",   // Google Public DNS64 (primary)
	"2001:4860:4860::6464", // Google Public DNS64 (secondary)
	"2606:4700:4700::64",   // Cloudflare DNS64
}

// ISPProbe discovers ISP DNS64 nameserver and NAT64 prefix.
// It tests DNS64 capability on candidates provided by the caller (from ADB)
// and falls back to public DNS64 servers.
type ISPProbe struct {
	Log *logrus.Logger
}

// NewISPProbe creates a new ISPProbe.
func NewISPProbe(log *logrus.Logger) *ISPProbe {
	return &ISPProbe{Log: log}
}

// Probe tests DNS64 capability on candidates in priority order:
//  1. hintDNS — carrier-assigned DNS from the phone (via ADB, most reliable)
//  2. Public DNS64 fallbacks — Google and Cloudflare
//
// Returns the first candidate that passes the DNS64 test (ipv4only.arpa AAAA).
// Returns error if ALL candidates fail — device cannot work without DNS64.
func (p *ISPProbe) Probe(hintDNS []string) (*model.ISPProbeResult, error) {
	// Build priority-ordered candidate list
	candidates := make([]string, 0, len(hintDNS)+len(dns64Fallbacks))
	candidates = append(candidates, hintDNS...)
	candidates = append(candidates, dns64Fallbacks...)

	// Test DNS64 on each candidate until one works
	for _, ns := range candidates {
		prefix, err := p.discoverNAT64Prefix(ns)
		if err != nil {
			p.Log.Debugf("isp-probe: DNS64 test failed on %s: %v", ns, err)
			continue
		}
		p.Log.Infof("isp-probe: DNS64 verified on %s with prefix %s", ns, prefix)
		return &model.ISPProbeResult{
			Nameserver:  ns,
			NAT64Prefix: prefix,
		}, nil
	}

	return nil, fmt.Errorf("no DNS64-capable server found (tested %d candidates)", len(candidates))
}

// discoverNAT64Prefix resolves ipv4only.arpa AAAA via the given DNS64
// nameserver and extracts the NAT64 /96 prefix from the response.
// Per RFC 7050, the well-known IPv4 addresses 192.0.0.170 and 192.0.0.171
// are embedded by the DNS64 server, revealing the prefix.
func (p *ISPProbe) discoverNAT64Prefix(nameserver string) (string, error) {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return net.DialTimeout("udp6", net.JoinHostPort(nameserver, "53"), rfc7050Timeout)
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), rfc7050Timeout)
	defer cancel()

	ips, err := resolver.LookupHost(ctx, rfc7050Host)
	if err != nil {
		return "", fmt.Errorf("resolve %s via %s: %w", rfc7050Host, nameserver, err)
	}

	// Find a AAAA record that contains a well-known IPv4 address
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil || ip.To4() != nil {
			continue // skip IPv4 results
		}

		// NAT64 address is prefix::/96 + IPv4 (4 bytes)
		// The IPv4 part occupies the last 4 bytes of the 16-byte IPv6 address
		ipBytes := ip.To16()
		if ipBytes == nil {
			continue
		}

		// Check if last 4 bytes match well-known IPv4 192.0.0.170 or 192.0.0.171
		v4Part := ipBytes[12:16]
		wk1 := net.ParseIP(rfc7050WellKnown1).To4()
		wk2 := net.ParseIP(rfc7050WellKnown2).To4()

		if bytesEqual(v4Part, wk1) || bytesEqual(v4Part, wk2) {
			// Extract /96 prefix (first 12 bytes)
			prefix := make(net.IP, 16)
			copy(prefix, ipBytes[:12])
			// Format as compressed IPv6 prefix string with trailing ::
			prefixStr := formatNAT64Prefix(prefix)
			return prefixStr, nil
		}
	}

	return "", fmt.Errorf("no NAT64 prefix found in %s AAAA response from %s", rfc7050Host, nameserver)
}

// formatNAT64Prefix formats a 12-byte prefix as an IPv6 /96 prefix string.
// Example: [fd 00 00 aa 00 bb 20 90 00 00 00 00 00 00 00 00] → "fd00:aa:bb:2090::"
func formatNAT64Prefix(ip net.IP) string {
	// Zero out the last 4 bytes to get a clean /96 prefix
	full := make(net.IP, 16)
	copy(full, ip[:12])
	// net.IP.String() will compress trailing zeros
	s := full.String()
	// Ensure trailing :: for prefix notation
	if len(s) > 2 && s[len(s)-1] != ':' {
		s += "::"
	}
	return s
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
