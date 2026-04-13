import { apiFetch } from './client'
import type { SystemStats } from './types'

export function getSystemStats(): Promise<SystemStats> {
  return apiFetch<SystemStats>('/system/stats')
}

export function restartADB(): Promise<string> {
  return apiFetch<string>('/system/restart-adb', { method: 'POST' })
}

export function cleanupNamespaces(): Promise<{ removed: number }> {
  return apiFetch<{ removed: number }>('/system/cleanup', { method: 'POST' })
}

export function restartService(): Promise<string> {
  return apiFetch<string>('/restart', { method: 'POST' })
}
