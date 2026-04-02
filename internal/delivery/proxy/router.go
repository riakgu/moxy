//go:build linux

package proxy

import (
	"context"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

// SlotRouter decides which slot to use for a proxy connection.
type SlotRouter interface {
	// Route selects a slot and returns the slot name.
	// clientIP is used for sticky-IP strategy.
	Route(ctx context.Context, clientIP string) (string, error)
}

// strategyRouter selects slots using the configured load balancing strategy.
type strategyRouter struct {
	slotUC *usecase.SlotUseCase
}

// NewStrategyRouter creates a router that uses the slot usecase's configured strategy.
func NewStrategyRouter(slotUC *usecase.SlotUseCase) SlotRouter {
	return &strategyRouter{slotUC: slotUC}
}

func (r *strategyRouter) Route(ctx context.Context, clientIP string) (string, error) {
	slot, err := r.slotUC.SelectSlot(clientIP)
	if err != nil {
		return "", model.ErrNoSlotsAvailable
	}
	return slot.Name, nil
}

// fixedSlotRouter always returns the same slot. Used by port-based handler.
type fixedSlotRouter struct {
	slotName string
}

// NewFixedSlotRouter creates a router that always returns a specific slot.
func NewFixedSlotRouter(slotName string) SlotRouter {
	return &fixedSlotRouter{slotName: slotName}
}

func (r *fixedSlotRouter) Route(ctx context.Context, clientIP string) (string, error) {
	return r.slotName, nil
}
