import { apiFetch } from './client'
import type { Device, ScanResponse, ProvisionResponse } from './types'

export function scanDevices(): Promise<ScanResponse> {
  return apiFetch<ScanResponse>('/devices/scan', { method: 'POST' })
}

export function listDevices(): Promise<Device[]> {
  return apiFetch<Device[]>('/devices')
}

export function getDevice(alias: string): Promise<Device> {
  return apiFetch<Device>(`/devices/${alias}`)
}

export function deleteDevice(alias: string): Promise<boolean> {
  return apiFetch<boolean>(`/devices/${alias}`, { method: 'DELETE' })
}

export function provisionDevice(alias: string, slots: number): Promise<ProvisionResponse> {
  return apiFetch<ProvisionResponse>(`/devices/${alias}/provision`, {
    method: 'POST',
    body: JSON.stringify({ slots }),
  })
}
