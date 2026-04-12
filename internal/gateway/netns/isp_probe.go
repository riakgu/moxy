//go:build linux

package netns

import (
	"context"
	"fmt"
	"net"
	"time"
	"log/slog"

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
	"2001:4860:4860::64",   // Google Public DNS64 
	"2001:4860:4860::6464", // Google Public DNS64
	"2606:4700:4700::64",   // Cloudflare DNS64
}

// ISPProbe discovers ISP DNS64 nameserver and NAT64 prefix.
type ISPProbe struct {
	Log *slog.Logger
}

func NewISPProbe(log *slog.Logger) *ISPProbe {
	return &ISPProbe{Log: log}
}

func (p *ISPProbe) Probe(hintDNS []string) (*model.ISPProbeResult, error) {
	candidates := make([]string, 0, len(hintDNS)+len(dns64Fallbacks))
	candidates = append(candidates, hintDNS...)
	candidates = append(candidates, dns64Fallbacks...)

	for _, ns := range candidates {
		prefix, err := p.discoverNAT64Prefix(ns)
		if err != nil {
			p.Log.Debug("dns64 test failed", "nameserver", ns, "error", err)
			continue
		}
		p.Log.Info("dns64 verified", "nameserver", ns, "prefix", prefix)
		return &model.ISPProbeResult{
			Nameserver:  ns,
			NAT64Prefix: prefix,
		}, nil
	}

	return nil, fmt.Errorf("no DNS64-capable server found (tested %d candidates)", len(candidates))
}

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

		v4Part := ipBytes[12:16]
		wk1 := net.ParseIP(rfc7050WellKnown1).To4()
		wk2 := net.ParseIP(rfc7050WellKnown2).To4()

		if bytesEqual(v4Part, wk1) || bytesEqual(v4Part, wk2) {
			prefix := make(net.IP, 16)
			copy(prefix, ipBytes[:12])
			prefixStr := formatNAT64Prefix(prefix)
			return prefixStr, nil
		}
	}

	return "", fmt.Errorf("no NAT64 prefix found in %s AAAA response from %s", rfc7050Host, nameserver)
}

// formatNAT64Prefix formats a 12-byte prefix as an IPv6 /96 prefix string.
// Example: [fd 00 00 aa 00 bb 20 90 00 00 00 00 00 00 00 00] → "fd00:aa:bb:2090::"
func formatNAT64Prefix(ip net.IP) string {
	full := make(net.IP, 16)
	copy(full, ip[:12])
	s := full.String()
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
