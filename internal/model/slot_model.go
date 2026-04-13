package model

type SlotResponse struct {
	Name              string `json:"name"`
	DeviceAlias       string `json:"device_alias"`
	Interface         string `json:"interface"`
	Nameserver        string `json:"nameserver"`
	NAT64Prefix       string `json:"nat64_prefix"`
	IPv6Address       string `json:"ipv6_address"`
	IPv4Address       string `json:"ipv4_address"`
	City              string   `json:"city"`
	ASN               string   `json:"asn"`
	Org               string   `json:"org"`
	RTT               string   `json:"rtt"`
	Status            string   `json:"status"`
	ActiveConnections int64  `json:"active_connections"`
	LastUsedAt        int64  `json:"last_used_at"`
	MonitorState      string `json:"monitor_state"`

}

type GetSlotRequest struct {
	SlotName string `json:"-"`
}

type ProvisionRequest struct {
	Alias string `json:"-"`
	Slots int    `json:"slots"`
}

type ChangeIPRequest struct {
	SlotName string `json:"-"`
}

type ProvisionResponse struct {
	Created   int `json:"created"`
	Failed    int `json:"failed"`
	Total     int `json:"total"`
	UniqueIPs int `json:"unique_ips"`
}

type CleanupResponse struct {
	Cleaned int `json:"cleaned"`
}

type DeleteSlotRequest struct {
	SlotName string
}

