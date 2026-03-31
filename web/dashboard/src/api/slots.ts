import { apiFetch } from './client'
import type { Slot } from './types'

export const slotsApi = {
  list: () => apiFetch<Slot[]>('/slots'),

  get: (name: string) => apiFetch<Slot>(`/slots/${name}`),

  changeIp: (name: string) =>
    apiFetch<Slot>(`/slots/${name}/changeip`, { method: 'POST' }),

  delete: (name: string) =>
    apiFetch<string>(`/slots/${name}`, { method: 'DELETE' }),
}
