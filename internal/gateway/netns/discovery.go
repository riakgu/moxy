package netns

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
)

type Discovery struct {
	Log         *logrus.Logger
	Concurrency int
	Provisioner *Provisioner
	Interface   string
}

func NewDiscovery(log *logrus.Logger, concurrency int, provisioner *Provisioner, iface string) *Discovery {
	return &Discovery{
		Log:         log,
		Concurrency: concurrency,
		Provisioner: provisioner,
		Interface:   iface,
	}
}

func ParseIPFromOutput(output string) (string, error) {
	ip := strings.TrimSpace(output)
	if ip == "" {
		return "", fmt.Errorf("empty IP output")
	}
	return ip, nil
}

func (d *Discovery) ResolveSlotIP(slotName string) (string, error) {
	cmd := exec.Command("ip", "netns", "exec", slotName, "curl", "-s", "--max-time", "10", "http://api.ipify.org")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve IP for %s failed: %w (output: %s)", slotName, err, strings.TrimSpace(string(output)))
	}

	return ParseIPFromOutput(string(output))
}

func (d *Discovery) ResolveSlotIPv6(slotName string) (string, error) {
	cmd := exec.Command("ip", "netns", "exec", slotName, "ip", "-6", "addr", "show", "scope", "global")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve IPv6 for %s failed: %w", slotName, err)
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet6 ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				addr := strings.Split(parts[1], "/")[0]
				return addr, nil
			}
		}
	}

	return "", fmt.Errorf("no global IPv6 found for %s", slotName)
}

type SlotDiscoveryResult struct {
	Slot *entity.Slot
	Err  error
}

func (d *Discovery) DiscoverAll(slotNames []string) []*entity.Slot {
	results := make([]*entity.Slot, 0, len(slotNames))
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
				results = append(results, &entity.Slot{
					Name:   slotName,
					Status: entity.SlotStatusUnhealthy,
				})
				mu.Unlock()
				return
			}

			ipv6, _ := d.ResolveSlotIPv6(slotName)

			// Add NDP proxy entry for the slot's IPv6 address
			if ipv6 != "" && d.Provisioner != nil {
				if err := d.Provisioner.AddNDPProxyEntry(ipv6, d.Interface); err != nil {
					d.Log.Warnf("discovery: %s NDP proxy entry failed: %v", slotName, err)
				}
			}

			mu.Lock()
			results = append(results, &entity.Slot{
				Name:        slotName,
				IPv6Address: ipv6,
				PublicIPv4:  ipv4,
				Status:      entity.SlotStatusHealthy,
			})
			mu.Unlock()
		}(name)
	}

	wg.Wait()
	return results
}
