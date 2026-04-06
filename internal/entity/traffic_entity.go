package entity

// TrafficKey uniquely identifies a traffic entry by destination, device, and protocol.
type TrafficKey struct {
	Domain      string // hostname extracted from host:port
	Port        string // "443", "80", etc.
	DeviceAlias string // "dev1", "dev2"
	Protocol    string // "ipv4" or "ipv6"
}

// TrafficEntry holds per-destination traffic statistics.
// All counter fields are updated atomically.
type TrafficEntry struct {
	TrafficKey
	ConnectionCount   int64  // total lifetime connections
	ActiveConnections int64  // currently open
	TxBytes           uint64 // bytes sent to destination
	RxBytes           uint64 // bytes received from destination
	FirstSeenAt       int64  // unix millis
	LastSeenAt        int64  // unix millis
}
