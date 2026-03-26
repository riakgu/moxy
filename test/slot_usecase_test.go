package test

import (
	"testing"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

func TestSlotUseCase_SelectRandom_ReturnsHealthySlot(t *testing.T) {
	uc := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	uc.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", IPv4Address: "1.1.1.1", Healthy: true},
		{Name: "slot1", IPv4Address: "2.2.2.2", Healthy: true},
	})

	slot, err := uc.SelectRandom()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slot.Status != entity.SlotStatusHealthy {
		t.Errorf("expected healthy slot, got %s", slot.Status)
	}
}

func TestSlotUseCase_SelectRandom_NoHealthySlots(t *testing.T) {
	uc := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	uc.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", Healthy: false},
	})

	_, err := uc.SelectRandom()
	if err == nil {
		t.Fatal("expected error when no healthy slots available")
	}
}

func TestSlotUseCase_SelectByName_Found(t *testing.T) {
	uc := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	uc.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", IPv4Address: "1.1.1.1", Healthy: true},
		{Name: "slot5", IPv4Address: "5.5.5.5", Healthy: true},
	})

	slot, err := uc.SelectByName("slot5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slot.Name != "slot5" {
		t.Errorf("expected slot5, got %s", slot.Name)
	}
}

func TestSlotUseCase_SelectByName_NotFound(t *testing.T) {
	uc := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	uc.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", Healthy: true},
	})

	_, err := uc.SelectByName("slot99")
	if err == nil {
		t.Fatal("expected error for missing slot")
	}
}

func TestSlotUseCase_SelectByName_Unhealthy(t *testing.T) {
	uc := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	uc.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", Healthy: false},
	})

	_, err := uc.SelectByName("slot0")
	if err == nil {
		t.Fatal("expected error for unhealthy slot")
	}
}

func TestSlotUseCase_ListAll(t *testing.T) {
	uc := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	uc.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", Healthy: true},
		{Name: "slot1", Healthy: false},
	})

	slots := uc.ListAll()
	if len(slots) != 2 {
		t.Errorf("expected 2 slots, got %d", len(slots))
	}
}

