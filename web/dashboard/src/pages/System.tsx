import { useState, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { useOutletContext } from 'react-router-dom'
import { restartADB, cleanupNamespaces, restartService } from '../api/system'
import type { SystemStats } from '../api/types'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}

function formatDuration(seconds: number): string {
  if (seconds <= 0) return '—'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h ${m}m`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

function cpuColor(pct: number): string {
  if (pct >= 90) return 'text-accent-red glow-red'
  if (pct >= 70) return 'text-accent-amber glow-amber'
  return 'text-accent-cyan glow-cyan'
}

function memColor(used: number, total: number): string {
  if (total === 0) return 'text-accent-cyan glow-cyan'
  const pct = (used / total) * 100
  if (pct >= 90) return 'text-accent-red glow-red'
  if (pct >= 70) return 'text-accent-amber glow-amber'
  return 'text-accent-cyan glow-cyan'
}

function tempColor(temp: number): string {
  if (temp >= 80) return 'text-accent-red glow-red'
  if (temp >= 65) return 'text-accent-amber glow-amber'
  return 'text-accent-green glow-green'
}

function loadColor(load: number): string {
  if (load >= 4) return 'text-accent-red glow-red'
  if (load >= 2) return 'text-accent-amber glow-amber'
  return 'text-accent-cyan glow-cyan'
}

function goroutineColor(count: number): string {
  if (count >= 1000) return 'text-accent-red glow-red'
  if (count >= 500) return 'text-accent-amber glow-amber'
  return 'text-accent-purple glow-purple'
}

function diskColor(used: number, total: number): string {
  if (total === 0) return 'text-accent-cyan glow-cyan'
  const pct = (used / total) * 100
  if (pct >= 95) return 'text-accent-red glow-red'
  if (pct >= 80) return 'text-accent-amber glow-amber'
  return 'text-accent-cyan glow-cyan'
}

interface MetricCardProps {
  label: string
  value: string
  sub?: string
  glowClass: string
  delay: number
}

function MetricCard({ label, value, sub, glowClass, delay }: MetricCardProps) {
  return (
    <div
      className="animate-fade-up bg-bg-surface border border-border-subtle rounded-lg p-5 card-glow"
      style={{ animationDelay: `${delay}ms` }}
    >
      <p className="text-xs text-text-muted uppercase tracking-wider font-medium mb-2">{label}</p>
      <p className={`font-mono text-3xl font-semibold ${glowClass}`}>{value}</p>
      {sub && <p className="text-xs text-text-muted font-mono mt-1">{sub}</p>}
    </div>
  )
}

type ModalAction = 'restart' | 'restart-adb' | 'cleanup' | null

const ACTIONS = {
  restart: {
    title: '⚠ Restart Moxy?',
    items: [
      { icon: '✕', text: 'Drop all active proxy connections' },
      { icon: '✕', text: 'Clear traffic stats, DNS cache, and logs' },
      { icon: '✕', text: 'Re-provision all slots (new IPs will be assigned)' },
    ],
    positive: '✓ Devices will be automatically re-detected after restart',
    confirmLabel: 'Restart Now',
    style: 'accent-red',
  },
  'restart-adb': {
    title: '⚠ Restart ADB Server?',
    items: [
      { icon: '✕', text: 'May temporarily interrupt device detection' },
      { icon: '✕', text: 'Active device watchers will reconnect' },
    ],
    positive: '✓ Existing proxy connections are unaffected',
    confirmLabel: 'Restart ADB',
    style: 'accent-amber',
  },
  cleanup: {
    title: 'Cleanup Orphaned Namespaces?',
    items: [
      { icon: '⚡', text: 'Removes leaked network namespaces from /var/run/netns' },
      { icon: '⚡', text: 'Only removes namespaces not linked to active slots' },
    ],
    positive: '✓ Active slots are never affected',
    confirmLabel: 'Cleanup Now',
    style: 'accent-cyan',
  },
}

export default function System() {
  const { systemStats } = useOutletContext<{ systemStats: SystemStats | null }>()
  const [modal, setModal] = useState<ModalAction>(null)
  const [loading, setLoading] = useState(false)
  const [toast, setToast] = useState<{ msg: string; type: 'success' | 'error' } | null>(null)

  const showToast = useCallback((msg: string, type: 'success' | 'error') => {
    setToast({ msg, type })
    setTimeout(() => setToast(null), 5000)
  }, [])

  const handleAction = useCallback(async () => {
    if (!modal) return
    setLoading(true)
    try {
      if (modal === 'restart') {
        await restartService()
        setModal(null)
        showToast('Restarting... Dashboard will reconnect automatically.', 'success')
        return // don't set loading=false — process is dying
      } else if (modal === 'restart-adb') {
        await restartADB()
        showToast('ADB server restarted successfully.', 'success')
      } else if (modal === 'cleanup') {
        const result = await cleanupNamespaces()
        showToast(`Cleaned up ${result.removed} orphaned namespace(s).`, 'success')
      }
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : String(err)
      showToast('Failed: ' + message, 'error')
    } finally {
      setLoading(false)
      setModal(null)
    }
  }, [modal, showToast])

  const s = systemStats

  if (!s) {
    return (
      <div className="animate-fade-up flex items-center justify-center h-64">
        <div className="text-text-muted font-mono text-sm flex items-center gap-3">
          <span className="inline-block w-4 h-4 border-2 border-accent-cyan/40 border-t-accent-cyan rounded-full animate-spin-slow" />
          Waiting for system stats...
        </div>
      </div>
    )
  }

  const memPct = s.mem_total_bytes > 0 ? ((s.mem_used_bytes / s.mem_total_bytes) * 100).toFixed(1) : '0'
  const diskPct = s.disk_total_bytes > 0 ? ((s.disk_used_bytes / s.disk_total_bytes) * 100).toFixed(1) : '0'

  return (
    <div className="animate-fade-up space-y-5 pb-8">
      {/* Header */}
      <div>
        <h1 className="text-xl font-semibold text-text-primary font-mono tracking-wide">
          <span className="text-accent-cyan">▌</span> SYSTEM MONITOR
        </h1>
        <p className="text-xs text-text-muted mt-1 font-mono">
          {s.hostname} · {s.arch} · {s.go_version}
        </p>
      </div>

      {/* Live Metrics Grid */}
      <div className="grid grid-cols-2 lg:grid-cols-3 gap-4">
        <MetricCard
          label="CPU"
          value={`${s.cpu_percent.toFixed(1)}%`}
          glowClass={cpuColor(s.cpu_percent)}
          delay={0}
        />
        <MetricCard
          label="Memory"
          value={`${formatBytes(s.mem_used_bytes)}`}
          sub={`${memPct}% of ${formatBytes(s.mem_total_bytes)}`}
          glowClass={memColor(s.mem_used_bytes, s.mem_total_bytes)}
          delay={40}
        />
        <MetricCard
          label="Temperature"
          value={s.temperature > 0 ? `${s.temperature.toFixed(1)}°C` : '—'}
          glowClass={tempColor(s.temperature)}
          delay={80}
        />
        <MetricCard
          label="Load (1m)"
          value={s.load_avg_1.toFixed(2)}
          sub={`5m: ${s.load_avg_5.toFixed(2)} · 15m: ${s.load_avg_15.toFixed(2)}`}
          glowClass={loadColor(s.load_avg_1)}
          delay={120}
        />
        <MetricCard
          label="Disk"
          value={formatBytes(s.disk_used_bytes)}
          sub={`${diskPct}% of ${formatBytes(s.disk_total_bytes)}`}
          glowClass={diskColor(s.disk_used_bytes, s.disk_total_bytes)}
          delay={160}
        />
        <MetricCard
          label="Goroutines"
          value={String(s.goroutines)}
          glowClass={goroutineColor(s.goroutines)}
          delay={200}
        />
      </div>

      {/* Uptime Bar */}
      <div className="grid grid-cols-2 gap-4">
        <div className="animate-fade-up bg-bg-surface border border-border-subtle rounded-lg p-5 card-glow" style={{ animationDelay: '240ms' }}>
          <p className="text-xs text-text-muted uppercase tracking-wider font-medium mb-2">Host Uptime</p>
          <p className="font-mono text-2xl font-semibold text-accent-green glow-green">
            {formatDuration(s.host_uptime_seconds)}
          </p>
        </div>
        <div className="animate-fade-up bg-bg-surface border border-border-subtle rounded-lg p-5 card-glow" style={{ animationDelay: '280ms' }}>
          <p className="text-xs text-text-muted uppercase tracking-wider font-medium mb-2">Process Uptime</p>
          <p className="font-mono text-2xl font-semibold text-accent-cyan glow-cyan">
            {formatDuration(s.process_uptime_seconds)}
          </p>
        </div>
      </div>

      {/* System Actions */}
      <div className="animate-fade-up bg-bg-surface border border-border-subtle rounded-lg card-glow overflow-hidden" style={{ animationDelay: '320ms' }}>
        <div className="px-5 py-3.5">
          <h2 className="text-sm font-semibold text-text-primary font-mono tracking-wider uppercase mb-4">
            System Actions
          </h2>
          <div className="space-y-3">
            {/* Restart ADB */}
            <div className="flex items-center justify-between">
              <div>
                <p className="text-xs text-text-secondary font-mono">Restart ADB Server</p>
                <p className="text-[10px] text-text-muted font-mono mt-0.5">Kill and restart the ADB daemon for device detection</p>
              </div>
              <button
                onClick={() => setModal('restart-adb')}
                className="px-4 py-2 rounded text-xs font-mono font-semibold bg-accent-amber/10 text-accent-amber border border-accent-amber/30 hover:bg-accent-amber/20 transition-colors whitespace-nowrap"
              >
                ⟳ Restart ADB
              </button>
            </div>

            {/* Cleanup Namespaces */}
            <div className="flex items-center justify-between border-t border-border-subtle/50 pt-3">
              <div>
                <p className="text-xs text-text-secondary font-mono">Cleanup Orphaned Namespaces</p>
                <p className="text-[10px] text-text-muted font-mono mt-0.5">Remove leaked network namespaces from /var/run/netns</p>
              </div>
              <button
                onClick={() => setModal('cleanup')}
                className="px-4 py-2 rounded text-xs font-mono font-semibold bg-accent-cyan/10 text-accent-cyan border border-accent-cyan/30 hover:bg-accent-cyan/20 transition-colors whitespace-nowrap"
              >
                ⚡ Cleanup
              </button>
            </div>

            {/* Restart Service */}
            <div className="flex items-center justify-between border-t border-border-subtle/50 pt-3">
              <div>
                <p className="text-xs text-text-secondary font-mono">Restart Moxy Service</p>
                <p className="text-[10px] text-text-muted font-mono mt-0.5">All active connections will be dropped and slots re-provisioned</p>
              </div>
              <button
                onClick={() => setModal('restart')}
                className="px-4 py-2 rounded text-xs font-mono font-semibold bg-accent-red/10 text-accent-red border border-accent-red/30 hover:bg-accent-red/20 transition-colors whitespace-nowrap"
              >
                ⟳ Restart Service
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Confirmation Modal */}
      {modal && createPortal(
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm animate-fade-in">
          <div className="bg-bg-surface border border-border-active rounded-xl shadow-2xl max-w-md w-full mx-4 p-6 space-y-4">
            <h3 className={`text-lg font-mono font-semibold text-${ACTIONS[modal].style} flex items-center gap-2`}>
              <span>{ACTIONS[modal].title}</span>
            </h3>

            <div className="space-y-2 text-sm text-text-secondary font-mono">
              <p className="text-text-muted text-xs uppercase tracking-wider">This will:</p>
              <ul className="space-y-1.5 text-xs">
                {ACTIONS[modal].items.map((item, i) => (
                  <li key={i} className="flex items-start gap-2">
                    <span className={`text-${ACTIONS[modal].style} mt-0.5`}>{item.icon}</span>
                    <span>{item.text}</span>
                  </li>
                ))}
              </ul>
            </div>

            <p className="text-xs text-accent-green font-mono flex items-center gap-2">
              <span>{ACTIONS[modal].positive}</span>
            </p>

            <div className="flex items-center justify-end gap-3 pt-2 border-t border-border-subtle">
              <button
                onClick={() => setModal(null)}
                className="px-4 py-2 rounded text-xs font-mono font-medium border border-border-subtle bg-bg-surface text-text-secondary hover:text-text-primary hover:bg-bg-surface-hover transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleAction}
                disabled={loading}
                className={`px-4 py-2 rounded text-xs font-mono font-semibold bg-${ACTIONS[modal].style}/20 text-${ACTIONS[modal].style} border border-${ACTIONS[modal].style}/40 hover:bg-${ACTIONS[modal].style}/30 transition-colors disabled:opacity-50`}
              >
                {loading ? (
                  <span className="flex items-center gap-2">
                    <span className={`inline-block w-3 h-3 border-2 border-${ACTIONS[modal].style}/40 border-t-${ACTIONS[modal].style} rounded-full animate-spin-slow`} />
                    Processing...
                  </span>
                ) : (
                  ACTIONS[modal].confirmLabel
                )}
              </button>
            </div>
          </div>
        </div>,
        document.body,
      )}

      {/* Toast */}
      {toast && createPortal(
        <div className={`fixed bottom-6 right-6 z-50 animate-toast-in px-4 py-3 rounded-lg border backdrop-blur-sm shadow-lg max-w-sm font-mono text-xs ${toast.type === 'success'
          ? 'bg-accent-green/10 border-accent-green/30 text-accent-green'
          : 'bg-accent-red/10 border-accent-red/30 text-accent-red'
          }`}>
          {toast.msg}
        </div>,
        document.body,
      )}
    </div>
  )
}
