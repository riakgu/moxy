//go:build linux

package netns

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"
	"log/slog"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

type Discovery struct {
	Log         *slog.Logger
	IPCheckHost string
}

func NewDiscovery(log *slog.Logger, ipCheckHost string) *Discovery {
	return &Discovery{
		Log:         log,
		IPCheckHost: ipCheckHost,
	}
}

// IPInfoResponse holds the parsed JSON from ip.moxy.my.id/json.
type IPInfoResponse struct {
	IP   string `json:"ip"`
	City string `json:"city"`
	ASN  string `json:"asn"`
	Org  string `json:"org"`
	RTT  string `json:"rtt"`
}

// httpGetInNamespace performs a raw HTTPS GET inside a slot's network namespace.
// Returns the HTTP response body bytes.
func (d *Discovery) httpGetInNamespace(slotName, path string) ([]byte, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save host namespace
	hostNs, err := netns.Get()
	if err != nil {
		return nil, fmt.Errorf("get host namespace: %w", err)
	}
	defer func() { _ = hostNs.Close() }()

	defer func() {
		_ = netns.Set(hostNs)
	}()

	// Enter slot namespace
	slotNs, err := netns.GetFromName(slotName)
	if err != nil {
		return nil, fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer func() { _ = slotNs.Close() }()

	if err := netns.Set(slotNs); err != nil {
		return nil, fmt.Errorf("enter namespace %s: %w", slotName, err)
	}

	// Resolve IP check host — uses namespace's /etc/resolv.conf
	resolver := &net.Resolver{PreferGo: true}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ips, err := resolver.LookupHost(ctx, d.IPCheckHost)
	if err != nil {
		return nil, fmt.Errorf("DNS64 resolve for %s: %w", slotName, err)
	}

	// Dial TCP6 with TLS
	var conn net.Conn
	for _, ip := range ips {
		if strings.Contains(ip, ":") {
			rawConn, dialErr := net.DialTimeout("tcp6", net.JoinHostPort(ip, "443"), 10*time.Second)
			if dialErr != nil {
				err = dialErr
				continue
			}
			tlsConn := tls.Client(rawConn, &tls.Config{ServerName: d.IPCheckHost})
			if hsErr := tlsConn.Handshake(); hsErr != nil {
				_ = rawConn.Close()
				err = hsErr
				continue
			}
			conn = tlsConn
			break
		}
	}
	if conn == nil {
		d.Log.Warn("discovery dns returned no reachable address", "slot", slotName, "addrs", len(ips), "error", err)
		return nil, fmt.Errorf("no reachable address for %s in %s: %v", d.IPCheckHost, slotName, err)
	}
	defer func() { _ = conn.Close() }()

	// Restore host namespace — the socket is already bound to the slot namespace
	_ = netns.Set(hostNs)

	// Write raw HTTP GET on the established TLS connection
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", path, d.IPCheckHost)
	if _, err = conn.Write([]byte(req)); err != nil {
		return nil, fmt.Errorf("write HTTP request for %s: %w", slotName, err)
	}

	// Read HTTP response properly (handles chunked encoding etc.)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return nil, fmt.Errorf("read HTTP response for %s: %w", slotName, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body for %s: %w", slotName, err)
	}

	return body, nil
}

// ResolveSlotIP returns the public IPv4 of a slot (plain text endpoint).
// Used for lightweight steady-state checks.
func (d *Discovery) ResolveSlotIP(slotName string) (string, error) {
	body, err := d.httpGetInNamespace(slotName, "/")
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return "", fmt.Errorf("empty IP response for %s", slotName)
	}
	return ip, nil
}

// ResolveSlotIPInfo returns full IP metadata from the JSON endpoint.
// Used during burst detection for rich metadata (city, ASN, RTT).
func (d *Discovery) ResolveSlotIPInfo(slotName string) (ip, city, asn, org, rtt string, err error) {
	body, err := d.httpGetInNamespace(slotName, "/json")
	if err != nil {
		return "", "", "", "", "", err
	}
	var info IPInfoResponse
	if err := json.Unmarshal(body, &info); err != nil {
		return "", "", "", "", "", fmt.Errorf("parse IP info for %s: %w", slotName, err)
	}
	return info.IP, info.City, info.ASN, info.Org, info.RTT, nil
}

func (d *Discovery) ResolveSlotIPv6(slotName string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save host namespace
	hostNs, err := netns.Get()
	if err != nil {
		return "", fmt.Errorf("get host namespace: %w", err)
	}
	defer func() { _ = hostNs.Close() }()

	defer func() {
		_ = netns.Set(hostNs)
	}()

	// Enter slot namespace
	slotNs, err := netns.GetFromName(slotName)
	if err != nil {
		return "", fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer func() { _ = slotNs.Close() }()

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
