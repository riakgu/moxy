import { apiFetch } from './client'
import type { MoxyConfig } from './types'

export interface ConfigSaveResult {
  config: MoxyConfig
  restart_required: boolean
}

export function getConfig(): Promise<MoxyConfig> {
  return apiFetch<MoxyConfig>('/config')
}

export async function saveConfig(config: MoxyConfig): Promise<ConfigSaveResult> {
  return apiFetch<ConfigSaveResult>('/config', {
    method: 'PUT',
    body: JSON.stringify(config),
  })
}

export async function restartService(): Promise<string> {
  return apiFetch<string>('/restart', { method: 'POST' })
}
