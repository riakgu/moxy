//go:build linux

package netns

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

const nat64Prefix = "64:ff9b::"

// SetnsDialer dials targets through network namespaces using the setns(2) syscall.
// This avoids the overhead of spawning a subprocess per connection.
type SetnsDialer struct {
	Log         *logrus.Logger
	DNS64Server string
}

// NewSetnsDialer creates a new SetnsDialer.
// dns64Server is the DNS64 nameserver address (e.g., "2001:4860:4860::6464")
// used for resolving domain names inside namespaces.
func NewSetnsDialer(log *logrus.Logger, dns64Server string) *SetnsDialer {
	return &SetnsDialer{
		Log:         log,
		DNS64Server: dns64Server,
	}
}

// toNAT64 converts an IPv4 address to its NAT64 representation
// using the well-known prefix 64:ff9b::/96.
func toNAT64(ipv4 net.IP) string {
	v4 := ipv4.To4()
	return fmt.Sprintf("%s%02x%02x:%02x%02x", nat64Prefix, v4[0], v4[1], v4[2], v4[3])
}

// Dial connects to addr through the network namespace identified by slotName.
// It uses setns(2) to enter the namespace in-process, avoiding subprocess overhead.
//
// For raw IPv4 addresses, NAT64 conversion is applied automatically.
// For domain names, DNS resolution uses a native net.Resolver inside the namespace.
func (d *SetnsDialer) Dial(slotName string, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	// Resolve the target address to a NAT64 IPv6 address
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() != nil {
		// Raw IPv4 → NAT64
		host = toNAT64(ip)
	} else if ip == nil {
		// Domain name — resolve DNS64 inside namespace using native resolver
		resolved, err := d.resolveInNamespace(slotName, host)
		if err != nil {
			return nil, fmt.Errorf("DNS64 resolve for %s: %w", slotName, err)
		}
		host = resolved
	}
	// At this point, host is always an IPv6 address (global or NAT64)
	target := net.JoinHostPort(host, port)

	// Pin goroutine to OS thread — required because setns operates on the calling thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save current (host) network namespace
	origNs, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return nil, fmt.Errorf("open host namespace: %w", err)
	}
	defer origNs.Close()

	// Open target namespace
	targetNs, err := os.Open("/var/run/netns/" + slotName)
	if err != nil {
		return nil, fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer targetNs.Close()

	// Enter target namespace
	if err := unix.Setns(int(targetNs.Fd()), unix.CLONE_NEWNET); err != nil {
		return nil, fmt.Errorf("setns to %s: %w", slotName, err)
	}

	// Pure TCP connect — no DNS resolution needed, host is already an IPv6 address
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, dialErr := dialer.DialContext(context.Background(), "tcp6", target)

	// Always restore host namespace, even if dial failed
	if restoreErr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); restoreErr != nil {
		if conn != nil {
			conn.Close()
		}
		return nil, fmt.Errorf("restore host namespace: %w", restoreErr)
	}

	if dialErr != nil {
		return nil, fmt.Errorf("dial %s via %s: %w", addr, slotName, dialErr)
	}

	return conn, nil
}

// resolveInNamespace resolves a hostname to an IPv6 address inside a network namespace
// using Go's native net.Resolver with setns. No subprocess needed.
func (d *SetnsDialer) resolveInNamespace(slotName string, hostname string) (string, error) {
	dnsServer := d.DNS64Server
	if dnsServer == "" {
		dnsServer = "2001:4860:4860::6464"
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save host namespace
	origNs, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return "", fmt.Errorf("open host namespace: %w", err)
	}
	defer origNs.Close()

	// Enter slot namespace
	targetNs, err := os.Open("/var/run/netns/" + slotName)
	if err != nil {
		return "", fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer targetNs.Close()

	if err := unix.Setns(int(targetNs.Fd()), unix.CLONE_NEWNET); err != nil {
		return "", fmt.Errorf("setns to %s: %w", slotName, err)
	}

	// Resolve using native Go resolver with DNS64 server
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return net.DialTimeout("udp6", net.JoinHostPort(dnsServer, "53"), 5*time.Second)
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ips, lookupErr := resolver.LookupHost(ctx, hostname)

	// Always restore host namespace
	if restoreErr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); restoreErr != nil {
		return "", fmt.Errorf("restore host namespace: %w", restoreErr)
	}

	if lookupErr != nil {
		return "", fmt.Errorf("lookup %s: %w", hostname, lookupErr)
	}

	// Pick first IPv6 from results
	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed != nil && strings.Contains(ip, ":") {
			return parsed.String(), nil
		}
	}

	return "", fmt.Errorf("no AAAA record for %s via DNS64", hostname)
}
