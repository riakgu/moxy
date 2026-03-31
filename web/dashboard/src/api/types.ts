// API wrapper type — all endpoints return { data: T }
export interface ApiResponse<T> {
  data: T
}

// Device
export interface Device {
  id: string
  serial: string
  alias: string
  carrier: string
  interface: string
  status: 'offline' | 'setup' | 'online' | 'error'
  max_slots: number
  slot_count: number
}

export interface RegisterDeviceRequest {
  serial: string
  alias: string
  max_slots?: number
}

export interface SetupProgress {
  device_id: string
  status: 'running' | 'completed' | 'failed'
  completed_steps: string[]
  failed_at?: string
  error?: string
}

export interface UpdateISPOverrideRequest {
  nameserver: string
  nat64_prefix: string
}

// Slot — now device-aware
export interface Slot {
  name: string
  device_alias: string
  interface: string
  ipv6_address: string
  public_ipv4: string
  status: 'healthy' | 'unhealthy' | 'discovering'
  active_connections: number
  bytes_sent: number
  bytes_received: number
  last_checked_at: number
}

// Stats
export interface Stats {
  total_slots: number
  healthy_slots: number
  unhealthy_slots: number
  active_connections: number
  slot_stats: Slot[]
}

// Health
export interface Health {
  status: string
  healthy_slots: number
  total_slots: number
}

// ProxyUser (renamed from User)
export interface ProxyUser {
  id: string
  username: string
  device_binding: string
  enabled: boolean
}

export interface CreateProxyUserRequest {
  username: string
  password: string
  device_binding?: string
}

export interface UpdateProxyUserRequest {
  password?: string
  device_binding?: string
  enabled?: boolean
}

// Provision
export interface ProvisionResponse {
  created: number
  failed: number
  total: number
  duplicates_found: number
  duplicates_resolved: number
  unique_ips: number
}

// Destinations
export interface DestinationStat {
  domain: string
  connections: number
  bytes_sent: number
  bytes_received: number
  last_accessed: number
}

export interface DestinationStatsResponse {
  total_domains: number
  destinations: DestinationStat[]
}

// Config
export interface MoxyConfig {
  proxy: {
    socks5_port: number
    http_port: number
    max_connections: number
    dial_timeout_seconds: number
    idle_timeout_seconds: number
    source_ip_strategy: string
    port_based_start: number
    port_based_end: number
  }
  slots: {
    max_slots_per_device: number
    discovery_interval_seconds: number
    discovery_concurrency: number
    monitor_interval_seconds: number
  }
  provision: {
    dns64_server: string
  }
  database: {
    path: string
  }
  server: {
    shutdown_drain_seconds: number
  }
  log: {
    level: string
    format: string
  }
}

// Logs
export interface LogsResponse {
  lines: string[]
  file: string
}
