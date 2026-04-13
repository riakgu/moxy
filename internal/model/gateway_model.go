package model

type ISPProbeResult struct {
	Nameserver  string `json:"nameserver"`
	NAT64Prefix string `json:"nat64_prefix"`
}

type DeviceEvent struct {
	Serial string
	Status string
}

type CreateSlotRequest struct {
	SlotIndex int
	Interface string
	DNS64     string
}

type DestroySlotRequest struct {
	Name string
}

type ReattachSlotRequest struct {
	SlotName  string
	Interface string
}

type EnableNDPProxyRequest struct {
	Interface string
}

type NDPProxyEntryRequest struct {
	IPv6      string
	Interface string
}

type CleanupNamespacesRequest struct {
	Keep []string
}

type ConfigureDHCPRequest struct {
	Interface string
}

type ConfigureIPv6SLAACRequest struct {
	Interface string
}

type BringInterfaceUpRequest struct {
	Interface string
}

type ResolveSlotRequest struct {
	SlotName   string
	Nameserver string
}

type SlotIPInfoResult struct {
	IP   string
	City string
	ASN  string
	Org  string
	RTT  string
}

type DialRequest struct {
	SlotName    string
	Addr        string
	Nameserver  string
	NAT64Prefix string
}

type ADBDeviceRequest struct {
	Serial string
}

type ADBDeviceInfoResult struct {
	Model          string
	Brand          string
	AndroidVersion string
}
