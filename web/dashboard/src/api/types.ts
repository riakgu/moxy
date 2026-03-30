// API wrapper type — all endpoints return { data: T }
export interface ApiResponse<T> {
  data: T
}

// Slot
export interface Slot {
  name: string
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

// User
export interface User {
  username: string
  device_binding: string
  enabled: boolean
}

export interface CreateUserRequest {
  username: string
  password: string
  device_binding?: string
  enabled: boolean
}

export interface UpdateUserRequest {
  password?: string
  device_binding?: string
  enabled?: boolean
}

// Provision
export interface ProvisionRequest {
  interface: string
  slots: number
  dns64?: string
}

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

// Config (new endpoint)
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
    max_slots: number
    discovery_interval_seconds: number
    discovery_concurrency: number
    monitor_interval_seconds: number
  }
  provision: {
    interface: string
    dns64_server: string
  }
  server: {
    shutdown_drain_seconds: number
  }
  log: {
    level: string
    format: string
  }
}

// Logs (new endpoint)
export interface LogsResponse {
  lines: string[]
  file: string
}
