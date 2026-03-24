package test

import (
	"testing"

	"github.com/riakgu/moxy/internal/model"
)

func TestParseProxyAuth_RandomMode(t *testing.T) {
	req := model.ParseProxyAuth("admin", "changeme")
	if req.SlotName != "" {
		t.Errorf("expected empty slot for random mode, got %q", req.SlotName)
	}
	if req.Username != "admin" {
		t.Errorf("expected username 'admin', got %q", req.Username)
	}
}

func TestParseProxyAuth_StickyMode(t *testing.T) {
	req := model.ParseProxyAuth("admin-slot5", "changeme")
	if req.SlotName != "slot5" {
		t.Errorf("expected slot 'slot5', got %q", req.SlotName)
	}
	if req.Username != "admin" {
		t.Errorf("expected username 'admin', got %q", req.Username)
	}
}

func TestParseProxyAuth_StickyModeHyphenatedUsername(t *testing.T) {
	req := model.ParseProxyAuth("my-admin-slot12", "changeme")
	if req.SlotName != "slot12" {
		t.Errorf("expected slot 'slot12', got %q", req.SlotName)
	}
	if req.Username != "my-admin" {
		t.Errorf("expected username 'my-admin', got %q", req.Username)
	}
}

func TestParseProxyAuth_NoSlotSuffix(t *testing.T) {
	req := model.ParseProxyAuth("admin-something", "changeme")
	if req.SlotName != "" {
		t.Errorf("expected empty slot for non-slot suffix, got %q", req.SlotName)
	}
	if req.Username != "admin-something" {
		t.Errorf("expected username 'admin-something', got %q", req.Username)
	}
}
