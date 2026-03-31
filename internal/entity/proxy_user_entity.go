package entity

type ProxyUser struct {
	ID            string
	Username      string
	PasswordHash  string
	DeviceBinding string
	Enabled       bool
	CreatedAt     int64
	UpdatedAt     int64
}
