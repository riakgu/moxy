package converter

import (
	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
)

func SlotToResponse(slot *entity.Slot) *model.SlotResponse {
	return &model.SlotResponse{
		Name:              slot.Name,
		IPv6Address:       slot.IPv6Address,
		PublicIPv4:        slot.PublicIPv4,
		Status:            slot.Status,
		ActiveConnections: slot.ActiveConnections,
		BytesSent:         slot.BytesSent,
		BytesReceived:     slot.BytesReceived,
		LastCheckedAt:     slot.LastCheckedAt,
	}
}
