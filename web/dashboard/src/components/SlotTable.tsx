import type { Slot } from '../api/types'
import SlotRow from './SlotRow'

interface SlotTableProps {
  slots: Slot[]
  onChangeIP: (name: string) => Promise<void>
  onDelete: (name: string) => Promise<void>
  host: string
}

export default function SlotTable({ slots, onChangeIP, onDelete, host }: SlotTableProps) {
  if (slots.length === 0) {
    return (
      <p className="py-6 text-center text-sm text-text-muted font-mono">
        No slots provisioned — use Provision to add slots
      </p>
    )
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left">
        <thead>
          <tr className="border-b border-border-subtle text-xs text-text-muted uppercase tracking-wider">
            <th className="py-2 px-3 font-medium">Name</th>
            <th className="py-2 px-3 font-medium">Public IPv4</th>
            <th className="py-2 px-3 font-medium">IPv6</th>
            <th className="py-2 px-3 font-medium">Status</th>
            <th className="py-2 px-3 font-medium">Conns</th>
            <th className="py-2 px-3 font-medium">Last Check</th>
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
            />
          ))}
        </tbody>
      </table>
    </div>
  )
}
