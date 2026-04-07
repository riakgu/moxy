import { apiFetch } from './client'
import type { MoxyConfig } from './types'

export function getConfig(): Promise<MoxyConfig> {
  return apiFetch<MoxyConfig>('/config')
}

export async function saveConfig(config: MoxyConfig): Promise<MoxyConfig> {
  return apiFetch<MoxyConfig>('/config', {
    method: 'PUT',
    body: JSON.stringify(config),
  })
}

export async function restartService(): Promise<string> {
  return apiFetch<string>('/restart', { method: 'POST' })
}
