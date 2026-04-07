//go:build linux

package netns

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"time"
	"log/slog"

	"golang.org/x/sys/unix"
)

// SetnsDialer dials targets through network namespaces using the setns(2) syscall.
// Uses CachingResolver for DNS64 lookups with per-device caching.
type SetnsDialer struct {
	Log      *slog.Logger
	Resolver *CachingResolver
}

// NewSetnsDialer creates a new SetnsDialer with the given CachingResolver.
func NewSetnsDialer(log *slog.Logger, resolver *CachingResolver) *SetnsDialer {
	return &SetnsDialer{Log: log, Resolver: resolver}
}

// toNAT64 converts an IPv4 address to its NAT64 representation using the provided prefix.
func toNAT64(ipv4 net.IP, prefix string) string {
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

	// If host is a domain name, resolve via DNS64 inside the namespace (cached)
	if ip == nil {
		resolved, err := d.Resolver.Resolve(host, nameserver, nat64Prefix)
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

// DialIPv6 connects to addr through the slot's namespace, preferring native IPv6.
// Resolution order for domains:
//  1. Try native AAAA (real IPv6, not DNS64-synthesized)
//  2. Fall back to DNS64/NAT64 if no native AAAA exists
//
// Raw IPv4 addresses are converted to NAT64. Raw IPv6 addresses dial directly.
func (d *SetnsDialer) DialIPv6(slotName string, addr string, nameserver string, nat64Prefix string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	ip := net.ParseIP(host)

	// Raw IPv4 → NAT64 (fallback, same as Dial)
	if ip != nil && ip.To4() != nil {
		host = toNAT64(ip, nat64Prefix)
	}

	// Raw IPv6 → use directly (no resolution needed)
	// For domains, we resolve below inside the namespace

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNs, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return nil, fmt.Errorf("open host namespace: %w", err)
	}
	defer origNs.Close()

	targetNs, err := os.Open("/var/run/netns/" + slotName)
	if err != nil {
		return nil, fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer targetNs.Close()

	if err := unix.Setns(int(targetNs.Fd()), unix.CLONE_NEWNET); err != nil {
		return nil, fmt.Errorf("setns to %s: %w", slotName, err)
	}

	// Inside namespace: resolve (if domain) + connect
	if ip == nil {
		// Try native AAAA first
		resolved, nativeErr := d.Resolver.ResolveNative(host, nameserver, nat64Prefix)
		if nativeErr != nil {
			// Fallback to DNS64
			resolved, err = d.Resolver.Resolve(host, nameserver, nat64Prefix)
			if err != nil {
				unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET)
				return nil, fmt.Errorf("DNS resolve %s for %s: native=%v, dns64=%w", host, slotName, nativeErr, err)
			}
			d.Log.Debug("dns64 fallback used", "host", host, "slot", slotName, "resolved", resolved)
		} else {
			d.Log.Debug("native ipv6 resolved", "host", host, "slot", slotName, "resolved", resolved)
		}
		host = resolved
	}

	target := net.JoinHostPort(host, port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, dialErr := dialer.DialContext(context.Background(), "tcp6", target)

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
