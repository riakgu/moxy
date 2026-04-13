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
	IPv4Address       string
	City              string
	ASN               string
	Org               string
	RTT               string
	Status            string
	ActiveConnections int64
	LastUsedAt        int64
	MonitorState      string

}
