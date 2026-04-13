//go:build linux

package netns

import (
	"fmt"
	"net"
	"syscall"
	"time"
	"log/slog"

	mdns "github.com/miekg/dns"
	"github.com/riakgu/moxy/internal/model"
	"golang.org/x/sys/unix"
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

func (p *ISPProbe) Probe(hintDNS []string, iface string) (*model.ISPProbeResult, error) {
	candidates := make([]string, 0, len(hintDNS)+len(dns64Fallbacks))
	candidates = append(candidates, hintDNS...)
	candidates = append(candidates, dns64Fallbacks...)

	for _, ns := range candidates {
		prefix, err := p.discoverNAT64Prefix(ns, iface)
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

func (p *ISPProbe) discoverNAT64Prefix(nameserver, iface string) (string, error) {
	dnsAddr := net.JoinHostPort(nameserver, "53")

	// Use miekg/dns — runs synchronously, no goroutine escape.
	// SO_BINDTODEVICE forces DNS through tethering interface, not LAN.
	client := &mdns.Client{
		Net:     "udp6",
		Timeout: rfc7050Timeout,
		Dialer: &net.Dialer{
			Timeout: rfc7050Timeout,
			Control: func(network, address string, c syscall.RawConn) error {
				if iface == "" {
					return nil
				}
				var sErr error
				if err := c.Control(func(fd uintptr) {
					sErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface)
				}); err != nil {
					return err
				}
				return sErr
			},
		},
	}

	msg := new(mdns.Msg)
	msg.SetQuestion(mdns.Fqdn(rfc7050Host), mdns.TypeAAAA)

	resp, _, err := client.Exchange(msg, dnsAddr)
	if err != nil {
		return "", fmt.Errorf("resolve %s via %s: %w", rfc7050Host, nameserver, err)
	}

	for _, ans := range resp.Answer {
		aaaa, ok := ans.(*mdns.AAAA)
		if !ok {
			continue
		}
		ip := aaaa.AAAA

		// NAT64 address is prefix::/96 + IPv4 (4 bytes)
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
