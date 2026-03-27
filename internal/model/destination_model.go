package model

type DestinationStat struct {
	Domain        string `json:"domain"`
	SlotName      string `json:"slot_name,omitempty"`
	Connections   int64  `json:"connections"`
	BytesSent     int64  `json:"bytes_sent"`
	BytesReceived int64  `json:"bytes_received"`
	LastAccessed  int64  `json:"last_accessed"`
}

type DestinationStatsResponse struct {
	TotalDomains int                `json:"total_domains"`
	Destinations []DestinationStat  `json:"destinations"`
}
