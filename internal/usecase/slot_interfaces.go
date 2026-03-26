package usecase

import (
	"io"

	"github.com/riakgu/moxy/internal/entity"
)

type SlotProvisioner interface {
	CreateSlot(slotIndex int, iface string, dns64 string) error
	DestroySlot(name string) error
	EnableNDPProxy(iface string) error
	AddNDPProxyEntry(ipv6 string, iface string) error
	RemoveNDPProxyEntry(ipv6 string, iface string) error
	ListSlotNamespaces() ([]string, error)
}

type SlotDiscovery interface {
	DiscoverAll(slotNames []string) []*entity.Slot
	ResolveSlotIP(slotName string) (string, error)
	ResolveSlotIPv6(slotName string) (string, error)
}

type SlotDialer interface {
	Dial(slotName string, addr string) (io.ReadWriteCloser, error)
}
