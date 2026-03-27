//go:build linux

package netns

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
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

	// Lock thread for entire operation — setns operates on the calling thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save host namespace first (before any namespace switching)
	hostNs, err := netns.Get()
	if err != nil {
		return fmt.Errorf("get host namespace: %w", err)
	}
	defer hostNs.Close()

	// Always restore host namespace on exit
	defer func() {
		if err := netns.Set(hostNs); err != nil {
			p.Log.Errorf("CRITICAL: failed to restore host namespace: %v", err)
		}
	}()

	// --- Create namespace ---
	// NewNamed() switches the current thread INTO the new namespace!
	newNs, err := netns.NewNamed(name)
	if err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("create namespace %s: %w", name, err)
		}
		p.Log.Debugf("slot %s: namespace already exists, reusing", name)
		newNs, err = netns.GetFromName(name)
		if err != nil {
			return fmt.Errorf("open existing namespace %s: %w", name, err)
		}
	}
	defer newNs.Close()

	// Switch BACK to host namespace to create and move the IPVLAN link
	if err := netns.Set(hostNs); err != nil {
		return fmt.Errorf("restore host ns for link setup: %w", err)
	}

	// --- Host namespace: create IPVLAN and move it ---

	// 1. Get parent interface
	parent, err := netlink.LinkByName(iface)
	if err != nil {
		return fmt.Errorf("get parent interface %s: %w", iface, err)
	}

	// 2. Create IPVLAN interface in L2 mode
	ipvlan := &netlink.IPVlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:        ipvlanName,
			ParentIndex: parent.Attrs().Index,
		},
		Mode: netlink.IPVLAN_MODE_L2,
	}
	if err := netlink.LinkAdd(ipvlan); err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("create IPVLAN %s: %w", ipvlanName, err)
		}
		p.Log.Debugf("slot %s: IPVLAN %s already exists, reusing", name, ipvlanName)
	}

	// Re-fetch the link to get its index
	ipvlanLink, err := netlink.LinkByName(ipvlanName)
	if err != nil {
		return fmt.Errorf("get IPVLAN link %s: %w", ipvlanName, err)
	}

	// 3. Move IPVLAN into the namespace
	if err := netlink.LinkSetNsFd(ipvlanLink, int(newNs)); err != nil {
		return fmt.Errorf("move %s to namespace %s: %w", ipvlanName, name, err)
	}

	// --- Enter slot namespace for configuration ---
	if err := netns.Set(newNs); err != nil {
		return fmt.Errorf("enter namespace %s: %w", name, err)
	}

	// 4a. Bring up loopback
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("get loopback in %s: %w", name, err)
	}
	if err := netlink.LinkSetUp(lo); err != nil {
		return fmt.Errorf("bring up loopback in %s: %w", name, err)
	}

	// 4b. Bring up IPVLAN
	ipvlanInNs, err := netlink.LinkByName(ipvlanName)
	if err != nil {
		return fmt.Errorf("get %s in namespace %s: %w", ipvlanName, name, err)
	}
	if err := netlink.LinkSetUp(ipvlanInNs); err != nil {
		return fmt.Errorf("bring up %s in %s: %w", ipvlanName, name, err)
	}

	// 4c. Enable accept_ra and autoconf via /proc/sys
	sysctlBase := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s", ipvlanName)
	for _, kv := range [][2]string{
		{"accept_ra", "2"},
		{"autoconf", "1"},
	} {
		path := fmt.Sprintf("%s/%s", sysctlBase, kv[0])
		if err := os.WriteFile(path, []byte(kv[1]), 0644); err != nil {
			return fmt.Errorf("set %s=%s for %s: %w", kv[0], kv[1], name, err)
		}
	}

	// 4d. Set DNS64 nameserver
	if dns64 != "" {
		resolvConf := fmt.Sprintf("nameserver %s\n", dns64)
		if err := os.WriteFile("/etc/resolv.conf", []byte(resolvConf), 0644); err != nil {
			return fmt.Errorf("set DNS64 for %s: %w", name, err)
		}
	}

	p.Log.Debugf("slot %s: provisioned successfully", name)
	return nil
}

func (p *Provisioner) EnableNDPProxy(iface string) error {
	path := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/proxy_ndp", iface)
	if err := os.WriteFile(path, []byte("1"), 0644); err != nil {
		return fmt.Errorf("enable NDP proxy on %s: %w", iface, err)
	}
	return nil
}

func (p *Provisioner) AddNDPProxyEntry(ipv6 string, iface string) error {
	if ipv6 == "" {
		return nil
	}
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return fmt.Errorf("get interface %s: %w", iface, err)
	}

	neigh := &netlink.Neigh{
		LinkIndex: link.Attrs().Index,
		Family:    unix.AF_INET6,
		Flags:     netlink.NTF_PROXY,
		IP:        net.ParseIP(ipv6),
	}
	if err := netlink.NeighAdd(neigh); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("add NDP proxy for %s on %s: %w", ipv6, iface, err)
	}
	p.Log.Debugf("NDP proxy entry added: %s on %s", ipv6, iface)
	return nil
}

func (p *Provisioner) RemoveNDPProxyEntry(ipv6 string, iface string) error {
	if ipv6 == "" {
		return nil
	}
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return fmt.Errorf("get interface %s: %w", iface, err)
	}

	neigh := &netlink.Neigh{
		LinkIndex: link.Attrs().Index,
		Family:    unix.AF_INET6,
		Flags:     netlink.NTF_PROXY,
		IP:        net.ParseIP(ipv6),
	}
	if err := netlink.NeighDel(neigh); err != nil {
		// Idempotent: ignore if not found
		return nil
	}
	p.Log.Debugf("NDP proxy entry removed: %s on %s", ipv6, iface)
	return nil
}

func (p *Provisioner) DestroySlot(name string) error {
	if err := netns.DeleteNamed(name); err != nil {
		return fmt.Errorf("delete namespace %s: %w", name, err)
	}
	return nil
}

func (p *Provisioner) ListSlotNamespaces() ([]string, error) {
	entries, err := os.ReadDir("/var/run/netns")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list namespaces: %w", err)
	}

	var slots []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "slot") {
			slots = append(slots, entry.Name())
		}
	}
	return slots, nil
}
