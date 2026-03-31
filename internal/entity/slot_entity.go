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
	IPv6Address       string
	PublicIPv4        string
	Status            string
	ActiveConnections int64
	BytesSent         int64
	BytesReceived     int64
	LastCheckedAt     int64
}
