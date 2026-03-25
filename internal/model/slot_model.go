package model

type SlotResponse struct {
	Name              string `json:"name"`
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
