package netns_test

import (
	"testing"

	"github.com/riakgu/moxy/internal/gateway/netns"
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
