import { useState } from 'react'
import type { Slot } from '../api/types'

interface SlotRowProps {
  slot: Slot
  onChangeIP: (name: string) => Promise<void>
  onDelete: (name: string) => Promise<void>
  host: string
  now: number
}

function timeAgo(timestampMs: number, now: number): string {
  if (!timestampMs) return 'Never'
  const diffMs = now - timestampMs
  if (diffMs < 5_000) return 'just now'
  const seconds = Math.floor(diffMs / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  const remainSec = seconds % 60
  if (minutes < 60) return `${minutes}m ${remainSec}s ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ${minutes % 60}m ago`
  return `${Math.floor(hours / 24)}d ago`
}

const statusStyles: Record<string, { dot: string; text: string; class: string }> = {
  healthy: { dot: 'bg-accent-green', text: 'Healthy', class: 'text-accent-green' },
  unhealthy: { dot: 'bg-accent-red', text: 'Unhealthy', class: 'text-accent-red' },
  discovering: { dot: 'bg-accent-amber animate-pulse-badge', text: 'Discovering', class: 'text-accent-amber' },
  suspended: { dot: 'bg-accent-amber', text: '⏸ Suspended', class: 'text-accent-amber' },
}

export default function SlotRow({ slot, onChangeIP, onDelete, now }: SlotRowProps) {
  const [changingIP, setChangingIP] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const status = statusStyles[slot.status] ?? statusStyles.unhealthy

  const handleChangeIP = async () => {
    setChangingIP(true)
    try {
      await onChangeIP(slot.name)
    } finally {
      setChangingIP(false)
    }
  }

  const handleDelete = async () => {
    if (!window.confirm(`Delete ${slot.name}? This will destroy the namespace.`)) return
    setDeleting(true)
    try {
      await onDelete(slot.name)
    } finally {
      setDeleting(false)
    }
  }

  return (
    <tr className="slot-row border-b border-border-subtle/50 hover:bg-bg-surface-hover/50 transition-colors">
      <td className="py-2.5 px-3 font-mono text-sm text-accent-cyan">{slot.name}</td>
      <td className="py-2.5 px-3 font-mono text-sm">
        {slot.ipv4_address || '—'}
      </td>
      <td className="py-2.5 px-3 text-sm text-text-secondary">{slot.city || '—'}</td>
      <td className="py-2.5 px-3">
        <span className={`inline-flex items-center gap-1.5 text-xs font-medium ${status.class}`}>
          <span className={`w-1.5 h-1.5 rounded-full ${status.dot}`} />
          {status.text}
        </span>
      </td>
      <td className="py-2.5 px-3 text-xs font-mono text-text-secondary">
        {slot.rtt || '—'}
      </td>
      <td className="py-2.5 px-3 text-xs font-mono text-text-secondary">
        {slot.active_connections > 0 ? (
          <span className="text-accent-cyan">{slot.active_connections}</span>
        ) : '0'}
      </td>
      <td className="py-2.5 px-3 text-xs text-text-muted font-mono">
        {timeAgo(slot.last_used_at, now)}
      </td>
      <td className="py-2.5 px-3">
        <div className="flex items-center gap-1.5">
          <button
            onClick={handleChangeIP}
            disabled={changingIP}
            className="px-2 py-1 text-xs rounded bg-accent-cyan/10 text-accent-cyan
              hover:bg-accent-cyan/20 disabled:opacity-50 transition-colors cursor-pointer disabled:cursor-wait"
          >
            {changingIP ? (
              <span className="inline-block w-3 h-3 border border-accent-cyan border-t-transparent rounded-full animate-spin-slow" />
            ) : (
              'Change IP'
            )}
          </button>
          <button
            onClick={handleDelete}
            disabled={deleting}
            className="px-2 py-1 text-xs rounded bg-accent-red/10 text-accent-red
              hover:bg-accent-red/20 disabled:opacity-50 transition-colors cursor-pointer disabled:cursor-wait"
          >
            {deleting ? '...' : 'Delete'}
          </button>
        </div>
      </td>
    </tr>
  )
}
