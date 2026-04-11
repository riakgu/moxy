// API wrapper — all backend endpoints return { data: T }
export interface ApiResponse<T> {
  data: T
}

// Device — matches model.DeviceResponse
export interface Device {
  alias: string
  serial: string
  model: string
  brand: string
  android_version: string
  carrier: string
  interface: string
  nameserver: string
  nat64_prefix: string
  status: 'detected' | 'offline' | 'setup' | 'online' | 'error' | 'disconnected'
  setup_step?: string
  slot_count: number
  unique_ips: number
  tx_bytes: number
  rx_bytes: number
  total_bytes: number
}

// Slot — matches model.SlotResponse
export interface Slot {
  name: string
  device_alias: string
  interface: string
  nameserver: string
  nat64_prefix: string
  ipv6_address: string
  public_ipv4s: string[]
  city: string
  asn: string
  org: string
  rtt: string
  status: 'healthy' | 'unhealthy' | 'discovering' | 'suspended'
  active_connections: number
  last_checked_at: number
  next_check_at: number
  last_used_at: number
  monitor_state: string
  ip_changed_at: number
  ip_change_count: number
}

// Scan — matches model.ScanResponse
export interface ScanResponse {
  discovered: number
  devices: Device[]
}

// Provision — matches model.ProvisionResponse
export interface ProvisionResponse {
  created: number
  failed: number
  total: number
  unique_ips: number
}

export interface CleanupResponse {
  cleaned: number
}

// Setup — matches model.SetupResponse
export interface SetupResponse {
  device: Device
  provision?: ProvisionResponse
}

// LogEntry — matches sse.LogEntry
export interface LogEntry {
  time: number
  level: string
  msg: string
  component?: string
  attrs?: Record<string, string>
}

// Config — mirrors config.json structure
export interface MoxyConfig {
  proxy: {
    ipv4: {
      port: number
      slot_port_start: number
    }
    ipv6: {
      port: number
      slot_port_start: number
    }
    source_ip_strategy: string
    udp_idle_timeout_seconds: number
    udp_max_associations: number
  }
  api: {
    port: number
  }
  devices: {
    grace_period_seconds: number
    watcher_reconnect_max_seconds: number
    drain_timeout_seconds: number
  }
  slots: {
    max_slots_per_device: number
    ip_check_host: string
    monitor_fast_interval_seconds: number
    monitor_steady_interval_seconds: number
    monitor_recovery_interval_seconds: number
    monitor_fast_ticks: number
    monitor_unhealthy_threshold: number
  }
  dns: {
    cache_max_entries_per_device: number
    cache_min_ttl_seconds: number
    cache_max_ttl_seconds: number
  }
  traffic: {
    max_tracked: number
  }
  sse: {
    debounce_ms: number
    heartbeat_seconds: number
    max_clients: number
    traffic_snapshot_limit: number
  }
  server: {
    shutdown_drain_seconds: number
  }
  log: {
    level: string
    format: string
    ring_buffer_size?: number
  }
}

// Traffic — matches model.TrafficEntryResponse
export interface TrafficEntry {
  domain: string
  port: string
  device_alias: string
  protocol: string
  transport: string
  connection_count: number
  active_connections: number
  tx_bytes: number
  rx_bytes: number
  first_seen_at: number
  last_seen_at: number
}

// TrafficList — matches model.TrafficListResponse
export interface TrafficList {
  entries: TrafficEntry[]
  total_entries: number
  total_connections: number
  total_active: number
  total_tx_bytes: number
  total_rx_bytes: number
  device_totals: Record<string, { tx_bytes: number; rx_bytes: number }>
}

// DNS Cache — matches model.DNSCacheStatsResponse
export interface DNSCacheStats {
  total_entries: number
  total_hits: number
  total_misses: number
  total_hit_rate_percent: number
}
