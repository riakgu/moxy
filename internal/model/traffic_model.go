package model

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

type TrafficListResponse struct {
	Entries          []TrafficEntryResponse `json:"entries"`
	TotalEntries     int                    `json:"total_entries"`
	TotalConnections int64                  `json:"total_connections"`
	TotalActive      int64                  `json:"total_active"`
	TotalTxBytes     uint64                 `json:"total_tx_bytes"`
	TotalRxBytes     uint64                 `json:"total_rx_bytes"`
}
