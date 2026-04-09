package converter

import (
	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
)

func TrafficEntryToResponse(entry *entity.TrafficEntry) model.TrafficEntryResponse {
	return model.TrafficEntryResponse{
		Domain:            entry.Domain,
		Port:              entry.Port,
		DeviceAlias:       entry.DeviceAlias,
		Protocol:          entry.Protocol,
		Transport:         entry.Transport,
		ConnectionCount:   entry.ConnectionCount,
		ActiveConnections: entry.ActiveConnections,
		TxBytes:           entry.TxBytes,
		RxBytes:           entry.RxBytes,
		FirstSeenAt:       entry.FirstSeenAt,
		LastSeenAt:        entry.LastSeenAt,
	}
}
