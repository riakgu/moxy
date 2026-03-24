package netns

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

type Provisioner struct {
	Log *logrus.Logger
}

func NewProvisioner(log *logrus.Logger) *Provisioner {
	return &Provisioner{Log: log}
}

func (p *Provisioner) CreateSlot(slotIndex int, iface string, dns64 string) error {
	name := fmt.Sprintf("slot%d", slotIndex)
	ipvlanName := fmt.Sprintf("ipvlan%d", slotIndex)

	commands := []struct {
		desc string
		args []string
	}{
		{"create IPVLAN interface", []string{"ip", "link", "add", ipvlanName, "link", iface, "type", "ipvlan", "mode", "l2"}},
		{"create network namespace", []string{"ip", "netns", "add", name}},
		{"move IPVLAN to namespace", []string{"ip", "link", "set", ipvlanName, "netns", name}},
		{"bring up loopback", []string{"ip", "netns", "exec", name, "ip", "link", "set", "lo", "up"}},
		{"bring up IPVLAN", []string{"ip", "netns", "exec", name, "ip", "link", "set", ipvlanName, "up"}},
		{"enable accept_ra", []string{"ip", "netns", "exec", name, "sysctl", "-w", fmt.Sprintf("net.ipv6.conf.%s.accept_ra=2", ipvlanName)}},
		{"enable autoconf", []string{"ip", "netns", "exec", name, "sysctl", "-w", fmt.Sprintf("net.ipv6.conf.%s.autoconf=1", ipvlanName)}},
		{"set DNS64", []string{"ip", "netns", "exec", name, "bash", "-c", fmt.Sprintf(`echo "nameserver %s" > /etc/resolv.conf`, dns64)}},
	}

	for _, c := range commands {
		p.Log.Debugf("slot %s: %s", name, c.desc)
		cmd := exec.Command(c.args[0], c.args[1:]...)
		if output, err := cmd.CombinedOutput(); err != nil {
			outStr := strings.TrimSpace(string(output))
			// Ignore "File exists" for namespace and link creation so provisioning is idempotent
			if strings.Contains(outStr, "File exists") {
				continue
			}
			return fmt.Errorf("%s failed for %s: %w (output: %s)", c.desc, name, err, outStr)
		}
	}

	return nil
}

func (p *Provisioner) EnableNDPProxy(iface string) error {
	cmd := exec.Command("sysctl", "-w", fmt.Sprintf("net.ipv6.conf.%s.proxy_ndp=1", iface))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("enable NDP proxy failed: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (p *Provisioner) DestroySlot(name string) error {
	cmd := exec.Command("ip", "netns", "del", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("delete namespace %s failed: %w (output: %s)", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (p *Provisioner) ListSlotNamespaces() ([]string, error) {
	cmd := exec.Command("ip", "netns", "list")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list namespaces failed: %w", err)
	}

	var slots []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		name := strings.Fields(line)
		if len(name) > 0 && strings.HasPrefix(name[0], "slot") {
			slots = append(slots, name[0])
		}
	}
	return slots, nil
}
