import type { ApiResponse } from './types'

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })

  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || `${res.status} ${res.statusText}`)
  }

  const json: ApiResponse<T> = await res.json()
  return json.data
}
