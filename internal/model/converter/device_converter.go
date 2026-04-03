package converter

import (
	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
)

func DeviceToResponse(device *entity.Device, slotCount int) *model.DeviceResponse {
	return &model.DeviceResponse{
		ID:          device.ID,
		Serial:      device.Serial,
		Alias:       device.Alias,
		Carrier:     device.Carrier,
		Interface:   device.Interface,
		Nameserver:  device.Nameserver,
		NAT64Prefix: device.NAT64Prefix,
		Status:      device.Status,
		MaxSlots:    device.MaxSlots,
		SlotCount:   slotCount,
	}
}
