import { apiFetch } from './client'
import type { Device, RegisterDeviceRequest, SetupProgress, UpdateISPOverrideRequest, ProvisionResponse } from './types'

export const devicesApi = {
  scanADB: () => apiFetch<string[]>('/adb-devices'),

  list: () => apiFetch<Device[]>('/devices'),

  get: (id: string) => apiFetch<Device>(`/devices/${id}`),

  register: (req: RegisterDeviceRequest) =>
    apiFetch<Device>('/devices', {
      method: 'POST',
      body: JSON.stringify(req),
    }),

  setup: (id: string) =>
    apiFetch<SetupProgress>(`/devices/${id}/setup`, { method: 'POST' }),

  teardown: (id: string) =>
    apiFetch<boolean>(`/devices/${id}/teardown`, { method: 'POST' }),

  delete: (id: string) =>
    apiFetch<boolean>(`/devices/${id}`, { method: 'DELETE' }),

  override: (id: string, req: UpdateISPOverrideRequest) =>
    apiFetch<Device>(`/devices/${id}/override`, {
      method: 'PUT',
      body: JSON.stringify(req),
    }),

  provision: (id: string, slots: number) =>
    apiFetch<ProvisionResponse>(`/devices/${id}/provision`, {
      method: 'POST',
      body: JSON.stringify({ slots }),
    }),
}
