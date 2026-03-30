import { useState } from 'react'
import StatCard from '../components/StatCard'
import StatusBadge from '../components/StatusBadge'
import Modal from '../components/Modal'
import { usePolling } from '../hooks/usePolling'
import { statsApi } from '../api/stats'
import { slotsApi } from '../api/slots'
import type { ProvisionRequest } from '../api/types'

export default function Dashboard() {
  const { data: stats, refresh } = usePolling(() => statsApi.getStats(), { intervalMs: 5000 })
  const [provIface, setProvIface] = useState('usb0')
  const [provCount, setProvCount] = useState(5)
  const [statusMsg, setStatusMsg] = useState('')
  const [loading, setLoading] = useState<string | null>(null)
  const [teardownConfirm, setTeardownConfirm] = useState(false)

  const handleProvision = async () => {
    setLoading('provision')
    setStatusMsg('')
    try {
      const req: ProvisionRequest = { interface: provIface, slots: provCount }
      const res = await slotsApi.provision(req)
      setStatusMsg(`Provisioned: ${res.created} created, ${res.failed} failed, ${res.total} total`)
      refresh()
    } catch (err) {
      setStatusMsg(`Error: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setLoading(null)
    }
  }

  const handleTeardown = async () => {
    setTeardownConfirm(false)
    setLoading('teardown')
    setStatusMsg('')
    try {
      const res = await slotsApi.teardown()
      setStatusMsg(`Teardown complete: ${res.total} destroyed`)
      refresh()
    } catch (err) {
      setStatusMsg(`Error: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setLoading(null)
    }
  }

  const handleChangeIp = async (slotName: string) => {
    setLoading(slotName)
    try {
      await slotsApi.changeIp(slotName)
      refresh()
    } catch (err) {
      setStatusMsg(`IP change failed for ${slotName}: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setLoading(null)
    }
  }

  const handleDeleteSlot = async (slotName: string) => {
    setLoading(slotName)
    try {
      await slotsApi.delete(slotName)
      refresh()
    } catch (err) {
      setStatusMsg(`Delete failed for ${slotName}: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setLoading(null)
    }
  }

  const slots = stats?.slot_stats ?? []

  return (
    <div>
      <h2 className="text-2xl font-bold mb-6">Dashboard</h2>

      {/* Stat cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <StatCard label="Total Slots" value={stats?.total_slots ?? 0} />
        <StatCard label="Healthy" value={stats?.healthy_slots ?? 0} accent="success" />
        <StatCard label="Unhealthy" value={stats?.unhealthy_slots ?? 0} accent="danger" />
        <StatCard label="Active Connections" value={stats?.active_connections ?? 0} accent="default" />
      </div>

      {/* Quick actions */}
      <div className="bg-bg-card rounded-xl border border-border p-5 mb-8">
        <div className="flex flex-wrap items-end gap-4">
          <div>
            <label className="block text-xs text-text-secondary font-semibold uppercase tracking-wider mb-1">Interface</label>
            <input
              type="text"
              value={provIface}
              onChange={e => setProvIface(e.target.value)}
              className="px-3 py-2 bg-bg-input border border-border rounded-lg text-sm text-text-primary w-28"
            />
          </div>
          <div>
            <label className="block text-xs text-text-secondary font-semibold uppercase tracking-wider mb-1">Slots</label>
            <input
              type="number"
              value={provCount}
              onChange={e => setProvCount(Number(e.target.value))}
              min={1}
              max={500}
              className="px-3 py-2 bg-bg-input border border-border rounded-lg text-sm text-text-primary w-20"
            />
          </div>
          <button
            onClick={handleProvision}
            disabled={loading === 'provision'}
            className="px-4 py-2 bg-accent text-white rounded-lg hover:bg-accent-hover transition-colors font-medium text-sm disabled:opacity-50 cursor-pointer"
          >
            {loading === 'provision' ? 'Provisioning...' : 'Provision'}
          </button>
          <button
            onClick={() => setTeardownConfirm(true)}
            disabled={loading === 'teardown'}
            className="px-4 py-2 bg-danger text-white rounded-lg hover:bg-red-600 transition-colors font-medium text-sm disabled:opacity-50 cursor-pointer"
          >
            {loading === 'teardown' ? 'Tearing down...' : 'Teardown All'}
          </button>
        </div>
        {statusMsg && (
          <p className="mt-3 text-sm text-text-secondary">{statusMsg}</p>
        )}
      </div>

      {/* Slot cards grid */}
      <h3 className="text-lg font-semibold mb-4">Slots</h3>
      <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-4">
        {slots.map(slot => (
          <div
            key={slot.name}
            className={`bg-bg-card rounded-xl border-l-4 p-5 ${
              slot.status === 'healthy' ? 'border-success' :
              slot.status === 'discovering' ? 'border-warning' : 'border-danger'
            }`}
          >
            <div className="flex justify-between items-start mb-3">
              <h4 className="text-lg font-bold text-text-primary">{slot.name}</h4>
              <StatusBadge status={slot.status} />
            </div>
            <div className="space-y-1 text-sm text-text-secondary mb-4">
              <p>IPv6: <span className="font-mono text-xs text-text-primary">{slot.ipv6_address || '—'}</span></p>
              <p>IPv4: <span className="font-mono text-text-primary">{slot.public_ipv4 || '—'}</span></p>
              <p>Connections: <span className="text-text-primary">{slot.active_connections}</span></p>
              <p>Traffic: <span className="text-text-primary">{formatBytes(slot.bytes_sent)}↑ {formatBytes(slot.bytes_received)}↓</span></p>
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => handleChangeIp(slot.name)}
                disabled={loading === slot.name}
                className="px-3 py-1.5 bg-accent text-white rounded-lg text-xs font-medium hover:bg-accent-hover disabled:opacity-50 cursor-pointer"
              >
                {loading === slot.name ? '...' : 'Change IP'}
              </button>
              <button
                onClick={() => handleDeleteSlot(slot.name)}
                disabled={loading === slot.name}
                className="px-3 py-1.5 bg-danger text-white rounded-lg text-xs font-medium hover:bg-red-600 disabled:opacity-50 cursor-pointer"
              >
                Delete
              </button>
            </div>
          </div>
        ))}
        {slots.length === 0 && (
          <div className="col-span-full text-center py-12 bg-bg-card rounded-xl border border-border">
            <p className="text-text-muted">No slots provisioned</p>
          </div>
        )}
      </div>

      {/* Teardown confirmation */}
      <Modal open={teardownConfirm} onClose={() => setTeardownConfirm(false)} title="Confirm Teardown">
        <p className="text-text-secondary mb-6">This will destroy all slots. Are you sure?</p>
        <div className="flex justify-end gap-3">
          <button onClick={() => setTeardownConfirm(false)} className="px-4 py-2 rounded-lg border border-border text-text-secondary hover:bg-bg-hover cursor-pointer">
            Cancel
          </button>
          <button onClick={handleTeardown} className="px-4 py-2 bg-danger text-white rounded-lg hover:bg-red-600 cursor-pointer">
            Teardown All
          </button>
        </div>
      </Modal>
    </div>
  )
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}
