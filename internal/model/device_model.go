package model

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
	SetupStep      string `json:"setup_step,omitempty"`
	SlotCount      int    `json:"slot_count"`
	UniqueIPs      int    `json:"unique_ips"`
	TxBytes        uint64 `json:"tx_bytes"`
	RxBytes        uint64 `json:"rx_bytes"`
	TotalBytes     uint64 `json:"total_bytes"`
}

type ScanResponse struct {
	Discovered int              `json:"discovered"`
	Devices    []DeviceResponse `json:"devices"`
}

type SetupResponse struct {
	Device    DeviceResponse     `json:"device"`
	Provision *ProvisionResponse `json:"provision,omitempty"`
}
