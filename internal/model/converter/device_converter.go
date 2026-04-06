package converter

import (
	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
)

func DeviceToResponse(device *entity.Device, slotCount int, uniqueIPs int) *model.DeviceResponse {
	return &model.DeviceResponse{
		Alias:          device.Alias,
		Serial:         device.Serial,
		Model:          device.Model,
		Brand:          device.Brand,
		AndroidVersion: device.AndroidVersion,
		Carrier:        device.Carrier,
		Interface:      device.Interface,
		Nameserver:     device.Nameserver,
		NAT64Prefix:    device.NAT64Prefix,
		Status:         device.Status,
		SlotCount:      slotCount,
		UniqueIPs:      uniqueIPs,
		TxBytes:        device.TxBytes,
		RxBytes:        device.RxBytes,
		TotalBytes:     device.TxBytes + device.RxBytes,
	}
}
