//go:build linux

package netns

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"log/slog"

	"github.com/riakgu/moxy/internal/model"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

type Provisioner struct {
	Log *slog.Logger
}

func NewProvisioner(log *slog.Logger) *Provisioner {
	return &Provisioner{Log: log}
}

func (p *Provisioner) CreateSlot(req *model.CreateSlotRequest) error {
	slotIndex := req.SlotIndex
	iface := req.Interface
	dns64 := req.DNS64
	name := fmt.Sprintf("slot%d", slotIndex)
	ipvlanName := fmt.Sprintf("ipvlan%d", slotIndex)

	// Lock thread for entire operation — setns operates on the calling thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hostNs, err := netns.Get()
	if err != nil {
		return fmt.Errorf("get host namespace: %w", err)
	}
	defer func() { _ = hostNs.Close() }()

	defer func() {
		if err := netns.Set(hostNs); err != nil {
			p.Log.Error("failed to restore host namespace", "error", err)
		}
	}()

	// NewNamed() switches the current thread INTO the new namespace!
	newNs, err := netns.NewNamed(name)
	if err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("create namespace %s: %w", name, err)
		}
		p.Log.Debug("namespace exists, reusing", "slot", name)
		newNs, err = netns.GetFromName(name)
		if err != nil {
			return fmt.Errorf("open existing namespace %s: %w", name, err)
		}
	}
	defer func() { _ = newNs.Close() }()

	if err := netns.Set(hostNs); err != nil {
		return fmt.Errorf("restore host ns for link setup: %w", err)
	}

	parent, err := netlink.LinkByName(iface)
	if err != nil {
		return fmt.Errorf("get parent interface %s: %w", iface, err)
	}

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
		p.Log.Debug("ipvlan exists, reusing", "slot", name, "ipvlan", ipvlanName)
	}

	ipvlanLink, err := netlink.LinkByName(ipvlanName)
	if err != nil {
		return fmt.Errorf("get IPVLAN link %s: %w", ipvlanName, err)
	}

	if err := netlink.LinkSetNsFd(ipvlanLink, int(newNs)); err != nil {
		return fmt.Errorf("move %s to namespace %s: %w", ipvlanName, name, err)
	}

	if err := netns.Set(newNs); err != nil {
		return fmt.Errorf("enter namespace %s: %w", name, err)
	}

	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("get loopback in %s: %w", name, err)
	}
	if err := netlink.LinkSetUp(lo); err != nil {
		return fmt.Errorf("bring up loopback in %s: %w", name, err)
	}

	ipvlanInNs, err := netlink.LinkByName(ipvlanName)
	if err != nil {
		return fmt.Errorf("get %s in namespace %s: %w", ipvlanName, name, err)
	}
	if err := netlink.LinkSetUp(ipvlanInNs); err != nil {
		return fmt.Errorf("bring up %s in %s: %w", ipvlanName, name, err)
	}

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

	// Write resolv.conf to /etc/netns/<name>/ — the standard Linux mechanism
	// for namespace-specific DNS. ip-netns bind-mounts this automatically.
	netnsDir := fmt.Sprintf("/etc/netns/%s", name)
	if err := os.MkdirAll(netnsDir, 0755); err != nil {
		return fmt.Errorf("create netns config dir for %s: %w", name, err)
	}
	resolvConf := fmt.Sprintf("nameserver %s\n", dns64)
	if err := os.WriteFile(fmt.Sprintf("%s/resolv.conf", netnsDir), []byte(resolvConf), 0644); err != nil {
		return fmt.Errorf("set DNS64 for %s: %w", name, err)
	}

	p.Log.Debug("slot provisioned", "slot", name)
	return nil
}

func (p *Provisioner) ConfigureDHCP(req *model.ConfigureDHCPRequest) error {
	iface := req.Interface
	if err := exec.Command("dhcpcd", iface).Run(); err != nil {
		return fmt.Errorf("dhcpcd on %s: %w", iface, err)
	}
	return nil
}

func (p *Provisioner) ConfigureIPv6SLAAC(req *model.ConfigureIPv6SLAACRequest) error {
	iface := req.Interface
	base := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s", iface)
	for _, kv := range [][2]string{
		{"accept_ra", "2"},
		{"autoconf", "1"},
	} {
		path := fmt.Sprintf("%s/%s", base, kv[0])
		if err := os.WriteFile(path, []byte(kv[1]), 0644); err != nil {
			return fmt.Errorf("set %s=%s for %s: %w", kv[0], kv[1], iface, err)
		}
	}
	return nil
}

func (p *Provisioner) BringInterfaceUp(req *model.BringInterfaceUpRequest) error {
	link, err := netlink.LinkByName(req.Interface)
	if err != nil {
		return fmt.Errorf("get interface %s: %w", req.Interface, err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("bring up %s: %w", req.Interface, err)
	}
	p.Log.Info("interface brought up", "interface", req.Interface)
	return nil
}

func (p *Provisioner) EnableNDPProxy(req *model.EnableNDPProxyRequest) error {
	iface := req.Interface
	path := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/proxy_ndp", iface)
	if err := os.WriteFile(path, []byte("1"), 0644); err != nil {
		return fmt.Errorf("enable NDP proxy on %s: %w", iface, err)
	}
	return nil
}

func (p *Provisioner) AddNDPProxyEntry(req *model.NDPProxyEntryRequest) error {
	ipv6 := req.IPv6
	iface := req.Interface
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
	p.Log.Debug("ndp proxy added", "ipv6", ipv6, "interface", iface)
	return nil
}

func (p *Provisioner) RemoveNDPProxyEntry(req *model.NDPProxyEntryRequest) error {
	ipv6 := req.IPv6
	iface := req.Interface
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
	p.Log.Debug("ndp proxy removed", "ipv6", ipv6, "interface", iface)
	return nil
}

func (p *Provisioner) DestroySlot(req *model.DestroySlotRequest) error {
	name := req.Name
	if err := netns.DeleteNamed(name); err != nil {
		return fmt.Errorf("delete namespace %s: %w", name, err)
	}
	// Clean up namespace-specific config
	_ = os.RemoveAll(fmt.Sprintf("/etc/netns/%s", name))
	return nil
}

func (p *Provisioner) ReattachSlot(req *model.ReattachSlotRequest) error {
	slotName := req.SlotName
	iface := req.Interface
	var slotIndex int
	if _, err := fmt.Sscanf(slotName, "slot%d", &slotIndex); err != nil {
		return fmt.Errorf("parse slot name %s: %w", slotName, err)
	}
	ipvlanName := fmt.Sprintf("ipvlan%d", slotIndex)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hostNs, err := netns.Get()
	if err != nil {
		return fmt.Errorf("get host namespace: %w", err)
	}
	defer func() { _ = hostNs.Close() }()
	defer func() {
		if err := netns.Set(hostNs); err != nil {
			p.Log.Error("failed to restore host namespace", "error", err)
		}
	}()

	slotNs, err := netns.GetFromName(slotName)
	if err != nil {
		return fmt.Errorf("open namespace %s: %w", slotName, err)
	}
	defer func() { _ = slotNs.Close() }()

	if oldLink, err := netlink.LinkByName(ipvlanName); err == nil {
		_ = netlink.LinkDel(oldLink)
	}

	// Also try to delete inside the namespace (it may be stuck there)
	if err := netns.Set(slotNs); err != nil {
		return fmt.Errorf("enter namespace %s for cleanup: %w", slotName, err)
	}
	if oldLink, err := netlink.LinkByName(ipvlanName); err == nil {
		_ = netlink.LinkDel(oldLink)
	}

	if err := netns.Set(hostNs); err != nil {
		return fmt.Errorf("restore host ns for link setup: %w", err)
	}

	parent, err := netlink.LinkByName(iface)
	if err != nil {
		return fmt.Errorf("get parent interface %s: %w", iface, err)
	}

	ipvlan := &netlink.IPVlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:        ipvlanName,
			ParentIndex: parent.Attrs().Index,
		},
		Mode: netlink.IPVLAN_MODE_L2,
	}
	if err := netlink.LinkAdd(ipvlan); err != nil {
		return fmt.Errorf("create IPVLAN %s: %w", ipvlanName, err)
	}

	ipvlanLink, err := netlink.LinkByName(ipvlanName)
	if err != nil {
		return fmt.Errorf("get IPVLAN link %s: %w", ipvlanName, err)
	}
	if err := netlink.LinkSetNsFd(ipvlanLink, int(slotNs)); err != nil {
		return fmt.Errorf("move %s to namespace %s: %w", ipvlanName, slotName, err)
	}

	if err := netns.Set(slotNs); err != nil {
		return fmt.Errorf("enter namespace %s: %w", slotName, err)
	}

	// Bring up loopback (should already be up, but be safe)
	if lo, err := netlink.LinkByName("lo"); err == nil {
		_ = netlink.LinkSetUp(lo)
	}

	ipvlanInNs, err := netlink.LinkByName(ipvlanName)
	if err != nil {
		return fmt.Errorf("get %s in namespace %s: %w", ipvlanName, slotName, err)
	}
	if err := netlink.LinkSetUp(ipvlanInNs); err != nil {
		return fmt.Errorf("bring up %s in %s: %w", ipvlanName, slotName, err)
	}

	sysctlBase := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s", ipvlanName)
	for _, kv := range [][2]string{
		{"accept_ra", "2"},
		{"autoconf", "1"},
	} {
		path := fmt.Sprintf("%s/%s", sysctlBase, kv[0])
		if err := os.WriteFile(path, []byte(kv[1]), 0644); err != nil {
			return fmt.Errorf("set %s=%s for %s: %w", kv[0], kv[1], slotName, err)
		}
	}

	p.Log.Info("slot re-attached", "slot", slotName, "interface", iface)
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

func (p *Provisioner) CleanupNamespaces(req *model.CleanupNamespacesRequest) (int, error) {
	keep := req.Keep
	all, err := p.ListSlotNamespaces()
	if err != nil {
		return 0, err
	}

	keepSet := make(map[string]struct{}, len(keep))
	for _, name := range keep {
		keepSet[name] = struct{}{}
	}

	cleaned := 0
	for _, name := range all {
		if _, ok := keepSet[name]; ok {
			continue
		}
		if err := p.DestroySlot(&model.DestroySlotRequest{Name: name}); err != nil {
			p.Log.Warn("namespace cleanup failed", "slot", name, "error", err)
			continue
		}
		p.Log.Info("orphaned namespace deleted", "slot", name)
		cleaned++
	}
	return cleaned, nil
}


