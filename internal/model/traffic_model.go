package model

// TrafficEntryResponse is the API response for a single traffic entry.
type TrafficEntryResponse struct {
	Domain            string `json:"domain"`
	Port              string `json:"port"`
	DeviceAlias       string `json:"device_alias"`
	Protocol          string `json:"protocol"`
	ConnectionCount   int64  `json:"connection_count"`
	ActiveConnections int64  `json:"active_connections"`
	TxBytes           uint64 `json:"tx_bytes"`
	RxBytes           uint64 `json:"rx_bytes"`
	FirstSeenAt       int64  `json:"first_seen_at"`
	LastSeenAt        int64  `json:"last_seen_at"`
}

// TrafficListResponse is the API response for the traffic list endpoint.
type TrafficListResponse struct {
	Entries      []TrafficEntryResponse `json:"entries"`
	TotalEntries int                    `json:"total_entries"`
}
