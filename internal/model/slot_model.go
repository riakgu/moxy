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

type ProvisionRequest struct {
	Interface string `json:"interface" validate:"required"`
	Slots     int    `json:"slots" validate:"required,min=1,max=500"`
	DNS64     string `json:"dns64"`
}

type ProvisionResponse struct {
	Created int `json:"created"`
	Failed  int `json:"failed"`
	Total   int `json:"total"`
}

type DeleteSlotRequest struct {
	SlotName string `validate:"required" json:"-"`
}
