import { useMemo, useState } from 'react'
import DataTable, { type Column } from '../components/DataTable'
import StatusBadge from '../components/StatusBadge'
import CopyButton from '../components/CopyButton'
import { usePolling } from '../hooks/usePolling'
import { statsApi } from '../api/stats'
import type { Slot } from '../api/types'

export default function SlotMonitor() {
  const { data: stats } = usePolling(() => statsApi.getStats(), { intervalMs: 5000 })
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')

  const slots = stats?.slot_stats ?? []

  const filteredSlots = useMemo(() => {
    return slots.filter(s => {
      if (filter !== 'all' && s.status !== filter) return false
      if (search) {
        const q = search.toLowerCase()
        return s.name.toLowerCase().includes(q) ||
               s.ipv6_address.toLowerCase().includes(q) ||
               (s.public_ipv4 && s.public_ipv4.toLowerCase().includes(q))
      }
      return true
    })
  }, [slots, search, filter])

  const columns: Column<Slot>[] = [
    { key: 'name', label: 'Name', sortable: true },
    { key: 'status', label: 'Status', sortable: true, render: row => <StatusBadge status={row.status} /> },
    { key: 'ipv6_address', label: 'IPv6 Address', render: row => (
      <div className="flex items-center gap-2">
        <span className="font-mono text-xs">{row.ipv6_address}</span>
        {row.ipv6_address && <CopyButton text={row.ipv6_address} label="📋" className="px-1.5 py-0.5" />}
      </div>
    )},
    { key: 'public_ipv4', label: 'Public IPv4', render: row => (
      <div className="flex items-center gap-2">
        <span className="font-mono text-xs">{row.public_ipv4 || '—'}</span>
        {row.public_ipv4 && <CopyButton text={row.public_ipv4} label="📋" className="px-1.5 py-0.5" />}
      </div>
    )},
    { key: 'active_connections', label: 'Conns', sortable: true },
    { key: 'bytes_sent', label: 'Sent', sortable: true, render: row => formatBytes(row.bytes_sent) },
    { key: 'bytes_received', label: 'Recv', sortable: true, render: row => formatBytes(row.bytes_received) },
  ]

  return (
    <div>
      <h2 className="text-2xl font-bold mb-6">Slot Monitor</h2>
      <div className="flex flex-wrap items-center gap-4 mb-6">
        <input
          type="text"
          placeholder="Search slots..."
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="px-4 py-2 bg-bg-card border border-border rounded-lg text-sm flex-1 max-w-sm"
        />
        <select
          value={filter}
          onChange={e => setFilter(e.target.value)}
          className="px-4 py-2 bg-bg-card border border-border rounded-lg text-sm"
        >
          <option value="all">All Statuses</option>
          <option value="healthy">Healthy</option>
          <option value="unhealthy">Unhealthy</option>
          <option value="discovering">Discovering</option>
        </select>
      </div>
      <DataTable
        data={filteredSlots}
        columns={columns}
        keyField="name"
        pageSize={20}
      />
    </div>
  )
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}
