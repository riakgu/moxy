import { apiFetch } from './client'
import type { Slot, CleanupResponse } from './types'

export function listSlots(): Promise<Slot[]> {
  return apiFetch<Slot[]>('/slots')
}

export function getSlot(name: string): Promise<Slot> {
  return apiFetch<Slot>(`/slots/${name}`)
}

export function changeSlotIP(name: string): Promise<Slot> {
  return apiFetch<Slot>(`/slots/${name}/changeip`, { method: 'POST' })
}

export function deleteSlot(name: string): Promise<string> {
  return apiFetch<string>(`/slots/${name}`, { method: 'DELETE' })
}

export function cleanupOrphans(): Promise<CleanupResponse> {
  return apiFetch<CleanupResponse>('/slots/cleanup', { method: 'POST' })
}
