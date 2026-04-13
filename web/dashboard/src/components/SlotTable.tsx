import { useState, useEffect } from 'react'
import type { Slot } from '../api/types'
import SlotRow from './SlotRow'

interface SlotTableProps {
  slots: Slot[]
  onChangeIP: (name: string) => Promise<void>
  onDelete: (name: string) => Promise<void>
  host: string
}

const VISIBLE_THRESHOLD = 12

export default function SlotTable({ slots, onChangeIP, onDelete, host }: SlotTableProps) {
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])

  if (slots.length === 0) {
    return (
      <p className="py-6 text-center text-sm text-text-muted font-mono">
        No slots provisioned — use Provision to add slots
      </p>
    )
  }

  return (
    <div>
      <div className="slot-scroll-container">
        <table className="w-full text-left">
          <thead className="sticky top-0 z-10 bg-bg-surface">
            <tr className="border-b border-border-subtle text-xs text-text-muted uppercase tracking-wider">
              <th className="py-2 px-3 font-medium">Name</th>
              <th className="py-2 px-3 font-medium">IPv4 Address</th>
              <th className="py-2 px-3 font-medium">City</th>
              <th className="py-2 px-3 font-medium">Status</th>
              <th className="py-2 px-3 font-medium">RTT</th>
              <th className="py-2 px-3 font-medium">Conns</th>
              <th className="py-2 px-3 font-medium">Last Used</th>
              <th className="py-2 px-3 font-medium">Actions</th>
            </tr>
          </thead>
          <tbody>
            {slots.map((slot) => (
              <SlotRow
                key={slot.name}
                slot={slot}
                onChangeIP={onChangeIP}
                onDelete={onDelete}
                host={host}
                now={now}
              />
            ))}
          </tbody>
        </table>
      </div>
      {slots.length > VISIBLE_THRESHOLD && (
        <p className="pt-2 text-center text-xs text-text-muted font-mono">
          {slots.length} slots · scroll to see all
        </p>
      )}
    </div>
  )
}
