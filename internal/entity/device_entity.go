package entity

const (
	DeviceStatusDetected = "detected"
	DeviceStatusOffline  = "offline"
	DeviceStatusSetup    = "setup"
	DeviceStatusOnline   = "online"
	DeviceStatusError    = "error"
)

type Device struct {
	Alias       string
	Serial      string
	Carrier     string
	Interface   string
	Nameserver  string
	NAT64Prefix string
	Status      string
}
