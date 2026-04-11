package model

type ISPProbeResult struct {
	Nameserver  string `json:"nameserver"`
	NAT64Prefix string `json:"nat64_prefix"`
}

type DeviceEvent struct {
	Serial string
	Status string
}
