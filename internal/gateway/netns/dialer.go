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

	"github.com/riakgu/moxy/internal/model"
	"golang.org/x/sys/unix"
)

type SetnsDialer struct {
	Log      *slog.Logger
	Resolver *CachingResolver
}

func NewSetnsDialer(log *slog.Logger, resolver *CachingResolver) *SetnsDialer {
	return &SetnsDialer{Log: log, Resolver: resolver}
}

func toNAT64(ipv4 net.IP, prefix string) string {
	v4 := ipv4.To4()
	return fmt.Sprintf("%s%02x%02x:%02x%02x", prefix, v4[0], v4[1], v4[2], v4[3])
}

func (d *SetnsDialer) Dial(req *model.DialRequest) (net.Conn, error) {
	slotName := req.SlotName
	addr := req.Addr
	nameserver := req.Nameserver
	nat64Prefix := req.NAT64Prefix
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	// Convert to NAT64 before entering namespace
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() != nil {
		host = toNAT64(ip, nat64Prefix)
	}

	// Pin goroutine to OS thread — required because setns operates on the calling thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNs, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return nil, fmt.Errorf("open host namespace: %w", err)
	}
	defer func() { _ = origNs.Close() }()

	targetNs, err := os.Open("/var/run/netns/" + slotName)
	if err != nil {
		return nil, fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer func() { _ = targetNs.Close() }()

	if err := unix.Setns(int(targetNs.Fd()), unix.CLONE_NEWNET); err != nil {
		return nil, fmt.Errorf("setns to %s: %w", slotName, err)
	}

	if ip == nil {
		resolved, err := d.Resolver.Resolve(host, nameserver, nat64Prefix)
		if err != nil {
			// Restore host namespace before returning error
			if restoreErr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); restoreErr != nil {
				d.Log.Error("failed to restore namespace after DNS error", "slot", slotName, "error", restoreErr)
			}
			return nil, fmt.Errorf("DNS64 resolve %s for %s: %w", host, slotName, err)
		}
		host = resolved
	}

	// Socket binds to this namespace
	target := net.JoinHostPort(host, port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, dialErr := dialer.DialContext(context.Background(), "tcp6", target)

	// Always restore host namespace, even if dial failed
	if restoreErr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); restoreErr != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, fmt.Errorf("restore host namespace: %w", restoreErr)
	}

	if dialErr != nil {
		return nil, fmt.Errorf("dial %s via %s: %w", addr, slotName, dialErr)
	}

	return conn, nil
}

func (d *SetnsDialer) DialIPv6(req *model.DialRequest) (net.Conn, error) {
	slotName := req.SlotName
	addr := req.Addr
	nameserver := req.Nameserver
	nat64Prefix := req.NAT64Prefix
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	ip := net.ParseIP(host)

	if ip != nil && ip.To4() != nil {
		host = toNAT64(ip, nat64Prefix)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNs, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return nil, fmt.Errorf("open host namespace: %w", err)
	}
	defer func() { _ = origNs.Close() }()

	targetNs, err := os.Open("/var/run/netns/" + slotName)
	if err != nil {
		return nil, fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer func() { _ = targetNs.Close() }()

	if err := unix.Setns(int(targetNs.Fd()), unix.CLONE_NEWNET); err != nil {
		return nil, fmt.Errorf("setns to %s: %w", slotName, err)
	}

	// Inside namespace: resolve (if domain) + connect
	if ip == nil {
		resolved, nativeErr := d.Resolver.ResolveNative(host, nameserver, nat64Prefix)
		if nativeErr != nil {
			resolved, err = d.Resolver.Resolve(host, nameserver, nat64Prefix)
			if err != nil {
				if restoreErr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); restoreErr != nil {
					d.Log.Error("failed to restore namespace after DNS error", "slot", slotName, "error", restoreErr)
				}
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
			_ = conn.Close()
		}
		return nil, fmt.Errorf("restore host namespace: %w", restoreErr)
	}

	if dialErr != nil {
		return nil, fmt.Errorf("dial %s via %s: %w", addr, slotName, dialErr)
	}

	return conn, nil
}

func (d *SetnsDialer) DialUDP(req *model.DialRequest) (*net.UDPConn, error) {
	slotName := req.SlotName
	addr := req.Addr
	nameserver := req.Nameserver
	nat64Prefix := req.NAT64Prefix
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	ip := net.ParseIP(host)
	if ip != nil && ip.To4() != nil {
		host = toNAT64(ip, nat64Prefix)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNs, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return nil, fmt.Errorf("open host namespace: %w", err)
	}
	defer func() { _ = origNs.Close() }()

	targetNs, err := os.Open("/var/run/netns/" + slotName)
	if err != nil {
		return nil, fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer func() { _ = targetNs.Close() }()

	if err := unix.Setns(int(targetNs.Fd()), unix.CLONE_NEWNET); err != nil {
		return nil, fmt.Errorf("setns to %s: %w", slotName, err)
	}

	if ip == nil {
		resolved, err := d.Resolver.Resolve(host, nameserver, nat64Prefix)
		if err != nil {
			if restoreErr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); restoreErr != nil {
				d.Log.Error("failed to restore namespace after DNS error", "slot", slotName, "error", restoreErr)
			}
			return nil, fmt.Errorf("DNS64 resolve %s for %s: %w", host, slotName, err)
		}
		host = resolved
	}

	portNum, _ := net.LookupPort("udp", port)
	targetIP := net.ParseIP(host)
	udpAddr := &net.UDPAddr{IP: targetIP, Port: portNum}
	conn, dialErr := net.DialUDP("udp6", nil, udpAddr)

	if restoreErr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); restoreErr != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, fmt.Errorf("restore host namespace: %w", restoreErr)
	}

	if dialErr != nil {
		return nil, fmt.Errorf("dial udp %s via %s: %w", addr, slotName, dialErr)
	}

	return conn, nil
}

func (d *SetnsDialer) DialIPv6UDP(req *model.DialRequest) (*net.UDPConn, error) {
	slotName := req.SlotName
	addr := req.Addr
	nameserver := req.Nameserver
	nat64Prefix := req.NAT64Prefix
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	ip := net.ParseIP(host)
	if ip != nil && ip.To4() != nil {
		host = toNAT64(ip, nat64Prefix)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNs, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return nil, fmt.Errorf("open host namespace: %w", err)
	}
	defer func() { _ = origNs.Close() }()

	targetNs, err := os.Open("/var/run/netns/" + slotName)
	if err != nil {
		return nil, fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer func() { _ = targetNs.Close() }()

	if err := unix.Setns(int(targetNs.Fd()), unix.CLONE_NEWNET); err != nil {
		return nil, fmt.Errorf("setns to %s: %w", slotName, err)
	}

	if ip == nil {
		resolved, nativeErr := d.Resolver.ResolveNative(host, nameserver, nat64Prefix)
		if nativeErr != nil {
			resolved, err = d.Resolver.Resolve(host, nameserver, nat64Prefix)
			if err != nil {
				if restoreErr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); restoreErr != nil {
					d.Log.Error("failed to restore namespace after DNS error", "slot", slotName, "error", restoreErr)
				}
				return nil, fmt.Errorf("DNS resolve %s for %s: native=%v, dns64=%w", host, slotName, nativeErr, err)
			}
			d.Log.Debug("udp dns64 fallback used", "host", host, "slot", slotName, "resolved", resolved)
		} else {
			d.Log.Debug("udp native ipv6 resolved", "host", host, "slot", slotName, "resolved", resolved)
		}
		host = resolved
	}

	portNum, _ := net.LookupPort("udp", port)
	targetIP := net.ParseIP(host)
	udpAddr := &net.UDPAddr{IP: targetIP, Port: portNum}
	conn, dialErr := net.DialUDP("udp6", nil, udpAddr)

	if restoreErr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); restoreErr != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, fmt.Errorf("restore host namespace: %w", restoreErr)
	}

	if dialErr != nil {
		return nil, fmt.Errorf("dial udp %s via %s: %w", addr, slotName, dialErr)
	}

	return conn, nil
}
