package test

import (
	"testing"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

func TestDestroySlot_NotFound(t *testing.T) {
	uc := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")

	err := uc.DestroySlot("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent slot")
	}
}

func TestDestroySlot_Busy(t *testing.T) {
	uc := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")

	// Add a slot with active connections
	uc.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", Healthy: true},
	})

	// Simulate active connection
	uc.IncrementConnections("slot0")

	err := uc.DestroySlot("slot0")
	if err == nil {
		t.Fatal("expected error for busy slot")
	}
	if err != model.ErrSlotBusy {
		t.Fatalf("expected ErrSlotBusy, got: %v", err)
	}
}

func TestTeardownAll_EmptySlots(t *testing.T) {
	uc := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")

	resp, err := uc.TeardownAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Total != 0 {
		t.Fatalf("expected 0 destroyed, got %d", resp.Total)
	}
}

