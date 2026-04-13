//go:build linux

package netns

import (
	"bufio"
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

	mdns "github.com/miekg/dns"
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

// lookupHostInNS resolves a hostname using miekg/dns synchronously.
// This runs entirely on the calling goroutine (locked OS thread),
// so it correctly stays within the network namespace.
// Go's net.Resolver spawns goroutines that escape the namespace.
func (d *Discovery) lookupHostInNS(host, nameserver string) ([]string, error) {
	dnsAddr := net.JoinHostPort(nameserver, "53")
	client := &mdns.Client{
		Net:     "udp",
		Timeout: 5 * time.Second,
	}

	fqdn := mdns.Fqdn(host)
	var addrs []string

	// Query AAAA (IPv6 / DNS64-synthesized)
	msg := new(mdns.Msg)
	msg.SetQuestion(fqdn, mdns.TypeAAAA)
	resp, _, err := client.Exchange(msg, dnsAddr)
	if err == nil && resp != nil {
		for _, ans := range resp.Answer {
			if aaaa, ok := ans.(*mdns.AAAA); ok {
				addrs = append(addrs, aaaa.AAAA.String())
			}
		}
	}

	// Query A (IPv4 fallback)
	msg = new(mdns.Msg)
	msg.SetQuestion(fqdn, mdns.TypeA)
	resp, _, err = client.Exchange(msg, dnsAddr)
	if err == nil && resp != nil {
		for _, ans := range resp.Answer {
			if a, ok := ans.(*mdns.A); ok {
				addrs = append(addrs, a.A.String())
			}
		}
	}

	if len(addrs) == 0 {
		return nil, fmt.Errorf("no DNS records for %s via %s", host, nameserver)
	}
	return addrs, nil
}

func (d *Discovery) httpGetInNamespace(slotName, nameserver, path string) ([]byte, error) {
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

	// Resolve using miekg/dns — runs synchronously on this locked thread,
	// so DNS queries go through the slot namespace's IPVLAN
	ips, err := d.lookupHostInNS(d.IPCheckHost, nameserver)
	if err != nil {
		return nil, fmt.Errorf("DNS resolve for %s in %s: %w", d.IPCheckHost, slotName, err)
	}
	d.Log.Debug("discovery dns resolved", "slot", slotName, "nameserver", nameserver, "host", d.IPCheckHost, "addrs", ips)

	var conn net.Conn
	for _, ip := range ips {
		network := "tcp6"
		if !strings.Contains(ip, ":") {
			network = "tcp4"
		}
		rawConn, dialErr := net.DialTimeout(network, net.JoinHostPort(ip, "443"), 10*time.Second)
		if dialErr != nil {
			d.Log.Debug("discovery dial failed", "slot", slotName, "ip", ip, "error", dialErr)
			err = dialErr
			continue
		}
		tlsConn := tls.Client(rawConn, &tls.Config{ServerName: d.IPCheckHost})
		if hsErr := tlsConn.Handshake(); hsErr != nil {
			_ = rawConn.Close()
			d.Log.Debug("discovery tls failed", "slot", slotName, "ip", ip, "error", hsErr)
			err = hsErr
			continue
		}
		conn = tlsConn
		break
	}
	if conn == nil {
		d.Log.Warn("discovery no reachable address", "slot", slotName, "tried", len(ips), "error", err)
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
	body, err := d.httpGetInNamespace(slotName, req.Nameserver, "/")
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
	body, err := d.httpGetInNamespace(slotName, req.Nameserver, "/json")
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
