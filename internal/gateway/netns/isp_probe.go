//go:build linux

package netns

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/mdlayher/ndp"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
)

const (
	rdnssTimeout      = 5 * time.Second
	rfc7050Timeout    = 5 * time.Second
	rfc7050Host       = "ipv4only.arpa"
	rfc7050WellKnown1 = "192.0.0.170"
	rfc7050WellKnown2 = "192.0.0.171"

	// Default DNS64 and NAT64 values — used as fallback when discovery fails.
	// These are the single source of truth for defaults across the entire package.
	defaultDNS64Server = "2001:4860:4860::6464"
	defaultNAT64Prefix = "64:ff9b::"
)

// ISPProbe discovers ISP DNS64 nameserver and NAT64 prefix from the network.
// It uses RDNSS from Router Advertisements (RFC 8106) and NAT64 prefix
// discovery via ipv4only.arpa (RFC 7050).
// Probe always returns usable values — defaults if discovery fails.
type ISPProbe struct {
	Log *logrus.Logger
}

// NewISPProbe creates a new ISPProbe.
func NewISPProbe(log *logrus.Logger) *ISPProbe {
	return &ISPProbe{Log: log}
}

// Probe discovers the ISP's DNS64 nameserver and NAT64 prefix on the given
// tethering interface. Always returns usable values — falls back to defaults
// if discovery fails.
func (p *ISPProbe) Probe(ifaceName string) (*model.ISPProbeResult, error) {
	// Step 1: Discover DNS64 nameserver via RDNSS
	nameserver, err := p.discoverRDNSS(ifaceName)
	if err != nil {
		p.Log.Warnf("isp-probe: RDNSS discovery failed on %s: %v — using defaults", ifaceName, err)
		return &model.ISPProbeResult{
			Nameserver:  defaultDNS64Server,
			NAT64Prefix: defaultNAT64Prefix,
		}, nil
	}

	p.Log.Infof("isp-probe: RDNSS discovered nameserver %s on %s", nameserver, ifaceName)

	// Step 2: Discover NAT64 prefix via RFC 7050
	prefix, err := p.discoverNAT64Prefix(nameserver)
	if err != nil {
		// Partial success: we got the nameserver but not the prefix
		p.Log.Warnf("isp-probe: NAT64 prefix discovery failed via %s: %v — using well-known prefix", nameserver, err)
		return &model.ISPProbeResult{
			Nameserver:  nameserver,
			NAT64Prefix: defaultNAT64Prefix,
		}, nil
	}

	p.Log.Infof("isp-probe: NAT64 prefix discovered %s via %s", prefix, nameserver)

	return &model.ISPProbeResult{
		Nameserver:  nameserver,
		NAT64Prefix: prefix,
	}, nil
}

// discoverRDNSS sends a Router Solicitation on the interface and parses
// the Router Advertisement for RDNSS (Recursive DNS Server) option.
func (p *ISPProbe) discoverRDNSS(ifaceName string) (string, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return "", fmt.Errorf("interface %s not found: %w", ifaceName, err)
	}

	conn, _, err := ndp.Listen(iface, ndp.LinkLocal)
	if err != nil {
		return "", fmt.Errorf("listen NDP on %s: %w", ifaceName, err)
	}
	defer conn.Close()

	// Send Router Solicitation
	rs := &ndp.RouterSolicitation{}
	allRouters := netip.MustParseAddr("ff02::2")
	if err := conn.WriteTo(rs, nil, allRouters); err != nil {
		return "", fmt.Errorf("send RS on %s: %w", ifaceName, err)
	}

	// Wait for Router Advertisement with RDNSS
	if err := conn.SetReadDeadline(time.Now().Add(rdnssTimeout)); err != nil {
		return "", fmt.Errorf("set read deadline: %w", err)
	}

	for {
		msg, _, _, err := conn.ReadFrom()
		if err != nil {
			return "", fmt.Errorf("read RA on %s: %w", ifaceName, err)
		}

		ra, ok := msg.(*ndp.RouterAdvertisement)
		if !ok {
			continue
		}

		// Extract RDNSS option
		for _, opt := range ra.Options {
			rdnss, ok := opt.(*ndp.RecursiveDNSServer)
			if !ok || len(rdnss.Servers) == 0 {
				continue
			}
			// Return first DNS server
			return rdnss.Servers[0].String(), nil
		}

		// RA received but no RDNSS option
		return "", fmt.Errorf("RA on %s has no RDNSS option", ifaceName)
	}
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
