package converter

import (
	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
)

func SlotToResponse(slot *entity.Slot) *model.SlotResponse {
	return &model.SlotResponse{
		Name:              slot.Name,
		DeviceAlias:       slot.DeviceAlias,
		Interface:         slot.Interface,
		Nameserver:        slot.Nameserver,
		NAT64Prefix:       slot.NAT64Prefix,
		IPv6Address:       slot.IPv6Address,
		PublicIPv4:        slot.PublicIPv4,
		Status:            slot.Status,
		ActiveConnections: slot.ActiveConnections,
		LastCheckedAt:     slot.LastCheckedAt,
	}
}
