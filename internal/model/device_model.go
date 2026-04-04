package model

type ProvisionDeviceRequest struct {
	Alias string `json:"-"`
	Slots int    `json:"slots"`
}

type DeviceResponse struct {
	Alias       string `json:"alias"`
	Serial      string `json:"serial"`
	Carrier     string `json:"carrier"`
	Interface   string `json:"interface"`
	Nameserver  string `json:"nameserver"`
	NAT64Prefix string `json:"nat64_prefix"`
	Status      string `json:"status"`
	SlotCount   int    `json:"slot_count"`
}

type ScanResponse struct {
	Discovered int              `json:"discovered"`
	SetupOk    int              `json:"setup_ok"`
	Failed     int              `json:"failed"`
	Devices    []DeviceResponse `json:"devices"`
}

type ISPProbeResult struct {
	Nameserver  string `json:"nameserver"`
	NAT64Prefix string `json:"nat64_prefix"`
}
