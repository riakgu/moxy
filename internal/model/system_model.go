package model

type SystemStatsResponse struct {
	// Host
	CPUPercent     float64 `json:"cpu_percent"`
	MemUsedBytes   uint64  `json:"mem_used_bytes"`
	MemTotalBytes  uint64  `json:"mem_total_bytes"`
	Temperature    float64 `json:"temperature"`
	LoadAvg1       float64 `json:"load_avg_1"`
	LoadAvg5       float64 `json:"load_avg_5"`
	LoadAvg15      float64 `json:"load_avg_15"`
	DiskUsedBytes  uint64  `json:"disk_used_bytes"`
	DiskTotalBytes uint64  `json:"disk_total_bytes"`
	HostUptime     int64   `json:"host_uptime_seconds"`

	// Process
	ProcessUptime int64  `json:"process_uptime_seconds"`
	Goroutines    int    `json:"goroutines"`
	GoVersion     string `json:"go_version"`
	Hostname      string `json:"hostname"`
	Arch          string `json:"arch"`
}
