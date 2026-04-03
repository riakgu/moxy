//go:build linux

package netns

import (
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"

	"github.com/riakgu/moxy/internal/model"
)

type Discovery struct {
	Log         *logrus.Logger
	Concurrency int
	IPCheckHost string
}

func NewDiscovery(log *logrus.Logger, concurrency int, ipCheckHost string) *Discovery {
	return &Discovery{
		Log:         log,
		Concurrency: concurrency,
		IPCheckHost: ipCheckHost,
	}
}

func (d *Discovery) ResolveSlotIP(slotName string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save host namespace
	hostNs, err := netns.Get()
	if err != nil {
		return "", fmt.Errorf("get host namespace: %w", err)
	}
	defer hostNs.Close()

	defer func() {
		netns.Set(hostNs)
	}()

	// Enter slot namespace
	slotNs, err := netns.GetFromName(slotName)
	if err != nil {
		return "", fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer slotNs.Close()

	if err := netns.Set(slotNs); err != nil {
		return "", fmt.Errorf("enter namespace %s: %w", slotName, err)
	}

	// Resolve IP check host — uses namespace's /etc/resolv.conf
	resolver := &net.Resolver{PreferGo: true}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ips, err := resolver.LookupHost(ctx, d.IPCheckHost)
	if err != nil {
		return "", fmt.Errorf("DNS64 resolve for %s: %w", slotName, err)
	}

	// Dial TCP6 directly on this locked thread (ensures correct namespace)
	var conn net.Conn
	for _, ip := range ips {
		if strings.Contains(ip, ":") {
			conn, err = net.DialTimeout("tcp6", net.JoinHostPort(ip, "80"), 10*time.Second)
			if err == nil {
				break
			}
		}
	}
	if conn == nil {
		return "", fmt.Errorf("no reachable address for %s in %s", d.IPCheckHost, slotName)
	}
	defer conn.Close()

	// Restore host namespace now — the socket is already bound to the slot namespace
	netns.Set(hostNs)

	// Write raw HTTP GET on the established connection
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	_, err = conn.Write([]byte(fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", d.IPCheckHost)))
	if err != nil {
		return "", fmt.Errorf("write HTTP request for %s: %w", slotName, err)
	}

	// Read response
	body, err := io.ReadAll(conn)
	if err != nil {
		return "", fmt.Errorf("read HTTP response for %s: %w", slotName, err)
	}

	// Parse body — skip HTTP headers
	parts := strings.SplitN(string(body), "\r\n\r\n", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid HTTP response for %s", slotName)
	}

	ip := strings.TrimSpace(parts[1])
	if ip == "" {
		return "", fmt.Errorf("empty IP response for %s", slotName)
	}
	return ip, nil
}

func (d *Discovery) ResolveSlotIPv6(slotName string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save host namespace
	hostNs, err := netns.Get()
	if err != nil {
		return "", fmt.Errorf("get host namespace: %w", err)
	}
	defer hostNs.Close()

	defer func() {
		netns.Set(hostNs)
	}()

	// Enter slot namespace
	slotNs, err := netns.GetFromName(slotName)
	if err != nil {
		return "", fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer slotNs.Close()

	if err := netns.Set(slotNs); err != nil {
		return "", fmt.Errorf("enter namespace %s: %w", slotName, err)
	}

	// List all links and find global IPv6 addresses
	links, err := netlink.LinkList()
	if err != nil {
		return "", fmt.Errorf("list links in %s: %w", slotName, err)
	}

	for _, link := range links {
		if link.Attrs().Name == "lo" {
			continue
		}
		addrs, err := netlink.AddrList(link, netlink.FAMILY_V6)
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if addr.Scope == int(netlink.SCOPE_UNIVERSE) && addr.IP.IsGlobalUnicast() && !addr.IP.IsLinkLocalUnicast() {
				return addr.IP.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no global IPv6 found for %s", slotName)
}

func (d *Discovery) DiscoverAll(slotNames []string) []*model.DiscoveredSlot {
	results := make([]*model.DiscoveredSlot, 0, len(slotNames))
	var mu sync.Mutex

	sem := make(chan struct{}, d.Concurrency)
	var wg sync.WaitGroup

	for _, name := range slotNames {
		wg.Add(1)
		go func(slotName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ipv4, err := d.ResolveSlotIP(slotName)
			if err != nil {
				d.Log.Warnf("discovery: %s IPv4 resolve failed: %v", slotName, err)
				mu.Lock()
				results = append(results, &model.DiscoveredSlot{
					Name:    slotName,
					Healthy: false,
				})
				mu.Unlock()
				return
			}

			ipv6, _ := d.ResolveSlotIPv6(slotName)

			mu.Lock()
			results = append(results, &model.DiscoveredSlot{
				Name:        slotName,
				IPv6Address: ipv6,
				IPv4Address: ipv4,
				Healthy:     true,
			})
			mu.Unlock()
		}(name)
	}

	wg.Wait()
	return results
}

