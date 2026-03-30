import { apiFetch } from './client'
import type { Slot, ProvisionRequest, ProvisionResponse } from './types'

export const slotsApi = {
  list: () => apiFetch<Slot[]>('/slots'),

  get: (name: string) => apiFetch<Slot>(`/slots/${name}`),

  changeIp: (name: string) =>
    apiFetch<Slot>(`/slots/${name}/changeip`, { method: 'POST' }),

  delete: (name: string) =>
    apiFetch<string>(`/slots/${name}`, { method: 'DELETE' }),

  provision: (req: ProvisionRequest) =>
    apiFetch<ProvisionResponse>('/provision', {
      method: 'POST',
      body: JSON.stringify(req),
    }),

  teardown: () =>
    apiFetch<ProvisionResponse>('/teardown', { method: 'POST' }),
}
