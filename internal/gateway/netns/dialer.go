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

const defaultNAT64Prefix = "64:ff9b::"
const defaultDNS64Server = "2001:4860:4860::6464"

// SetnsDialer dials targets through network namespaces using the setns(2) syscall.
// Stateless — all ISP config (nameserver, NAT64 prefix) is provided per call.
type SetnsDialer struct {
	Log *logrus.Logger
}

// NewSetnsDialer creates a new SetnsDialer.
func NewSetnsDialer(log *logrus.Logger) *SetnsDialer {
	return &SetnsDialer{Log: log}
}

// toNAT64 converts an IPv4 address to its NAT64 representation
// using the provided prefix. Falls back to well-known 64:ff9b:: if empty.
func toNAT64(ipv4 net.IP, prefix string) string {
	if prefix == "" {
		prefix = defaultNAT64Prefix
	}
	v4 := ipv4.To4()
	return fmt.Sprintf("%s%02x%02x:%02x%02x", prefix, v4[0], v4[1], v4[2], v4[3])
}

// Dial connects to addr through the network namespace identified by slotName.
// It uses setns(2) to enter the namespace in-process, performing DNS resolution
// and TCP connect in a single namespace entry.
//
// nameserver is the DNS64 server for domain resolution (falls back to Google DNS64 if empty).
// nat64Prefix is the NAT64 /96 prefix for IPv4→IPv6 translation (falls back to 64:ff9b:: if empty).
func (d *SetnsDialer) Dial(slotName string, addr string, nameserver string, nat64Prefix string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	// Apply fallbacks
	if nameserver == "" {
		nameserver = defaultDNS64Server
	}
	if nat64Prefix == "" {
		nat64Prefix = defaultNAT64Prefix
	}

	// Check if host is a raw IPv4 — convert to NAT64 before entering namespace
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() != nil {
		host = toNAT64(ip, nat64Prefix)
	}

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

	// --- Inside namespace: resolve (if needed) + connect ---

	// If host is a domain name, resolve via DNS64 inside the namespace
	if ip == nil {
		resolved, err := resolveDNS64(host, nameserver)
		if err != nil {
			// Restore host namespace before returning error
			unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET)
			return nil, fmt.Errorf("DNS64 resolve %s for %s: %w", host, slotName, err)
		}
		host = resolved
	}

	// TCP connect — socket binds to this namespace
	target := net.JoinHostPort(host, port)
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

// resolveDNS64 resolves a hostname to an IPv6 address using the given DNS64 server.
// Must be called while the thread is inside the target namespace.
func resolveDNS64(hostname string, dnsServer string) (string, error) {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return net.DialTimeout("udp6", net.JoinHostPort(dnsServer, "53"), 5*time.Second)
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ips, err := resolver.LookupHost(ctx, hostname)
	if err != nil {
		return "", fmt.Errorf("lookup %s: %w", hostname, err)
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
