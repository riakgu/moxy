package model

type CreateProxyUserRequest struct {
	Username      string `json:"username" validate:"required,max=100"`
	Password      string `json:"password" validate:"required,min=4"`
	DeviceBinding string `json:"device_binding"`
}

type UpdateProxyUserRequest struct {
	Username      string `json:"-" validate:"required"`
	Password      string `json:"password"`
	DeviceBinding string `json:"device_binding"`
	Enabled       *bool  `json:"enabled"`
}

type ProxyUserResponse struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	DeviceBinding string `json:"device_binding"`
	Enabled       bool   `json:"enabled"`
}
