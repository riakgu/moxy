package entity

const (
	SlotStatusHealthy     = "healthy"
	SlotStatusUnhealthy   = "unhealthy"
	SlotStatusDiscovering = "discovering"
	SlotStatusSuspended   = "suspended"
)

type Slot struct {
	Name              string
	DeviceAlias       string
	Interface         string
	Nameserver        string
	NAT64Prefix       string
	IPv6Address       string
	PublicIPv4s       []string
	City              string
	ASN               string
	Org               string
	RTT               string
	Status            string
	ActiveConnections int64
	LastCheckedAt     int64
	NextCheckAt       int64
	LastUsedAt        int64
	MonitorState      string
}
