// API wrapper — all backend endpoints return { data: T }
export interface ApiResponse<T> {
  data: T
}

// Device — matches model.DeviceResponse
export interface Device {
  alias: string
  serial: string
  carrier: string
  interface: string
  nameserver: string
  nat64_prefix: string
  status: 'offline' | 'setup' | 'online' | 'error'
  slot_count: number
}

// Slot — matches model.SlotResponse
export interface Slot {
  name: string
  device_alias: string
  interface: string
  nameserver: string
  nat64_prefix: string
  ipv6_address: string
  public_ipv4: string
  status: 'healthy' | 'unhealthy' | 'discovering'
  active_connections: number
  last_checked_at: number
  next_check_at: number
  monitor_state: string
}

// Scan — matches model.ScanResponse
export interface ScanResponse {
  discovered: number
  setup_ok: number
  failed: number
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

