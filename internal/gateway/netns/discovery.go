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

	"github.com/riakgu/moxy/internal/model"
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

type IPInfoResponse struct {
	IP   string `json:"ip"`
	City string `json:"city"`
	ASN  string `json:"asn"`
	Org  string `json:"org"`
	RTT  string `json:"rtt"`
}

func (d *Discovery) httpGetInNamespace(slotName, path string) ([]byte, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hostNs, err := netns.Get()
	if err != nil {
		return nil, fmt.Errorf("get host namespace: %w", err)
	}
	defer func() { _ = hostNs.Close() }()

	defer func() {
		_ = netns.Set(hostNs)
	}()

	slotNs, err := netns.GetFromName(slotName)
	if err != nil {
		return nil, fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer func() { _ = slotNs.Close() }()

	if err := netns.Set(slotNs); err != nil {
		return nil, fmt.Errorf("enter namespace %s: %w", slotName, err)
	}

	// Uses namespace's /etc/resolv.conf
	resolver := &net.Resolver{PreferGo: true}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ips, err := resolver.LookupHost(ctx, d.IPCheckHost)
	if err != nil {
		return nil, fmt.Errorf("DNS64 resolve for %s: %w", slotName, err)
	}

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

	// The socket is already bound to the slot namespace
	_ = netns.Set(hostNs)

	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", path, d.IPCheckHost)
	if _, err = conn.Write([]byte(req)); err != nil {
		return nil, fmt.Errorf("write HTTP request for %s: %w", slotName, err)
	}

	// http.ReadResponse handles chunked encoding
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

func (d *Discovery) ResolveSlotIP(req *model.ResolveSlotRequest) (string, error) {
	slotName := req.SlotName
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

func (d *Discovery) ResolveSlotIPInfo(req *model.ResolveSlotRequest) (*model.SlotIPInfoResult, error) {
	slotName := req.SlotName
	body, err := d.httpGetInNamespace(slotName, "/json")
	if err != nil {
		return nil, err
	}
	var info IPInfoResponse
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse IP info for %s: %w", slotName, err)
	}
	return &model.SlotIPInfoResult{
		IP:   info.IP,
		City: info.City,
		ASN:  info.ASN,
		Org:  info.Org,
		RTT:  info.RTT,
	}, nil
}

func (d *Discovery) ResolveSlotIPv6(req *model.ResolveSlotRequest) (string, error) {
	slotName := req.SlotName
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hostNs, err := netns.Get()
	if err != nil {
		return "", fmt.Errorf("get host namespace: %w", err)
	}
	defer func() { _ = hostNs.Close() }()

	defer func() {
		_ = netns.Set(hostNs)
	}()

	slotNs, err := netns.GetFromName(slotName)
	if err != nil {
		return "", fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer func() { _ = slotNs.Close() }()

	if err := netns.Set(slotNs); err != nil {
		return "", fmt.Errorf("enter namespace %s: %w", slotName, err)
	}

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
