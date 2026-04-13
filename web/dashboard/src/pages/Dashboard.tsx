import { useState, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import type { Device, Slot, TrafficList, DNSCacheStats } from '../api/types'
import { setupDevice } from '../api/devices'
import { provisionDevice, deleteDevice, resetDevice } from '../api/devices'
import { changeSlotIP, deleteSlot } from '../api/slots'
import StatsBar from '../components/StatsBar'
import DeviceCard from '../components/DeviceCard'
import ProxyGenerator from '../components/ProxyGenerator'

interface Toast {
  id: number
  message: string
  type: 'success' | 'error'
}

let toastId = 0

interface DashboardContext {
  devices: Device[]
  slots: Slot[]
  traffic: TrafficList | null
  dnsStats: DNSCacheStats | null
  connected: boolean
  error: string | null
}

export default function Dashboard() {
  const { devices, slots, traffic, dnsStats, connected, error } = useOutletContext<DashboardContext>()
  const [toasts, setToasts] = useState<Toast[]>([])

  const host = window.location.hostname || 'localhost'
  const dnsHitRate = dnsStats?.total_hit_rate_percent



  const addToast = useCallback((message: string, type: 'success' | 'error') => {
    const id = ++toastId
    setToasts((prev) => [...prev, { id, message, type }])
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id))
    }, 3000)
  }, [])

  const handleSetupDevice = async (id: string) => {
    try {
      const result = await setupDevice(id)
      const name = result.device.alias || id
      const msg = result.provision
        ? `${name} online — ${result.provision.created} slot${result.provision.created !== 1 ? 's' : ''} provisioned`
        : `${name} online`
      addToast(msg, 'success')
    } catch (e) {
      addToast(`Setup failed: ${e instanceof Error ? e.message : 'Unknown error'}`, 'error')
    }
  }

  const handleProvision = async (id: string, count: number) => {
    try {
      const result = await provisionDevice(id, count)
      addToast(`Provisioned ${result.created} slot${result.created !== 1 ? 's' : ''} for ${id}`, 'success')
    } catch (e) {
      addToast(`Provision failed: ${e instanceof Error ? e.message : 'Unknown error'}`, 'error')
    }
  }

  const handleDeleteDevice = async (id: string) => {
    try {
      await deleteDevice(id)
      addToast(`Deleted — ready for re-setup`, 'success')
    } catch (e) {
      addToast(`Delete failed: ${e instanceof Error ? e.message : 'Unknown error'}`, 'error')
    }
  }

  const handleResetDevice = async (id: string) => {
    try {
      const result = await resetDevice(id)
      const name = result.device.alias || id
      addToast(`Reset ${name} — back online`, 'success')
    } catch (e) {
      addToast(`Reset failed: ${e instanceof Error ? e.message : 'Unknown error'}`, 'error')
    }
  }

  const handleChangeSlotIP = async (name: string) => {
    try {
      await changeSlotIP(name)
      addToast(`IP changed for ${name}`, 'success')
    } catch (e) {
      addToast(`Change IP failed: ${e instanceof Error ? e.message : 'Unknown error'}`, 'error')
    }
  }

  const handleDeleteSlot = async (name: string) => {
    try {
      await deleteSlot(name)
      addToast(`Deleted ${name}`, 'success')
    } catch (e) {
      addToast(`Delete slot failed: ${e instanceof Error ? e.message : 'Unknown error'}`, 'error')
    }
  }

  const loading = !connected && devices.length === 0 && slots.length === 0

  return (
    <div className="space-y-8">
      {/* Toast notifications */}
      <div className="fixed top-16 right-4 z-50 space-y-2">
        {toasts.map((toast) => (
          <div
            key={toast.id}
            className={`animate-toast-in px-4 py-2.5 rounded-lg shadow-lg font-mono text-xs
              border backdrop-blur-sm max-w-sm ${toast.type === 'success'
                ? 'bg-accent-green/15 border-accent-green/30 text-accent-green'
                : 'bg-accent-red/15 border-accent-red/30 text-accent-red'
              }`}
          >
            {toast.type === 'success' ? '✓' : '✗'} {toast.message}
          </div>
        ))}
      </div>

      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary font-mono tracking-wide">
            <span className="text-accent-cyan">▌</span> DASHBOARD
          </h1>
          <p className="text-xs text-text-muted mt-1 font-mono">
            {devices.length} device{devices.length !== 1 ? 's' : ''} · {slots.length} slot{slots.length !== 1 ? 's' : ''}
            <span
              className={`inline-block w-2 h-2 rounded-full ml-2 align-middle ${connected ? 'bg-accent-green' : 'bg-accent-red animate-pulse'
                }`}
              title={connected ? 'Connected' : 'Reconnecting...'}
            />
          </p>
        </div>
      </div>

      {/* Loading state */}
      {loading && (
        <div className="flex items-center justify-center py-20">
          <div className="text-center space-y-3">
            <span className="inline-block w-8 h-8 border-2 border-accent-cyan border-t-transparent rounded-full animate-spin-slow" />
            <p className="font-mono text-sm text-text-muted">Connecting to backend...</p>
          </div>
        </div>
      )}

      {/* Error state */}
      {!loading && error && (
        <div className="bg-accent-red/10 border border-accent-red/20 rounded-lg px-5 py-4">
          <p className="font-mono text-sm text-accent-red">{error}</p>
        </div>
      )}

      {/* Main content */}
      {!loading && (
        <>
          {/* Stats */}
          <StatsBar slots={slots} dnsHitRate={dnsHitRate} />

          {/* Device cards */}
          <div className="space-y-4">
            {devices.length === 0 && !error && (
              <div className="bg-bg-surface border border-border-subtle rounded-lg px-6 py-12 text-center">
                <p className="font-mono text-text-secondary mb-2">No devices found</p>
                <p className="text-sm text-text-muted">Connect a phone via USB — it will be detected automatically</p>
              </div>
            )}
            {devices.map((device, i) => (
              <DeviceCard
                key={device.alias || device.serial}
                device={device}
                slots={slots.filter((s) => device.alias && s.device_alias === device.alias)}
                onProvision={handleProvision}
                onDeleteDevice={handleDeleteDevice}
                onResetDevice={handleResetDevice}
                onSetupDevice={handleSetupDevice}
                onChangeSlotIP={handleChangeSlotIP}
                onDeleteSlot={handleDeleteSlot}
                host={host}
                animationDelay={200 + i * 50}
                trafficTotals={traffic?.device_totals?.[device.alias]}
              />
            ))}
          </div>

          {/* Proxy Generator */}
          <ProxyGenerator devices={devices} slots={slots} />
        </>
      )}
    </div>
  )
}
