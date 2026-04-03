package model

import "errors"

var (
	ErrNoSlotsAvailable   = errors.New("no healthy slots available")
	ErrSlotNotFound       = errors.New("slot not found")
	ErrSlotUnhealthy      = errors.New("slot is unhealthy")
	ErrSlotBusy           = errors.New("slot has active connections")
	ErrTooManyConnections = errors.New("too many concurrent connections")
)
