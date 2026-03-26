package test

import (
	"testing"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

func TestProxyUseCase_Authenticate_ValidRandom(t *testing.T) {
	slotUC := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	slotUC.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", IPv4Address: "1.1.1.1", Healthy: true},
	})

	proxyUC := usecase.NewProxyUseCase(nil, slotUC, nil, "admin", "secret")

	slot, err := proxyUC.Authenticate(model.ParseProxyAuth("admin", "secret"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slot.Name != "slot0" {
		t.Errorf("expected slot0, got %s", slot.Name)
	}
}

func TestProxyUseCase_Authenticate_ValidSticky(t *testing.T) {
	slotUC := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	slotUC.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", IPv4Address: "1.1.1.1", Healthy: true},
		{Name: "slot3", IPv4Address: "3.3.3.3", Healthy: true},
	})

	proxyUC := usecase.NewProxyUseCase(nil, slotUC, nil, "admin", "secret")

	slot, err := proxyUC.Authenticate(model.ParseProxyAuth("admin-slot3", "secret"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slot.Name != "slot3" {
		t.Errorf("expected slot3, got %s", slot.Name)
	}
}

func TestProxyUseCase_Authenticate_WrongPassword(t *testing.T) {
	proxyUC := usecase.NewProxyUseCase(nil, nil, nil, "admin", "secret")
	_, err := proxyUC.Authenticate(model.ParseProxyAuth("admin", "wrong"))
	if err == nil {
		t.Fatal("expected auth error for wrong password")
	}
}

func TestProxyUseCase_Authenticate_WrongUsername(t *testing.T) {
	proxyUC := usecase.NewProxyUseCase(nil, nil, nil, "admin", "secret")
	_, err := proxyUC.Authenticate(model.ParseProxyAuth("hacker", "secret"))
	if err == nil {
		t.Fatal("expected auth error for wrong username")
	}
}

