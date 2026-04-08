import { apiFetch } from './client'
import type { DNSCacheStats } from './types'

export async function getDNSStats(): Promise<DNSCacheStats> {
  return apiFetch<DNSCacheStats>('/dns/stats')
}
