import { apiFetch } from './client'
import type { SystemStats } from './types'

export function getSystemStats(): Promise<SystemStats> {
  return apiFetch<SystemStats>('/system/stats')
}

export function restartService(): Promise<string> {
  return apiFetch<string>('/system/restart', { method: 'POST' })
}

export function restartADB(): Promise<string> {
  return apiFetch<string>('/system/restart-adb', { method: 'POST' })
}
