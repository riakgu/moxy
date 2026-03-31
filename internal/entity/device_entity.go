package entity

const (
	DeviceStatusOffline = "offline"
	DeviceStatusSetup   = "setup"
	DeviceStatusOnline  = "online"
	DeviceStatusError   = "error"
)

type Device struct {
	ID          string
	Serial      string
	Alias       string
	Carrier     string
	Interface   string
	Nameserver  string
	NAT64Prefix string
	Status      string
	MaxSlots    int
	CreatedAt   int64
	UpdatedAt   int64
}
