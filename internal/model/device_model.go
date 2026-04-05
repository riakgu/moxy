package model

type ProvisionDeviceRequest struct {
	Alias string `json:"-"`
	Slots int    `json:"slots"`
}

type DeviceResponse struct {
	Alias          string `json:"alias"`
	Serial         string `json:"serial"`
	Model          string `json:"model"`
	Brand          string `json:"brand"`
	AndroidVersion string `json:"android_version"`
	Carrier        string `json:"carrier"`
	Interface      string `json:"interface"`
	Nameserver     string `json:"nameserver"`
	NAT64Prefix    string `json:"nat64_prefix"`
	Status         string `json:"status"`
	SlotCount      int    `json:"slot_count"`
	UniqueIPs      int    `json:"unique_ips"`
}

type ScanResponse struct {
	Discovered int              `json:"discovered"`
	Devices    []DeviceResponse `json:"devices"`
}

type SetupResponse struct {
	Device    DeviceResponse     `json:"device"`
	Provision *ProvisionResponse `json:"provision,omitempty"`
}

type ISPProbeResult struct {
	Nameserver  string `json:"nameserver"`
	NAT64Prefix string `json:"nat64_prefix"`
}

// DeviceEvent represents a device connect/disconnect event from the ADB watcher.
type DeviceEvent struct {
	Serial string
	Status string // "connected" or "disconnected"
}
