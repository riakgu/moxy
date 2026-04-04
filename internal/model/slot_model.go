package model

type SlotResponse struct {
	Name              string `json:"name"`
	DeviceAlias       string `json:"device_alias"`
	Interface         string `json:"interface"`
	Nameserver        string `json:"nameserver"`
	NAT64Prefix       string `json:"nat64_prefix"`
	IPv6Address       string `json:"ipv6_address"`
	PublicIPv4s       []string `json:"public_ipv4s"`
	Status            string `json:"status"`
	ActiveConnections int64  `json:"active_connections"`
	LastCheckedAt     int64  `json:"last_checked_at"`
	NextCheckAt       int64  `json:"next_check_at"`
	MonitorState      string `json:"monitor_state"`
}

type GetSlotRequest struct {
	SlotName string `json:"-"`
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

