package entity

const (
	SlotStatusHealthy     = "healthy"
	SlotStatusUnhealthy   = "unhealthy"
	SlotStatusDiscovering = "discovering"
)

type Slot struct {
	Name              string
	DeviceAlias       string
	Interface         string
	Nameserver        string
	NAT64Prefix       string
	IPv6Address       string
	PublicIPv4        string
	Status            string
	ActiveConnections int64
	LastCheckedAt     int64
	NextCheckAt       int64
	MonitorState      string
}
