//go:build linux

package proxy

import (
	"context"
	"fmt"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
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

// convertSlot is a helper to convert an entity slot to a response (used when auth is needed).
func convertSlot(slotUC *usecase.SlotUseCase, slotName string) (*model.SlotResponse, error) {
	slot, err := slotUC.SelectByName(slotName)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", model.ErrSlotNotFound, slotName)
	}
	return converter.SlotToResponse(slot), nil
}
