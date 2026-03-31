import { useState } from 'react'
import StatCard from '../components/StatCard'
import StatusBadge from '../components/StatusBadge'
import Modal from '../components/Modal'
import { usePolling } from '../hooks/usePolling'
import { devicesApi } from '../api/devices'
import { statsApi } from '../api/stats'
import type { Device, SetupProgress } from '../api/types'

const SETUP_STEPS = [
  'screen_unlocked', 'enabled_tethering', 'interface_detected',
  'enabled_data', 'dismissed_dialog', 'disabled_wifi',
  'dhcp_configured', 'ipv6_configured', 'isp_detected',
]

export default function Devices() {
  const { data: devices, refresh } = usePolling(() => devicesApi.list(), { intervalMs: 5000 })
  const { data: stats } = usePolling(() => statsApi.getStats(), { intervalMs: 5000 })
  const [adbSerials, setAdbSerials] = useState<string[]>([])
  const [regSerial, setRegSerial] = useState('')
  const [regAlias, setRegAlias] = useState('')
  const [loading, setLoading] = useState<string | null>(null)
  const [statusMsg, setStatusMsg] = useState('')
  const [setupProgress, setSetupProgress] = useState<Record<string, SetupProgress>>({})
  const [provisionCounts, setProvisionCounts] = useState<Record<string, number>>({})
  const [showOverride, setShowOverride] = useState<string | null>(null)
  const [overrideNS, setOverrideNS] = useState('')
  const [overrideNAT64, setOverrideNAT64] = useState('')
  const [teardownConfirm, setTeardownConfirm] = useState<string | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)

  const handleScanADB = async () => {
    setLoading('scan')
    try {
      const serials = await devicesApi.scanADB() ?? []
      setAdbSerials(serials)
      if (serials.length > 0) setRegSerial(serials[0])
    } catch (err) {
      setStatusMsg(`Scan failed: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setLoading(null)
    }
  }

  const handleRegister = async () => {
    if (!regSerial || !regAlias) return
    setLoading('register')
    try {
      await devicesApi.register({ serial: regSerial, alias: regAlias })
      setRegSerial('')
      setRegAlias('')
      setAdbSerials([])
      refresh()
    } catch (err) {
      setStatusMsg(`Register failed: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setLoading(null)
    }
  }

  const handleSetup = async (device: Device) => {
    setLoading(device.id)
    try {
      const progress = await devicesApi.setup(device.id)
      setSetupProgress(prev => ({ ...prev, [device.id]: progress }))
      // Auto-provision 1 slot after successful setup
      if (progress.status === 'completed') {
        await devicesApi.provision(device.id, 1)
      }
      refresh()
    } catch (err) {
      setStatusMsg(`Setup failed: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setLoading(null)
    }
  }

  const handleProvision = async (deviceId: string) => {
    const count = provisionCounts[deviceId] || 5
    setLoading(deviceId)
    try {
      const res = await devicesApi.provision(deviceId, count)
      setStatusMsg(`Provisioned: ${res.created} created, ${res.failed} failed`)
      refresh()
    } catch (err) {
      setStatusMsg(`Provision failed: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setLoading(null)
    }
  }

  const handleTeardown = async (deviceId: string) => {
    setTeardownConfirm(null)
    setLoading(deviceId)
    try {
      await devicesApi.teardown(deviceId)
      refresh()
    } catch (err) {
      setStatusMsg(`Teardown failed: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setLoading(null)
    }
  }

  const handleDelete = async (deviceId: string) => {
    setDeleteConfirm(null)
    setLoading(deviceId)
    try {
      await devicesApi.delete(deviceId)
      refresh()
    } catch (err) {
      setStatusMsg(`Delete failed: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setLoading(null)
    }
  }

  const handleOverride = async (deviceId: string) => {
    setLoading(deviceId)
    try {
      await devicesApi.override(deviceId, { nameserver: overrideNS, nat64_prefix: overrideNAT64 })
      setShowOverride(null)
      refresh()
    } catch (err) {
      setStatusMsg(`Override failed: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setLoading(null)
    }
  }

  const statusBorderColor = (status: string) => {
    switch (status) {
      case 'online': return 'border-success'
      case 'setup': return 'border-warning'
      case 'error': return 'border-danger'
      default: return 'border-border'
    }
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h2 className="text-2xl font-bold">Devices</h2>
        <button
          onClick={handleScanADB}
          disabled={loading === 'scan'}
          className="px-4 py-2 bg-accent text-white rounded-lg hover:bg-accent-hover transition-colors font-medium text-sm disabled:opacity-50 cursor-pointer"
        >
          {loading === 'scan' ? 'Scanning...' : '🔍 Scan ADB'}
        </button>
      </div>

      {/* Summary bar */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-8">
        <StatCard label="Devices" value={devices?.length ?? 0} />
        <StatCard label="Total Slots" value={stats?.total_slots ?? 0} />
        <StatCard label="Active Connections" value={stats?.active_connections ?? 0} />
      </div>

      {statusMsg && (
        <div className="bg-bg-card border border-border rounded-lg p-3 mb-4 text-sm text-text-secondary">
          {statusMsg}
          <button onClick={() => setStatusMsg('')} className="ml-2 text-text-muted hover:text-text-primary">✕</button>
        </div>
      )}

      {/* Device cards */}
      <div className="space-y-4 mb-8">
        {(devices ?? []).map(device => (
          <div
            key={device.id}
            className={`bg-bg-card rounded-xl border-l-4 p-5 ${statusBorderColor(device.status)}`}
          >
            {/* Header */}
            <div className="flex justify-between items-start mb-3">
              <div>
                <h3 className="text-lg font-bold text-text-primary">{device.alias}</h3>
                <p className="text-xs text-text-muted font-mono">{device.serial}</p>
              </div>
              <div className="flex items-center gap-3">
                {device.carrier && (
                  <span className="text-xs text-text-secondary bg-bg-hover px-2 py-1 rounded">{device.carrier}</span>
                )}
                {device.interface && (
                  <span className="text-xs text-text-muted font-mono">{device.interface}</span>
                )}
                <StatusBadge status={device.status} />
              </div>
            </div>

            {/* Stats (online only) */}
            {device.status === 'online' && (
              <div className="text-sm text-text-secondary mb-4">
                {device.slot_count} slots
              </div>
            )}

            {/* Setup progress (setup/error status) */}
            {(device.status === 'setup' || setupProgress[device.id]) && (
              <div className="mb-4 bg-bg-input rounded-lg p-3">
                <p className="text-xs font-semibold text-text-secondary uppercase tracking-wider mb-2">Setup Progress</p>
                <div className="space-y-1">
                  {SETUP_STEPS.map(step => {
                    const progress = setupProgress[device.id]
                    const completed = progress?.completed_steps?.includes(step)
                    const failed = progress?.failed_at === step
                    const icon = completed ? '✅' : failed ? '❌' : '◻'
                    return (
                      <div key={step} className="flex items-center gap-2 text-xs">
                        <span>{icon}</span>
                        <span className={failed ? 'text-danger' : completed ? 'text-success' : 'text-text-muted'}>
                          {step.replace(/_/g, ' ')}
                        </span>
                        {failed && progress?.error && (
                          <span className="text-danger text-xs ml-2">— {progress.error}</span>
                        )}
                      </div>
                    )
                  })}
                </div>
              </div>
            )}

            {/* Actions */}
            <div className="flex flex-wrap items-center gap-2">
              {(device.status === 'offline' || device.status === 'error') && (
                <>
                  <button
                    onClick={() => handleSetup(device)}
                    disabled={loading === device.id}
                    className="px-3 py-1.5 bg-accent text-white rounded-lg text-xs font-medium hover:bg-accent-hover disabled:opacity-50 cursor-pointer"
                  >
                    {loading === device.id ? 'Setting up...' : device.status === 'error' ? 'Retry Setup' : 'Setup'}
                  </button>
                  <button
                    onClick={() => setDeleteConfirm(device.id)}
                    className="px-3 py-1.5 bg-danger-muted text-danger rounded-lg text-xs font-medium hover:bg-danger hover:text-white cursor-pointer"
                  >
                    Delete
                  </button>
                </>
              )}
              {device.status === 'online' && (
                <>
                  <div className="flex items-center gap-1">
                    <input
                      type="number"
                      min={1}
                      max={500}
                      value={provisionCounts[device.id] || 5}
                      onChange={e => setProvisionCounts(prev => ({ ...prev, [device.id]: Number(e.target.value) }))}
                      className="w-16 px-2 py-1.5 bg-bg-input border border-border rounded-lg text-xs text-text-primary"
                    />
                    <button
                      onClick={() => handleProvision(device.id)}
                      disabled={loading === device.id}
                      className="px-3 py-1.5 bg-accent text-white rounded-lg text-xs font-medium hover:bg-accent-hover disabled:opacity-50 cursor-pointer"
                    >
                      Provision
                    </button>
                  </div>
                  <button
                    onClick={() => setTeardownConfirm(device.id)}
                    className="px-3 py-1.5 bg-danger-muted text-danger rounded-lg text-xs font-medium hover:bg-danger hover:text-white cursor-pointer"
                  >
                    Teardown
                  </button>
                  <button
                    onClick={() => { setShowOverride(device.id); setOverrideNS(device.interface); setOverrideNAT64(''); }}
                    className="px-3 py-1.5 bg-bg-hover text-text-secondary rounded-lg text-xs font-medium hover:bg-border cursor-pointer"
                  >
                    ISP Override
                  </button>
                </>
              )}
            </div>

            {/* ISP Override inline form */}
            {showOverride === device.id && (
              <div className="mt-3 bg-bg-input rounded-lg p-3 flex flex-wrap items-end gap-3">
                <div>
                  <label className="block text-xs text-text-muted mb-1">Nameserver</label>
                  <input type="text" value={overrideNS} onChange={e => setOverrideNS(e.target.value)} className="px-2 py-1.5 bg-bg-card border border-border rounded text-xs w-48" />
                </div>
                <div>
                  <label className="block text-xs text-text-muted mb-1">NAT64 Prefix</label>
                  <input type="text" value={overrideNAT64} onChange={e => setOverrideNAT64(e.target.value)} className="px-2 py-1.5 bg-bg-card border border-border rounded text-xs w-48" />
                </div>
                <button onClick={() => handleOverride(device.id)} className="px-3 py-1.5 bg-accent text-white rounded text-xs">Save</button>
                <button onClick={() => setShowOverride(null)} className="px-3 py-1.5 text-text-muted text-xs">Cancel</button>
              </div>
            )}
          </div>
        ))}

        {(devices ?? []).length === 0 && (
          <div className="text-center py-12 bg-bg-card rounded-xl border border-border">
            <p className="text-text-muted mb-2">No devices registered</p>
            <p className="text-xs text-text-muted">Click "Scan ADB" to discover connected phones</p>
          </div>
        )}
      </div>

      {/* Register new device */}
      {adbSerials.length > 0 && (
        <div className="bg-bg-card rounded-xl border border-border p-5 mb-8">
          <h3 className="text-sm font-semibold text-text-primary mb-3">Register New Device</h3>
          <div className="flex flex-wrap items-end gap-3">
            <div>
              <label className="block text-xs text-text-muted mb-1">Serial</label>
              <select
                value={regSerial}
                onChange={e => setRegSerial(e.target.value)}
                className="px-3 py-2 bg-bg-input border border-border rounded-lg text-sm"
              >
                {adbSerials.map(s => <option key={s} value={s}>{s}</option>)}
              </select>
            </div>
            <div>
              <label className="block text-xs text-text-muted mb-1">Alias</label>
              <input
                type="text"
                value={regAlias}
                onChange={e => setRegAlias(e.target.value)}
                placeholder="dev1"
                className="px-3 py-2 bg-bg-input border border-border rounded-lg text-sm w-32"
              />
            </div>
            <button
              onClick={handleRegister}
              disabled={loading === 'register' || !regSerial || !regAlias}
              className="px-4 py-2 bg-accent text-white rounded-lg text-sm font-medium hover:bg-accent-hover disabled:opacity-50 cursor-pointer"
            >
              {loading === 'register' ? 'Registering...' : 'Register'}
            </button>
          </div>
        </div>
      )}

      {/* Teardown confirmation */}
      <Modal open={!!teardownConfirm} onClose={() => setTeardownConfirm(null)} title="Confirm Teardown">
        <p className="text-text-secondary mb-6">This will destroy all slots for this device. Are you sure?</p>
        <div className="flex justify-end gap-3">
          <button onClick={() => setTeardownConfirm(null)} className="px-4 py-2 rounded-lg border border-border text-text-secondary hover:bg-bg-hover cursor-pointer">Cancel</button>
          <button onClick={() => teardownConfirm && handleTeardown(teardownConfirm)} className="px-4 py-2 bg-danger text-white rounded-lg hover:bg-red-600 cursor-pointer">Teardown</button>
        </div>
      </Modal>

      {/* Delete confirmation */}
      <Modal open={!!deleteConfirm} onClose={() => setDeleteConfirm(null)} title="Confirm Delete">
        <p className="text-text-secondary mb-6">This will remove the device and all its slots permanently. Are you sure?</p>
        <div className="flex justify-end gap-3">
          <button onClick={() => setDeleteConfirm(null)} className="px-4 py-2 rounded-lg border border-border text-text-secondary hover:bg-bg-hover cursor-pointer">Cancel</button>
          <button onClick={() => deleteConfirm && handleDelete(deleteConfirm)} className="px-4 py-2 bg-danger text-white rounded-lg hover:bg-red-600 cursor-pointer">Delete</button>
        </div>
      </Modal>
    </div>
  )
}
