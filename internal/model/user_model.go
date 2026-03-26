package model

type CreateUserRequest struct {
	Username      string `json:"username" validate:"required"`
	Password      string `json:"password" validate:"required"`
	DeviceBinding string `json:"device_binding"`
	Enabled       bool   `json:"enabled"`
}

type UpdateUserRequest struct {
	Username      string `json:"-" validate:"required"`
	Password      string `json:"password"`
	DeviceBinding string `json:"device_binding"`
	Enabled       *bool  `json:"enabled"`
}

type UserResponse struct {
	Username      string `json:"username"`
	DeviceBinding string `json:"device_binding,omitempty"`
	Enabled       bool   `json:"enabled"`
}
