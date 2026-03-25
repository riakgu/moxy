package model

import "errors"

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrNoSlotsAvailable   = errors.New("no healthy slots available")
	ErrSlotNotFound       = errors.New("slot not found")
	ErrSlotUnhealthy      = errors.New("slot is unhealthy")
	ErrSlotBusy           = errors.New("slot has active connections")
)
