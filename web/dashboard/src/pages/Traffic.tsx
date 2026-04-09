import { useState, useMemo } from 'react'
import { useOutletContext } from 'react-router-dom'
import type { TrafficList, TrafficEntry } from '../api/types'

interface TrafficContext {
  traffic: TrafficList | null
}

type SortField = 'domain' | 'device_alias' | 'connection_count' | 'active_connections' | 'data' | 'last_seen_at'
type SortDir = 'asc' | 'desc'

// ─── Formatting helpers ──────────────────────────────────────────

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  const val = bytes / Math.pow(1024, i)
  return `${val < 10 ? val.toFixed(2) : val < 100 ? val.toFixed(1) : Math.round(val)} ${units[i]}`
}

function formatRelative(ms: number): string {
  if (ms === 0) return 'never'
  const seconds = Math.floor((Date.now() - ms) / 1000)
  if (seconds < 5) return 'just now'
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

function formatDomain(domain: string, port: string): string {
  const display = domain.length > 30 ? domain.substring(0, 27) + '…' : domain
  if (port && port !== '443' && port !== '80') {
    return `${display}:${port}`
  }
  return display
}

// ─── Stat card (matches StatsBar.tsx pattern) ────────────────────

function StatCard({ label, value, glowClass, delay }: {
  label: string
  value: string
  glowClass: string
  delay: number
}) {
  return (
    <div
      className="animate-fade-up bg-bg-surface border border-border-subtle rounded-lg p-5 card-glow"
      style={{ animationDelay: `${delay}ms` }}
    >
      <p className="text-xs text-text-muted uppercase tracking-wider font-medium mb-2">{label}</p>
      <p className={`font-mono text-3xl font-semibold ${glowClass}`}>{value}</p>
    </div>
  )
}

// ─── Sortable column header ──────────────────────────────────────

function SortHeader({ field, current, dir, onSort, align, children }: {
  field: SortField
  current: SortField
  dir: SortDir
  onSort: (f: SortField) => void
  align?: 'right'
  children: React.ReactNode
}) {
  const isActive = current === field
  return (
    <th
      onClick={() => onSort(field)}
      className={`px-3 py-2.5 text-xs uppercase tracking-wider font-medium cursor-pointer select-none group transition-colors whitespace-nowrap
        ${align === 'right' ? 'text-right' : 'text-left'}
        ${isActive ? 'text-accent-cyan' : 'text-text-muted hover:text-text-secondary'}`}
    >
      {children}
      {isActive
        ? <span className="ml-1">{dir === 'asc' ? '▲' : '▼'}</span>
        : <span className="ml-1 opacity-0 group-hover:opacity-40 transition-opacity">▼</span>
      }
    </th>
  )
}

// ─── Main component ──────────────────────────────────────────────

export default function Traffic() {
  const { traffic } = useOutletContext<TrafficContext>()

  // Sort state
  const [sortField, setSortField] = useState<SortField>('connection_count')
  const [sortDir, setSortDir] = useState<SortDir>('desc')

  // Filter state
  const [protocolFilter, setProtocolFilter] = useState<Set<string>>(new Set(['ipv4', 'ipv6']))
  const [transportFilter, setTransportFilter] = useState<Set<string>>(new Set(['tcp', 'udp']))
  const [deviceFilter, setDeviceFilter] = useState('')
  const [search, setSearch] = useState('')

  const entries = traffic?.entries ?? []

  // Extract unique devices for dropdown
  const deviceOptions = useMemo(() => {
    const set = new Set<string>()
    entries.forEach((e) => { if (e.device_alias) set.add(e.device_alias) })
    return Array.from(set).sort()
  }, [entries])

  // Filter entries
  const filtered = useMemo(() => {
    return entries.filter((e) => {
      if (!protocolFilter.has(e.protocol)) return false
      if (!transportFilter.has(e.transport || 'tcp')) return false
      if (deviceFilter && e.device_alias !== deviceFilter) return false
      if (search) {
        const q = search.toLowerCase()
        if (!e.domain.toLowerCase().includes(q)) return false
      }
      return true
    })
  }, [entries, protocolFilter, transportFilter, deviceFilter, search])

  // Sort entries
  const sorted = useMemo(() => {
    const arr = [...filtered]
    arr.sort((a, b) => {
      let cmp = 0
      switch (sortField) {
        case 'domain':
          cmp = a.domain.localeCompare(b.domain); break
        case 'device_alias':
          cmp = a.device_alias.localeCompare(b.device_alias); break
        case 'connection_count':
          cmp = a.connection_count - b.connection_count; break
        case 'active_connections':
          cmp = a.active_connections - b.active_connections; break
        case 'data':
          cmp = (a.tx_bytes + a.rx_bytes) - (b.tx_bytes + b.rx_bytes); break
        case 'last_seen_at':
          cmp = a.last_seen_at - b.last_seen_at; break
      }
      return sortDir === 'asc' ? cmp : -cmp
    })
    return arr
  }, [filtered, sortField, sortDir])

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortField(field)
      setSortDir('desc')
    }
  }

  const toggleProtocol = (proto: string) => {
    setProtocolFilter((prev) => {
      const next = new Set(prev)
      if (next.has(proto)) next.delete(proto)
      else next.add(proto)
      return next
    })
  }

  const toggleTransport = (t: string) => {
    setTransportFilter((prev) => {
      const next = new Set(prev)
      if (next.has(t)) next.delete(t)
      else next.add(t)
      return next
    })
  }

  return (
    <div className="animate-fade-up space-y-4">
      {/* ── Header ── */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary font-mono tracking-wide">
            <span className="text-accent-cyan">▸</span> TRAFFIC
          </h1>
          <p className="text-xs text-text-muted mt-1 font-mono">
            {traffic
              ? <>{traffic.total_entries.toLocaleString()} destination{traffic.total_entries !== 1 ? 's' : ''}
                  {entries.length < traffic.total_entries && (
                    <span className="text-text-secondary ml-1">(showing top {entries.length})</span>
                  )}
                </>
              : 'Connecting...'
            }
          </p>
        </div>
      </div>

      {/* ── Stats Bar ── */}
      {traffic && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          <StatCard
            label="Total Connections"
            value={traffic.total_connections.toLocaleString()}
            glowClass="text-accent-cyan glow-cyan"
            delay={0}
          />
          <StatCard
            label="Active"
            value={traffic.total_active.toLocaleString()}
            glowClass={traffic.total_active > 0 ? 'text-accent-green glow-green' : 'text-text-muted'}
            delay={50}
          />
          <StatCard
            label="TX Total"
            value={formatBytes(traffic.total_tx_bytes)}
            glowClass="text-accent-amber glow-amber"
            delay={100}
          />
          <StatCard
            label="RX Total"
            value={formatBytes(traffic.total_rx_bytes)}
            glowClass="text-accent-purple"
            delay={150}
          />
        </div>
      )}

      {/* ── Filter Toolbar ── */}
      <div className="flex flex-wrap items-center gap-3 bg-bg-surface border border-border-subtle rounded-lg px-4 py-3">
        {/* Protocol pills */}
        <div className="flex items-center gap-1.5">
          {(['ipv4', 'ipv6'] as const).map((proto) => {
            const active = protocolFilter.has(proto)
            const activeClass = proto === 'ipv4'
              ? 'text-accent-cyan bg-accent-cyan/10 border-accent-cyan/20'
              : 'text-accent-purple bg-accent-purple/10 border-accent-purple/20'
            return (
              <button
                key={proto}
                onClick={() => toggleProtocol(proto)}
                className={`px-2.5 py-1 rounded text-xs font-mono font-medium transition-all cursor-pointer border ${
                  active
                    ? activeClass
                    : 'bg-bg-elevated text-text-muted border-transparent hover:text-text-secondary'
                }`}
              >
                {proto === 'ipv4' ? 'IPv4' : 'IPv6'}
              </button>
            )
          })}
        </div>

        <div className="w-px h-5 bg-border-subtle" />

        {/* Transport pills */}
        <div className="flex items-center gap-1.5">
          {(['tcp', 'udp'] as const).map((t) => {
            const active = transportFilter.has(t)
            const activeClass = t === 'tcp'
              ? 'text-accent-green bg-accent-green/10 border-accent-green/20'
              : 'text-accent-amber bg-accent-amber/10 border-accent-amber/20'
            return (
              <button
                key={t}
                onClick={() => toggleTransport(t)}
                className={`px-2.5 py-1 rounded text-xs font-mono font-medium transition-all cursor-pointer border ${
                  active
                    ? activeClass
                    : 'bg-bg-elevated text-text-muted border-transparent hover:text-text-secondary'
                }`}
              >
                {t.toUpperCase()}
              </button>
            )
          })}
        </div>

        <div className="w-px h-5 bg-border-subtle" />

        {/* Device dropdown */}
        <select
          value={deviceFilter}
          onChange={(e) => setDeviceFilter(e.target.value)}
          className="bg-bg-elevated border border-border-subtle text-text-secondary text-xs font-mono rounded px-2 py-1.5 focus:outline-none focus:border-accent-cyan/40"
        >
          <option value="">All Devices</option>
          {deviceOptions.map((d) => (
            <option key={d} value={d}>{d}</option>
          ))}
        </select>

        <div className="w-px h-5 bg-border-subtle" />

        {/* Search */}
        <input
          type="text"
          placeholder="Search domain..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="bg-bg-elevated border border-border-subtle text-text-primary text-xs font-mono rounded px-3 py-1.5 w-48 placeholder:text-text-muted focus:outline-none focus:border-accent-cyan/40"
        />

        {/* Result count */}
        <span className="text-xs text-text-muted font-mono ml-auto">
          {sorted.length} shown
        </span>
      </div>

      {/* ── Traffic Table ── */}
      <div className="bg-bg-surface border border-border-subtle rounded-lg overflow-hidden">
        <div className="slot-scroll-container">
          {sorted.length === 0 ? (
            <div className="flex items-center justify-center h-48 text-text-muted font-mono text-sm">
              {traffic ? 'No traffic recorded yet' : 'Connecting...'}
            </div>
          ) : (
            <table className="w-full">
              <thead className="sticky top-0 bg-bg-surface border-b border-border-subtle z-10">
                <tr>
                  <SortHeader field="domain" current={sortField} dir={sortDir} onSort={handleSort}>
                    Domain
                  </SortHeader>
                  <SortHeader field="device_alias" current={sortField} dir={sortDir} onSort={handleSort}>
                    Device
                  </SortHeader>
                  <th className="px-3 py-2.5 text-left text-xs text-text-muted uppercase tracking-wider font-medium">
                    Proto
                  </th>
                  <SortHeader field="connection_count" current={sortField} dir={sortDir} onSort={handleSort} align="right">
                    Conn
                  </SortHeader>
                  <SortHeader field="active_connections" current={sortField} dir={sortDir} onSort={handleSort} align="right">
                    Active
                  </SortHeader>
                  <SortHeader field="data" current={sortField} dir={sortDir} onSort={handleSort} align="right">
                    Data
                  </SortHeader>
                  <SortHeader field="last_seen_at" current={sortField} dir={sortDir} onSort={handleSort} align="right">
                    Last Active
                  </SortHeader>
                </tr>
              </thead>
              <tbody>
                {sorted.map((entry) => (
                  <TrafficRow key={`${entry.domain}:${entry.port}:${entry.device_alias}:${entry.protocol}:${entry.transport}`} entry={entry} />
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  )
}

// ─── Table row (extracted for content-visibility optimization) ───

function TrafficRow({ entry }: { entry: TrafficEntry }) {
  const totalBytes = entry.tx_bytes + entry.rx_bytes
  return (
    <tr className="slot-row border-b border-border-subtle/50 hover:bg-bg-surface-hover/50 transition-colors">
      {/* Domain */}
      <td className="px-3 py-2 font-mono text-xs text-text-primary max-w-[280px]" title={`${entry.domain}:${entry.port}`}>
        <span className="truncate block">{formatDomain(entry.domain, entry.port)}</span>
      </td>

      {/* Device */}
      <td className="px-3 py-2 font-mono text-xs text-text-secondary">
        {entry.device_alias}
      </td>

      {/* Protocol badge */}
      <td className="px-3 py-2">
        <span className={`inline-block px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold ${
          entry.protocol === 'ipv6'
            ? 'bg-accent-purple/15 text-accent-purple'
            : 'bg-accent-cyan/15 text-accent-cyan'
        }`}>
          {entry.protocol === 'ipv6' ? 'v6' : 'v4'}
        </span>
        {(entry.transport === 'udp') && (
          <span className="inline-block px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold bg-accent-amber/15 text-accent-amber ml-1">
            UDP
          </span>
        )}
      </td>

      {/* Connections */}
      <td className="px-3 py-2 font-mono text-xs text-text-primary text-right tabular-nums">
        {entry.connection_count.toLocaleString()}
      </td>

      {/* Active — green glow when > 0 */}
      <td className={`px-3 py-2 font-mono text-xs text-right tabular-nums ${
        entry.active_connections > 0 ? 'text-accent-green glow-green' : 'text-text-muted'
      }`}>
        {entry.active_connections}
      </td>

      {/* Data (combined) with TX/RX breakdown */}
      <td className="px-3 py-2 font-mono text-xs text-right">
        <span className="text-text-primary tabular-nums">{formatBytes(totalBytes)}</span>
        {totalBytes > 0 && (
          <span className="text-text-muted text-[10px] block tabular-nums">
            ↑{formatBytes(entry.tx_bytes)} ↓{formatBytes(entry.rx_bytes)}
          </span>
        )}
      </td>

      {/* Last Active */}
      <td className="px-3 py-2 font-mono text-xs text-text-secondary text-right">
        {formatRelative(entry.last_seen_at)}
      </td>
    </tr>
  )
}
