package model

type RegisterDeviceRequest struct {
	Serial   string `json:"serial" validate:"required,max=100"`
	Alias    string `json:"alias" validate:"required,max=50"`
	MaxSlots int    `json:"max_slots"`
}

type SetupDeviceRequest struct {
	DeviceId string `json:"-" validate:"required"`
}

type TeardownDeviceRequest struct {
	DeviceId string `json:"-" validate:"required"`
}

type UpdateISPOverrideRequest struct {
	DeviceId    string `json:"-" validate:"required"`
	Nameserver  string `json:"nameserver"`
	NAT64Prefix string `json:"nat64_prefix"`
}

type DeviceResponse struct {
	ID        string `json:"id"`
	Serial    string `json:"serial"`
	Alias     string `json:"alias"`
	Carrier   string `json:"carrier"`
	Interface string `json:"interface"`
	Status    string `json:"status"`
	MaxSlots  int    `json:"max_slots"`
	SlotCount int    `json:"slot_count"`
}

type SetupProgressResponse struct {
	DeviceId       string   `json:"device_id"`
	Status         string   `json:"status"`
	CompletedSteps []string `json:"completed_steps"`
	FailedAt       string   `json:"failed_at,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type ISPProbeResult struct {
	Nameserver  string `json:"nameserver"`
	NAT64Prefix string `json:"nat64_prefix"`
}
