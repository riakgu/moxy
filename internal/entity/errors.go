package entity

import "errors"

var (
	ErrNoSlotsAvailable  = errors.New("no healthy slots available")
	ErrSlotNotFound      = errors.New("slot not found")
	ErrSlotBusy          = errors.New("slot has active connections")
	ErrDeviceNotDetected = errors.New("device is not in 'detected' state")
)
