package test

import (
	"testing"

	"github.com/riakgu/moxy/internal/gateway/netns"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

func TestAddNDPProxyEntry_EmptyIPv6IsNoop(t *testing.T) {
	p := netns.NewProvisioner(nil)
	err := p.AddNDPProxyEntry("", "usb0")
	if err != nil {
		t.Fatalf("expected no error for empty IPv6, got: %v", err)
	}
}

func TestRemoveNDPProxyEntry_EmptyIPv6IsNoop(t *testing.T) {
	p := netns.NewProvisioner(nil)
	err := p.RemoveNDPProxyEntry("", "usb0")
	if err != nil {
		t.Fatalf("expected no error for empty IPv6, got: %v", err)
	}
}

func TestDiscoverAll_NilProvisionerNoCrash(t *testing.T) {
	// Discovery with nil provisioner should work fine (NDP proxy is skipped)
	d := netns.NewDiscovery(nil, 5, nil, "")

	// DiscoverAll with empty slot list should return empty results
	results := d.DiscoverAll([]string{})
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestSlotUseCase_NilProvisionerForNDPProxy(t *testing.T) {
	// SlotUseCase with nil provisioner/discovery should not crash during UpdateSlots
	slotUC := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	slotUC.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", IPv6Address: "2001:db8::1", IPv4Address: "1.1.1.1", Healthy: true},
	})

	slot, err := slotUC.SelectByName("slot0")
	if err != nil {
		t.Fatalf("select should succeed: %v", err)
	}
	if slot.IPv6Address != "2001:db8::1" {
		t.Fatalf("expected IPv6 2001:db8::1, got %s", slot.IPv6Address)
	}
}

