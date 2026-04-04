import { useState } from 'react'
import type { Slot } from '../api/types'
import CopyButton from './CopyButton'

interface SlotRowProps {
  slot: Slot
  onChangeIP: (name: string) => Promise<void>
  onDelete: (name: string) => Promise<void>
  host: string
}

function relativeTime(timestampMs: number): string {
  const diffMs = Date.now() - timestampMs
  const seconds = Math.floor(diffMs / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

function extractSlotIndex(name: string): number {
  const match = name.match(/^slot(\d+)$/)
  return match ? parseInt(match[1], 10) : 0
}

const statusStyles: Record<string, { dot: string; text: string; class: string }> = {
  healthy: { dot: 'bg-accent-green', text: 'Healthy', class: 'text-accent-green' },
  unhealthy: { dot: 'bg-accent-red', text: 'Unhealthy', class: 'text-accent-red' },
  discovering: { dot: 'bg-accent-amber animate-pulse-badge', text: 'Discovering', class: 'text-accent-amber' },
}

export default function SlotRow({ slot, onChangeIP, onDelete, host }: SlotRowProps) {
  const [changingIP, setChangingIP] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const status = statusStyles[slot.status] ?? statusStyles.unhealthy

  const slotPort = 10000 + extractSlotIndex(slot.name)
  const proxyString = `socks5://${host}:${slotPort}`

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

  const truncatedIPv6 = slot.ipv6_address.length > 24
    ? slot.ipv6_address.slice(0, 24) + '…'
    : slot.ipv6_address

  return (
    <tr className="border-b border-border-subtle/50 hover:bg-bg-surface-hover/50 transition-colors">
      <td className="py-2.5 px-3 font-mono text-sm text-accent-cyan">{slot.name}</td>
      <td className="py-2.5 px-3 font-mono text-sm">{slot.public_ipv4 || '—'}</td>
      <td className="py-2.5 px-3 font-mono text-xs text-text-secondary" title={slot.ipv6_address}>
        {truncatedIPv6 || '—'}
      </td>
      <td className="py-2.5 px-3">
        <span className={`inline-flex items-center gap-1.5 text-xs font-medium ${status.class}`}>
          <span className={`w-1.5 h-1.5 rounded-full ${status.dot}`} />
          {status.text}
        </span>
      </td>
      <td className="py-2.5 px-3 font-mono text-sm text-text-secondary">{slot.active_connections}</td>
      <td className="py-2.5 px-3 text-xs text-text-muted">
        {slot.last_checked_at ? relativeTime(slot.last_checked_at) : '—'}
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
          <CopyButton text={proxyString} label="Copy" />
        </div>
      </td>
    </tr>
  )
}
