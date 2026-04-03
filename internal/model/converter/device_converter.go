package converter

import (
	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
)

func DeviceToResponse(device *entity.Device, slotCount int) *model.DeviceResponse {
	return &model.DeviceResponse{
		Alias:       device.Alias,
		Serial:      device.Serial,
		Carrier:     device.Carrier,
		Interface:   device.Interface,
		Nameserver:  device.Nameserver,
		NAT64Prefix: device.NAT64Prefix,
		Status:      device.Status,
		SlotCount:   slotCount,
	}
}
