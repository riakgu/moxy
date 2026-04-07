package entity

type TrafficKey struct {
	Domain      string
	Port        string
	DeviceAlias string
	Protocol    string
}

type TrafficEntry struct {
	TrafficKey
	ConnectionCount   int64
	ActiveConnections int64
	TxBytes           uint64
	RxBytes           uint64
	FirstSeenAt       int64
	LastSeenAt        int64
}
