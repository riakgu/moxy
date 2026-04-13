package model

type MoxyConfig struct {
	Proxy   ProxyConfig   `json:"proxy"`
	API     APIConfig     `json:"api"`
	Devices DevicesConfig `json:"devices"`
	Slots   SlotsConfig   `json:"slots"`
	DNS     DNSConfig     `json:"dns"`
	Traffic TrafficConfig `json:"traffic"`
	SSE     SSEConfig     `json:"sse"`
	Server  ServerConfig  `json:"server"`
	Log     LogConfig     `json:"log"`
}

type ProxyConfig struct {
	IPv4                  ProxyPortConfig `json:"ipv4"`
	IPv6                  ProxyPortConfig `json:"ipv6"`
	SourceIPStrategy      string          `json:"source_ip_strategy"`
	UDPIdleTimeoutSeconds int             `json:"udp_idle_timeout_seconds"`
	UDPMaxAssociations    int             `json:"udp_max_associations"`
}

type ProxyPortConfig struct {
	Port          int `json:"port"`
	SlotPortStart int `json:"slot_port_start"`
}

type APIConfig struct {
	Port int `json:"port"`
}

type DevicesConfig struct {
	MaxDevices                 int `json:"max_devices"`
	GracePeriodSeconds         int `json:"grace_period_seconds"`
	WatcherReconnectMaxSeconds int `json:"watcher_reconnect_max_seconds"`
	DrainTimeoutSeconds        int `json:"drain_timeout_seconds"`
}

type SlotsConfig struct {
	MaxSlots                       int    `json:"max_slots"`
	MaxSlotsPerDevice              int    `json:"max_slots_per_device"`
	IPCheckHost                    string `json:"ip_check_host"`
	MonitorSteadyIntervalSeconds   int    `json:"monitor_steady_interval_seconds"`
	MonitorRecoveryIntervalSeconds int    `json:"monitor_recovery_interval_seconds"`
	MonitorUnhealthyThreshold      int    `json:"monitor_unhealthy_threshold"`
}

type DNSConfig struct {
	CacheMaxEntriesPerDevice int `json:"cache_max_entries_per_device"`
	CacheMinTTLSeconds       int `json:"cache_min_ttl_seconds"`
	CacheMaxTTLSeconds       int `json:"cache_max_ttl_seconds"`
}

type TrafficConfig struct {
	MaxTracked int `json:"max_tracked"`
}

type SSEConfig struct {
	DebounceMs           int `json:"debounce_ms"`
	HeartbeatSeconds     int `json:"heartbeat_seconds"`
	MaxClients           int `json:"max_clients"`
	TrafficSnapshotLimit int `json:"traffic_snapshot_limit"`
}

type ServerConfig struct {
	ShutdownDrainSeconds int `json:"shutdown_drain_seconds"`
}

type LogConfig struct {
	Level          string `json:"level"`
	Format         string `json:"format"`
	RingBufferSize int    `json:"ring_buffer_size,omitempty"`
}

