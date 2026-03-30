import DataTable, { type Column } from '../components/DataTable'
import { usePolling } from '../hooks/usePolling'
import { statsApi } from '../api/stats'
import type { DestinationStat } from '../api/types'

export default function DestinationStats() {
  const { data } = usePolling(() => statsApi.getDestinations(200), { intervalMs: 5000 })
  const destinations = data?.destinations ?? []

  const columns: Column<DestinationStat>[] = [
    { key: 'domain', label: 'Domain / Destination', sortable: true, render: r => <span className="font-medium text-text-primary">{r.domain}</span> },
    { key: 'connections', label: 'Total Connections', sortable: true },
    { key: 'bytes_sent', label: 'Data Sent', sortable: true, render: r => formatBytes(r.bytes_sent) },
    { key: 'bytes_received', label: 'Data Received', sortable: true, render: r => formatBytes(r.bytes_received) },
    { key: 'last_accessed', label: 'Last Accessed', sortable: true, render: r => new Date(r.last_accessed * 1000).toLocaleString() },
  ]

  return (
    <div>
      <h2 className="text-2xl font-bold mb-6">Destination Statistics</h2>
      <p className="text-sm text-text-secondary mb-6">Top traffic destinations accessed across all slots.</p>
      <DataTable data={destinations} columns={columns} keyField="domain" pageSize={20} />
    </div>
  )
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}
