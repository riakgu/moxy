import { apiFetch } from './client'
import type { Stats, Health, DestinationStatsResponse } from './types'

export const statsApi = {
  getStats: () => apiFetch<Stats>('/stats'),

  getHealth: () => apiFetch<Health>('/health'),

  getDestinations: (limit = 100) =>
    apiFetch<DestinationStatsResponse>(`/destinations?limit=${limit}`),
}
