//go:build linux

package netns

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
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
// For domain names, DNS resolution uses the configured DNS64 server.
func (d *SetnsDialer) Dial(slotName string, addr string) (io.ReadWriteCloser, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	// NAT64 conversion for raw IPv4 addresses
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() != nil {
		host = toNAT64(ip)
		addr = net.JoinHostPort(host, port)
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

	// Build dialer with DNS64 resolver for domain name resolution inside the namespace.
	// PreferGo ensures DNS resolution runs synchronously on this locked thread.
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	if d.DNS64Server != "" {
		dialer.Resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				return net.DialTimeout("udp6", net.JoinHostPort(d.DNS64Server, "53"), 5*time.Second)
			},
		}
	}

	// Dial inside the slot namespace
	conn, dialErr := dialer.DialContext(context.Background(), "tcp6", addr)

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
