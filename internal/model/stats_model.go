package model

type StatsResponse struct {
	TotalSlots        int            `json:"total_slots"`
	HealthySlots      int            `json:"healthy_slots"`
	UnhealthySlots    int            `json:"unhealthy_slots"`
	ActiveConnections int64          `json:"active_connections"`
	SlotStats         []SlotResponse `json:"slot_stats"`
}

type HealthResponse struct {
	Status       string `json:"status"`
	HealthySlots int    `json:"healthy_slots"`
	TotalSlots   int    `json:"total_slots"`
}
