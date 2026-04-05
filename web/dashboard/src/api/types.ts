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
  slot_count: number
  unique_ips: number
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
  monitor_state: string
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

