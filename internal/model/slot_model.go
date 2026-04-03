package model

type SlotResponse struct {
	Name              string `json:"name"`
	DeviceAlias       string `json:"device_alias"`
	Interface         string `json:"interface"`
	Nameserver        string `json:"nameserver"`
	NAT64Prefix       string `json:"nat64_prefix"`
	IPv6Address       string `json:"ipv6_address"`
	PublicIPv4        string `json:"public_ipv4"`
	Status            string `json:"status"`
	ActiveConnections int64  `json:"active_connections"`
	LastCheckedAt     int64  `json:"last_checked_at"`
}

type GetSlotRequest struct {
	SlotName string `validate:"required" json:"-"`
}

type ChangeIPRequest struct {
	SlotName string `validate:"required" json:"-"`
}

type ProvisionResponse struct {
	Created   int `json:"created"`
	Failed    int `json:"failed"`
	Total     int `json:"total"`
	UniqueIPs int `json:"unique_ips"`
}

type DiscoveredSlot struct {
	Name        string
	IPv4Address string
	IPv6Address string
	Healthy     bool
}
