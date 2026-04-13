import { useState, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { useOutletContext } from 'react-router-dom'
import { restartADB, restartService } from '../api/system'
import type { SystemStats } from '../api/types'

/* ── Helpers ──────────────────────────────────────────────────────────────── */

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

function statusColor(pct: number): { ring: string; text: string; glow: string } {
  if (pct >= 90) return { ring: '#f87171', text: 'text-accent-red', glow: 'rgba(248,113,113,0.3)' }
  if (pct >= 70) return { ring: '#fbbf24', text: 'text-accent-amber', glow: 'rgba(251,191,36,0.3)' }
  return { ring: '#38bdf8', text: 'text-accent-cyan', glow: 'rgba(56,189,248,0.2)' }
}

function tempStatusColor(temp: number): { ring: string; text: string; glow: string } {
  if (temp >= 80) return { ring: '#f87171', text: 'text-accent-red', glow: 'rgba(248,113,113,0.3)' }
  if (temp >= 65) return { ring: '#fbbf24', text: 'text-accent-amber', glow: 'rgba(251,191,36,0.3)' }
  return { ring: '#4ade80', text: 'text-accent-green', glow: 'rgba(74,222,128,0.2)' }
}

/* ── Ring Gauge ────────────────────────────────────────────────────────────── */

interface RingGaugeProps {
  value: number      // 0-100
  label: string
  display: string
  sub?: string
  color: string      // hex
  glow: string       // rgba
  delay: number
}

function RingGauge({ value, label, display, sub, color, glow, delay }: RingGaugeProps) {
  const radius = 42
  const stroke = 5
  const circumference = 2 * Math.PI * radius
  const offset = circumference - (Math.min(value, 100) / 100) * circumference

  return (
    <div
      className="animate-fade-up bg-bg-surface border border-border-subtle rounded-xl p-5 flex flex-col items-center gap-3 card-glow relative overflow-hidden group"
      style={{ animationDelay: `${delay}ms` }}
    >
      {/* Atmospheric gradient */}
      <div
        className="absolute inset-0 opacity-0 group-hover:opacity-100 transition-opacity duration-500"
        style={{ background: `radial-gradient(circle at 50% 30%, ${glow}, transparent 70%)` }}
      />

      {/* SVG ring */}
      <div className="relative w-24 h-24">
        <svg viewBox="0 0 100 100" className="w-full h-full -rotate-90">
          {/* Background track */}
          <circle
            cx="50" cy="50" r={radius}
            fill="none"
            stroke="rgba(56,189,248,0.06)"
            strokeWidth={stroke}
          />
          {/* Value arc */}
          <circle
            cx="50" cy="50" r={radius}
            fill="none"
            stroke={color}
            strokeWidth={stroke}
            strokeLinecap="round"
            strokeDasharray={circumference}
            strokeDashoffset={offset}
            style={{ transition: 'stroke-dashoffset 0.8s ease, stroke 0.5s ease' }}
          />
        </svg>
        {/* Center value */}
        <div className="absolute inset-0 flex items-center justify-center">
          <span className="font-mono text-lg font-bold text-text-primary">{display}</span>
        </div>
      </div>

      <div className="text-center relative z-10">
        <p className="text-[10px] text-text-muted uppercase tracking-[0.15em] font-medium">{label}</p>
        {sub && <p className="text-[10px] text-text-muted font-mono mt-0.5">{sub}</p>}
      </div>
    </div>
  )
}

/* ── Stat Chip ─────────────────────────────────────────────────────────────── */

interface StatChipProps {
  label: string
  value: string
  sub?: string
  colorClass: string
  delay: number
}

function StatChip({ label, value, sub, colorClass, delay }: StatChipProps) {
  return (
    <div
      className="animate-fade-up bg-bg-surface border border-border-subtle rounded-xl p-4 card-glow"
      style={{ animationDelay: `${delay}ms` }}
    >
      <p className="text-[10px] text-text-muted uppercase tracking-[0.15em] font-medium mb-1.5">{label}</p>
      <p className={`font-mono text-2xl font-bold ${colorClass}`}>{value}</p>
      {sub && <p className="text-[10px] text-text-muted font-mono mt-1">{sub}</p>}
    </div>
  )
}

/* ── Actions & Modal ──────────────────────────────────────────────────────── */

type ModalAction = 'restart' | 'restart-adb' | null

const ACTIONS = {
  restart: {
    title: 'Restart Moxy?',
    icon: '⟳',
    items: [
      'Drop all active proxy connections',
      'Clear traffic stats, DNS cache, and logs',
      'Re-provision all slots (new IPs assigned)',
    ],
    positive: 'Devices will auto-detect after restart',
    confirmLabel: 'Restart Now',
    color: 'accent-red',
  },
  'restart-adb': {
    title: 'Restart ADB Server?',
    icon: '⟳',
    items: [
      'May temporarily interrupt device detection',
      'Active device watchers will reconnect',
    ],
    positive: 'Existing proxy connections are unaffected',
    confirmLabel: 'Restart ADB',
    color: 'accent-amber',
  },
}

interface ActionRowProps {
  label: string
  desc: string
  btnLabel: string
  btnColor: string
  onClick: () => void
  delay: number
}

function ActionRow({ label, desc, btnLabel, btnColor, onClick, delay }: ActionRowProps) {
  return (
    <div
      className="animate-fade-up flex items-center justify-between py-3 first:pt-0 border-b border-border-subtle/40 last:border-0"
      style={{ animationDelay: `${delay}ms` }}
    >
      <div className="min-w-0">
        <p className="text-xs text-text-secondary font-mono truncate">{label}</p>
        <p className="text-[10px] text-text-muted font-mono mt-0.5">{desc}</p>
      </div>
      <button
        onClick={onClick}
        className={`ml-4 px-4 py-2 rounded-lg text-[11px] font-mono font-semibold border transition-all duration-200 whitespace-nowrap
          bg-${btnColor}/5 text-${btnColor} border-${btnColor}/20
          hover:bg-${btnColor}/15 hover:border-${btnColor}/40 hover:shadow-[0_0_12px_rgba(0,0,0,0.3)]
          active:scale-[0.97]`}
      >
        {btnLabel}
      </button>
    </div>
  )
}

/* ── Main Component ───────────────────────────────────────────────────────── */

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
        return
      } else if (modal === 'restart-adb') {
        await restartADB()
        showToast('ADB server restarted successfully.', 'success')
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

  const cpuPct = s.cpu_percent
  const memPct = s.mem_total_bytes > 0 ? (s.mem_used_bytes / s.mem_total_bytes) * 100 : 0
  const diskPct = s.disk_total_bytes > 0 ? (s.disk_used_bytes / s.disk_total_bytes) * 100 : 0
  const tempPct = Math.min(s.temperature, 100) // clamped for gauge

  const cpuC = statusColor(cpuPct)
  const memC = statusColor(memPct)
  const diskC = statusColor(diskPct)
  const tempC = tempStatusColor(s.temperature)

  return (
    <div className="space-y-6 pb-8">
      {/* Header */}
      <div className="animate-fade-up">
        <div className="flex items-center gap-2">
          <h1 className="text-xl font-semibold text-text-primary font-mono tracking-wide">
            <span className="text-accent-cyan">▌</span> SYSTEM
          </h1>
          {/* Live pulse */}
          <span className="relative flex h-2 w-2">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-accent-green opacity-75" />
            <span className="relative inline-flex rounded-full h-2 w-2 bg-accent-green" />
          </span>
        </div>
        <p className="text-xs text-text-muted mt-1 font-mono">
          {s.hostname} · {s.arch} · {s.go_version}
        </p>
      </div>

      {/* Gauges Row */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <RingGauge
          label="CPU"
          value={cpuPct}
          display={`${cpuPct.toFixed(0)}%`}
          color={cpuC.ring}
          glow={cpuC.glow}
          delay={0}
        />
        <RingGauge
          label="Memory"
          value={memPct}
          display={`${memPct.toFixed(0)}%`}
          sub={`${formatBytes(s.mem_used_bytes)} / ${formatBytes(s.mem_total_bytes)}`}
          color={memC.ring}
          glow={memC.glow}
          delay={50}
        />
        <RingGauge
          label="Disk"
          value={diskPct}
          display={`${diskPct.toFixed(0)}%`}
          sub={`${formatBytes(s.disk_used_bytes)} / ${formatBytes(s.disk_total_bytes)}`}
          color={diskC.ring}
          glow={diskC.glow}
          delay={100}
        />
        <RingGauge
          label="Temperature"
          value={tempPct}
          display={s.temperature > 0 ? `${s.temperature.toFixed(0)}°` : '—'}
          color={tempC.ring}
          glow={tempC.glow}
          delay={150}
        />
      </div>

      {/* Stats Row */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <StatChip
          label="Load Average"
          value={s.load_avg_1.toFixed(2)}
          sub={`5m: ${s.load_avg_5.toFixed(2)} · 15m: ${s.load_avg_15.toFixed(2)}`}
          colorClass={s.load_avg_1 >= 4 ? 'text-accent-red' : s.load_avg_1 >= 2 ? 'text-accent-amber' : 'text-accent-cyan'}
          delay={200}
        />
        <StatChip
          label="Goroutines"
          value={String(s.goroutines)}
          colorClass={s.goroutines >= 1000 ? 'text-accent-red' : s.goroutines >= 500 ? 'text-accent-amber' : 'text-accent-purple'}
          delay={240}
        />
        <StatChip
          label="Host Uptime"
          value={formatDuration(s.host_uptime_seconds)}
          colorClass="text-accent-green"
          delay={280}
        />
        <StatChip
          label="Process Uptime"
          value={formatDuration(s.process_uptime_seconds)}
          colorClass="text-accent-cyan"
          delay={320}
        />
      </div>

      {/* System Actions */}
      <div
        className="animate-fade-up bg-bg-surface border border-border-subtle rounded-xl card-glow"
        style={{ animationDelay: '360ms' }}
      >
        <div className="px-5 py-4">
          <h2 className="text-[10px] text-text-muted uppercase tracking-[0.15em] font-semibold mb-3">
            Actions
          </h2>
          <ActionRow
            label="Restart ADB Server"
            desc="Kill and restart the ADB daemon for device detection"
            btnLabel="⟳ Restart ADB"
            btnColor="accent-amber"
            onClick={() => setModal('restart-adb')}
            delay={400}
          />

          <ActionRow
            label="Restart Moxy Service"
            desc="All active connections will be dropped and slots re-provisioned"
            btnLabel="⟳ Restart"
            btnColor="accent-red"
            onClick={() => setModal('restart')}
            delay={440}
          />
        </div>
      </div>

      {/* ── Confirmation Modal ─────────────────────────────────────────────── */}
      {modal && createPortal(
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm animate-fade-in">
          <div className="bg-bg-surface border border-border-active rounded-xl shadow-2xl max-w-md w-full mx-4 p-6 space-y-4">
            <h3 className={`text-base font-mono font-semibold text-${ACTIONS[modal].color} flex items-center gap-2`}>
              <span className="text-lg">{ACTIONS[modal].icon}</span>
              <span>{ACTIONS[modal].title}</span>
            </h3>

            <ul className="space-y-2 text-xs text-text-secondary font-mono">
              {ACTIONS[modal].items.map((item, i) => (
                <li key={i} className="flex items-start gap-2">
                  <span className={`text-${ACTIONS[modal].color} mt-0.5`}>›</span>
                  <span>{item}</span>
                </li>
              ))}
            </ul>

            <p className="text-xs text-accent-green font-mono flex items-center gap-2">
              <span>✓</span>
              <span>{ACTIONS[modal].positive}</span>
            </p>

            <div className="flex items-center justify-end gap-3 pt-3 border-t border-border-subtle">
              <button
                onClick={() => setModal(null)}
                className="px-4 py-2 rounded-lg text-xs font-mono font-medium border border-border-subtle bg-bg-surface text-text-secondary hover:text-text-primary hover:bg-bg-surface-hover transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleAction}
                disabled={loading}
                className={`px-4 py-2 rounded-lg text-xs font-mono font-semibold transition-all duration-200 disabled:opacity-50
                  bg-${ACTIONS[modal].color}/15 text-${ACTIONS[modal].color} border border-${ACTIONS[modal].color}/30
                  hover:bg-${ACTIONS[modal].color}/25 hover:border-${ACTIONS[modal].color}/50 active:scale-[0.97]`}
              >
                {loading ? (
                  <span className="flex items-center gap-2">
                    <span className={`inline-block w-3 h-3 border-2 border-${ACTIONS[modal].color}/40 border-t-${ACTIONS[modal].color} rounded-full animate-spin-slow`} />
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

      {/* ── Toast ──────────────────────────────────────────────────────────── */}
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
