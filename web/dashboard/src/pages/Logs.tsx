import { useState, useEffect, useRef, useMemo } from 'react'
import { useOutletContext } from 'react-router-dom'
import type { LogEntry } from '../api/types'

interface LogsContext {
  logs: LogEntry[]
}

const LEVEL_CONFIG: Record<string, { color: string; bg: string; glow: string }> = {
  DEBUG: { color: 'text-text-muted', bg: 'bg-text-muted/10', glow: '' },
  INFO: { color: 'text-accent-cyan', bg: 'bg-accent-cyan/10', glow: 'glow-cyan' },
  WARN: { color: 'text-accent-amber', bg: 'bg-accent-amber/10', glow: 'glow-amber' },
  ERROR: { color: 'text-accent-red', bg: 'bg-accent-red/10', glow: 'glow-red' },
}

function formatTime(ts: number): string {
  const d = new Date(ts)
  return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' }) +
    '.' + String(d.getMilliseconds()).padStart(3, '0')
}

export default function Logs() {
  const { logs } = useOutletContext<LogsContext>()

  // Filters
  const [levelFilter, setLevelFilter] = useState<Set<string>>(new Set(['DEBUG', 'INFO', 'WARN', 'ERROR']))
  const [componentFilter, setComponentFilter] = useState<string>('')
  const [search, setSearch] = useState('')
  const [paused, setPaused] = useState(false)
  const [pausedLogs, setPausedLogs] = useState<LogEntry[]>([])

  // Auto-scroll
  const scrollRef = useRef<HTMLDivElement>(null)
  const [autoScroll, setAutoScroll] = useState(true)

  // When paused, snapshot logs
  useEffect(() => {
    if (paused) {
      setPausedLogs(logs)
    }
  }, [paused]) // eslint-disable-line react-hooks/exhaustive-deps

  const activeLogs = paused ? pausedLogs : logs

  // Extract unique components
  const components = useMemo(() => {
    const set = new Set<string>()
    activeLogs.forEach((l) => {
      if (l.component) set.add(l.component)
    })
    return Array.from(set).sort()
  }, [activeLogs])

  // Filter logs
  const filtered = useMemo(() => {
    return activeLogs.filter((entry) => {
      if (!levelFilter.has(entry.level)) return false
      if (componentFilter && entry.component !== componentFilter) return false
      if (search) {
        const q = search.toLowerCase()
        const match =
          entry.msg.toLowerCase().includes(q) ||
          (entry.component || '').toLowerCase().includes(q) ||
          Object.values(entry.attrs || {}).some((v) => v.toLowerCase().includes(q))
        if (!match) return false
      }
      return true
    })
  }, [activeLogs, levelFilter, componentFilter, search])

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScroll && !paused && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [filtered, autoScroll, paused])

  // Detect manual scroll
  const handleScroll = () => {
    if (!scrollRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current
    const atBottom = scrollHeight - scrollTop - clientHeight < 40
    setAutoScroll(atBottom)
  }

  const toggleLevel = (level: string) => {
    setLevelFilter((prev) => {
      const next = new Set(prev)
      if (next.has(level)) {
        next.delete(level)
      } else {
        next.add(level)
      }
      return next
    })
  }

  return (
    <div className="animate-fade-up space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary font-mono tracking-wide">
            <span className="text-accent-cyan">▌</span> SYSTEM LOGS
          </h1>
          <p className="text-xs text-text-muted mt-1 font-mono">
            {filtered.length.toLocaleString()} entries
            {paused && <span className="text-accent-amber ml-2">⏸ PAUSED</span>}
            {!autoScroll && !paused && <span className="text-text-secondary ml-2">↑ scroll locked</span>}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setPaused(!paused)}
            className={`px-3 py-1.5 rounded text-xs font-mono font-medium transition-colors border ${
              paused
                ? 'border-accent-amber/40 bg-accent-amber/10 text-accent-amber hover:bg-accent-amber/20'
                : 'border-border-subtle bg-bg-surface text-text-secondary hover:text-text-primary hover:bg-bg-surface-hover'
            }`}
          >
            {paused ? '▶ RESUME' : '⏸ PAUSE'}
          </button>
        </div>
      </div>

      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-3 bg-bg-surface border border-border-subtle rounded-lg px-4 py-3">
        {/* Level pills */}
        <div className="flex items-center gap-1.5">
          {['DEBUG', 'INFO', 'WARN', 'ERROR'].map((level) => {
            const cfg = LEVEL_CONFIG[level]
            const active = levelFilter.has(level)
            return (
              <button
                key={level}
                onClick={() => toggleLevel(level)}
                className={`px-2.5 py-1 rounded text-xs font-mono font-medium transition-all ${
                  active
                    ? `${cfg.bg} ${cfg.color} border border-current/20`
                    : 'bg-bg-elevated text-text-muted border border-transparent hover:text-text-secondary'
                }`}
              >
                {level}
              </button>
            )
          })}
        </div>

        <div className="w-px h-5 bg-border-subtle" />

        {/* Component filter */}
        <select
          value={componentFilter}
          onChange={(e) => setComponentFilter(e.target.value)}
          className="bg-bg-elevated border border-border-subtle text-text-secondary text-xs font-mono rounded px-2 py-1.5 focus:outline-none focus:border-accent-cyan/40"
        >
          <option value="">All Components</option>
          {components.map((c) => (
            <option key={c} value={c}>{c}</option>
          ))}
        </select>

        <div className="w-px h-5 bg-border-subtle" />

        {/* Search */}
        <input
          type="text"
          placeholder="Search logs..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="bg-bg-elevated border border-border-subtle text-text-primary text-xs font-mono rounded px-3 py-1.5 w-48 placeholder:text-text-muted focus:outline-none focus:border-accent-cyan/40"
        />
      </div>

      {/* Log entries */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="log-scroll-container bg-bg-surface border border-border-subtle rounded-lg font-mono text-xs"
      >
        {filtered.length === 0 ? (
          <div className="flex items-center justify-center h-48 text-text-muted">
            <span>No log entries matching filters</span>
          </div>
        ) : (
          <table className="w-full">
            <tbody>
              {filtered.map((entry, i) => {
                const cfg = LEVEL_CONFIG[entry.level] || LEVEL_CONFIG.INFO
                // Build attrs string
                const attrParts: string[] = []
                if (entry.attrs) {
                  Object.entries(entry.attrs).forEach(([k, v]) => {
                    attrParts.push(`${k}=${v}`)
                  })
                }
                return (
                  <tr key={i} className="log-row border-b border-border-subtle/50 hover:bg-bg-surface-hover/50 transition-colors">
                    <td className="px-3 py-1 text-text-muted whitespace-nowrap w-[100px]">
                      {formatTime(entry.time)}
                    </td>
                    <td className={`px-2 py-1 whitespace-nowrap w-[52px] ${cfg.color} font-semibold`}>
                      {entry.level.padEnd(5)}
                    </td>
                    <td className="px-2 py-1 whitespace-nowrap w-[110px]">
                      {entry.component && (
                        <span className="text-accent-purple/80 text-[10px]">
                          [{entry.component}]
                        </span>
                      )}
                    </td>
                    <td className="px-2 py-1 text-text-primary">
                      {entry.msg}
                      {attrParts.length > 0 && (
                        <span className="text-text-muted ml-2">
                          {attrParts.join(' ')}
                        </span>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
