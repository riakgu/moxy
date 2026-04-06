package entity

const (
	DeviceStatusDetected     = "detected"
	DeviceStatusOffline      = "offline"
	DeviceStatusSetup        = "setup"
	DeviceStatusOnline       = "online"
	DeviceStatusError        = "error"
	DeviceStatusDisconnected = "disconnected"
)

type Device struct {
	Alias          string
	Serial         string
	Model          string
	Brand          string
	AndroidVersion string
	Carrier        string
	Interface      string
	Nameserver     string
	NAT64Prefix    string
	Status         string
}
