package entity

type ProxySession struct {
	ID            string
	SlotName      string
	ClientAddress string
	TargetAddress string
	StartedAt     int64
	BytesSent     int64
	BytesReceived int64
}
