import { useState, useEffect, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { getConfig, saveConfig, restartService } from '../api/config'
import type { MoxyConfig } from '../api/types'

// Field metadata for rendering the form
interface FieldDef {
  key: string
  subgroup?: string
  label: string
  type: 'number' | 'text' | 'select'
  options?: string[]
  description: string
  warning?: string
  min?: number
  max?: number
  readonly?: boolean
}

const SECTIONS: { title: string; group: keyof MoxyConfig; fields: FieldDef[] }[] = [
  {
    title: 'Proxy',
    group: 'proxy',
    fields: [
      { key: 'port', subgroup: 'ipv4', label: 'IPv4 Port', type: 'number', min: 1, max: 65535, description: 'Main SOCKS5/HTTP proxy port', readonly: true },
      { key: 'slot_port_start', subgroup: 'ipv4', label: 'IPv4 Slot Port Start', type: 'number', min: 1, max: 65535, description: 'First port for per-slot proxy listeners', readonly: true },
      { key: 'port', subgroup: 'ipv6', label: 'IPv6 Port', type: 'number', min: 0, max: 65535, description: 'IPv6-preferred proxy port (0 = disabled)', readonly: true },
      { key: 'slot_port_start', subgroup: 'ipv6', label: 'IPv6 Slot Port Start', type: 'number', min: 0, max: 65535, description: 'First port for per-slot IPv6 listeners (0 = disabled)', readonly: true },
      { key: 'source_ip_strategy', label: 'Strategy', type: 'select', options: ['random', 'round-robin', 'least-connections'], description: 'Load balancing strategy for slot selection' },
      { key: 'udp_idle_timeout_seconds', label: 'UDP Idle Timeout (s)', type: 'number', min: 10, description: 'Seconds of inactivity before closing a UDP association' },
      { key: 'udp_max_associations', label: 'UDP Max Associations', type: 'number', min: 1, max: 10000, description: 'Maximum concurrent UDP ASSOCIATE sessions' },
    ],
  },
  {
    title: 'API',
    group: 'api',
    fields: [
      { key: 'port', label: 'Port', type: 'number', min: 1, max: 65535, description: 'Dashboard and API port', readonly: true },
    ],
  },
  {
    title: 'Devices',
    group: 'devices',
    fields: [
      { key: 'grace_period_seconds', label: 'Grace Period (s)', type: 'number', min: 1, description: 'Seconds to wait before tearing down a disconnected device' },
      { key: 'watcher_reconnect_max_seconds', label: 'Watcher Reconnect Max (s)', type: 'number', min: 1, description: 'Maximum backoff for ADB watcher reconnection', readonly: true },
      { key: 'drain_timeout_seconds', label: 'Drain Timeout (s)', type: 'number', min: 1, description: 'Seconds to wait for active connections before force-destroying a slot', readonly: true },
    ],
  },
  {
    title: 'Slots',
    group: 'slots',
    fields: [
      { key: 'max_slots', label: 'Max Slots', type: 'number', min: 1, max: 10000, description: 'Global maximum slots across all devices' },
      { key: 'max_slots_per_device', label: 'Max Slots Per Device', type: 'number', min: 1, max: 1000, description: 'Maximum network namespaces per USB device' },
      { key: 'max_pool_size', label: 'Max Pool Size', type: 'number', min: 1, description: 'Maximum IPs tracked per slot (oldest evicted when exceeded)' },
      { key: 'ip_check_host', label: 'IP Check Host', type: 'text', description: 'Hostname used for IP discovery checks' },
      { key: 'monitor_fast_interval_seconds', label: 'Fast Interval (s)', type: 'number', min: 1, description: 'Check interval during initial FAST phase', readonly: true },
      { key: 'monitor_steady_interval_seconds', label: 'Steady Interval (s)', type: 'number', min: 1, description: 'Check interval during STEADY phase' },
      { key: 'monitor_recovery_interval_seconds', label: 'Recovery Interval (s)', type: 'number', min: 1, description: 'Check interval during RECOVERY phase', readonly: true },
      { key: 'monitor_fast_ticks', label: 'Fast Ticks', type: 'number', min: 1, description: 'Number of checks in FAST phase before transitioning to STEADY', readonly: true },
      { key: 'monitor_unhealthy_threshold', label: 'Unhealthy Threshold', type: 'number', min: 1, description: 'Consecutive failures before marking slot unhealthy' },
    ],
  },
  {
    title: 'DNS Cache',
    group: 'dns',
    fields: [
      { key: 'cache_max_entries_per_device', label: 'Max Entries Per Device', type: 'number', min: 100, description: 'LRU cache size per device' },
      { key: 'cache_min_ttl_seconds', label: 'Min TTL (s)', type: 'number', min: 1, description: 'Minimum cache TTL (clamps low DNS TTLs)' },
      { key: 'cache_max_ttl_seconds', label: 'Max TTL (s)', type: 'number', min: 1, description: 'Maximum cache TTL (clamps high DNS TTLs)' },
    ],
  },
  {
    title: 'Traffic',
    group: 'traffic',
    fields: [
      { key: 'max_tracked', label: 'Max Tracked Destinations', type: 'number', min: 100, description: 'Maximum traffic entries before LRU eviction' },
    ],
  },
  {
    title: 'SSE',
    group: 'sse',
    fields: [
      { key: 'debounce_ms', label: 'Debounce (ms)', type: 'number', min: 100, description: 'Event coalescing window for SSE push', readonly: true },
      { key: 'heartbeat_seconds', label: 'Heartbeat (s)', type: 'number', min: 5, description: 'SSE keepalive ping interval', readonly: true },
      { key: 'max_clients', label: 'Max Clients', type: 'number', min: 1, description: 'Maximum concurrent SSE connections', readonly: true },
      { key: 'traffic_snapshot_limit', label: 'Traffic Snapshot Limit', type: 'number', min: 10, description: 'Max traffic entries pushed via SSE (REST returns all)', readonly: true },
    ],
  },
  {
    title: 'Server',
    group: 'server',
    fields: [
      { key: 'shutdown_drain_seconds', label: 'Shutdown Drain (s)', type: 'number', min: 1, description: 'Seconds to wait for in-flight requests during graceful shutdown', readonly: true },
    ],
  },
  {
    title: 'Log',
    group: 'log',
    fields: [
      { key: 'level', label: 'Level', type: 'select', options: ['debug', 'info', 'warn', 'error'], description: 'Minimum log level' },
      { key: 'format', label: 'Format', type: 'select', options: ['json', 'text'], description: 'Log output format', readonly: true },
      { key: 'ring_buffer_size', label: 'Ring Buffer Size', type: 'number', min: 100, description: 'In-memory log buffer entries for SSE streaming', readonly: true },
    ],
  },
]

export default function Config() {
  const [config, setConfig] = useState<MoxyConfig | null>(null)
  const [savedConfig, setSavedConfig] = useState<MoxyConfig | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const [restarting, setRestarting] = useState(false)
  const [toast, setToast] = useState<{ msg: string; type: 'success' | 'error' } | null>(null)
  const [showRestartModal, setShowRestartModal] = useState(false)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [loadError, setLoadError] = useState<string | null>(null)

  useEffect(() => {
    getConfig()
      .then((cfg) => {
        setConfig(cfg)
        setSavedConfig(cfg)
      })
      .catch((err) => setLoadError(err.message))
  }, [])

  const isDirty = config && savedConfig && JSON.stringify(config) !== JSON.stringify(savedConfig)

  const updateField = useCallback((group: keyof MoxyConfig, key: string, value: string | number, subgroup?: string) => {
    setConfig((prev) => {
      if (!prev) return prev
      if (subgroup) {
        const groupObj = prev[group] as Record<string, unknown>
        const sub = groupObj[subgroup] as Record<string, unknown>
        return {
          ...prev,
          [group]: { ...groupObj, [subgroup]: { ...sub, [key]: value } },
        }
      }
      return {
        ...prev,
        [group]: { ...(prev[group] as Record<string, unknown>), [key]: value },
      }
    })
    const errorKey = subgroup ? `${String(group)}.${subgroup}.${key}` : `${String(group)}.${key}`
    setErrors((prev) => {
      const next = { ...prev }
      delete next[errorKey]
      return next
    })
  }, [])

  const handleDiscard = useCallback(() => {
    if (savedConfig) {
      setConfig(JSON.parse(JSON.stringify(savedConfig)))
    }
    setErrors({})
  }, [savedConfig])

  const showToast = useCallback((msg: string, type: 'success' | 'error') => {
    setToast({ msg, type })
    setTimeout(() => setToast(null), 5000)
  }, [])

  const handleSave = useCallback(async () => {
    if (!config) return
    setSaving(true)
    setErrors({})
    try {
      const saved = await saveConfig(config)
      setSavedConfig(saved)
      setConfig(saved)
      showToast('Config saved. Restart required to apply.', 'success')
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : String(err)
      try {
        const body = JSON.parse(message)
        if (body.errors) {
          setErrors(body.errors)
          showToast('Validation failed. Check highlighted fields.', 'error')
          return
        }
      } catch { /* not JSON, use raw message */ }
      showToast('Failed to save: ' + message, 'error')
    } finally {
      setSaving(false)
    }
  }, [config, showToast])

  const handleRestart = useCallback(async () => {
    setRestarting(true)
    try {
      await restartService()
      setShowRestartModal(false)
      showToast('Restarting... Dashboard will reconnect automatically.', 'success')
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : String(err)
      showToast('Restart failed: ' + message, 'error')
      setRestarting(false)
    }
    // Don't set restarting=false on success — the process is dying.
    // SSE reconnect handles recovery.
  }, [showToast])

  const toggleCollapse = useCallback((title: string) => {
    setCollapsed((prev) => ({ ...prev, [title]: !prev[title] }))
  }, [])

  const isFieldChanged = useCallback((group: keyof MoxyConfig, key: string, subgroup?: string): boolean => {
    if (!config || !savedConfig) return false
    if (subgroup) {
      const currentSub = (config[group] as unknown as Record<string, Record<string, unknown>>)[subgroup]
      const savedSub = (savedConfig[group] as unknown as Record<string, Record<string, unknown>>)[subgroup]
      return currentSub?.[key] !== savedSub?.[key]
    }
    const current = (config[group] as Record<string, unknown>)[key]
    const saved = (savedConfig[group] as Record<string, unknown>)[key]
    return current !== saved
  }, [config, savedConfig])

  // Loading state
  if (loadError) {
    return (
      <div className="animate-fade-up flex items-center justify-center h-64">
        <div className="text-center">
          <div className="text-accent-red text-lg font-mono mb-2">⚠ LOAD FAILED</div>
          <p className="text-text-muted text-sm font-mono">{loadError}</p>
        </div>
      </div>
    )
  }

  if (!config) {
    return (
      <div className="animate-fade-up flex items-center justify-center h-64">
        <div className="text-text-muted font-mono text-sm flex items-center gap-3">
          <span className="inline-block w-4 h-4 border-2 border-accent-cyan/40 border-t-accent-cyan rounded-full animate-spin-slow" />
          Loading configuration...
        </div>
      </div>
    )
  }

  return (
    <div className="animate-fade-up space-y-5 pb-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary font-mono tracking-wide">
            <span className="text-accent-cyan">▌</span> CONFIGURATION
          </h1>
          <p className="text-xs text-text-muted mt-1 font-mono">
            {SECTIONS.reduce((n, s) => n + s.fields.length, 0)} parameters · {SECTIONS.length} groups
          </p>
        </div>
      </div>

      {/* Unsaved changes bar */}
      {isDirty && (
        <div className="sticky top-14 z-40 flex items-center justify-between bg-accent-amber/10 border border-accent-amber/30 rounded-lg px-4 py-2.5 backdrop-blur-sm">
          <div className="flex items-center gap-2">
            <span className="inline-block w-2 h-2 rounded-full bg-accent-amber animate-pulse-badge" />
            <span className="text-accent-amber text-sm font-medium font-mono">Unsaved changes</span>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleDiscard}
              className="px-3 py-1.5 rounded text-xs font-mono font-medium border border-border-subtle bg-bg-surface text-text-secondary hover:text-text-primary hover:bg-bg-surface-hover transition-colors"
            >
              Discard
            </button>
            <button
              onClick={handleSave}
              disabled={saving}
              className="px-4 py-1.5 rounded text-xs font-mono font-medium bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/30 hover:bg-accent-cyan/30 transition-colors disabled:opacity-50"
            >
              {saving ? (
                <span className="flex items-center gap-2">
                  <span className="inline-block w-3 h-3 border-2 border-accent-cyan/40 border-t-accent-cyan rounded-full animate-spin-slow" />
                  Saving...
                </span>
              ) : (
                'Save'
              )}
            </button>
          </div>
        </div>
      )}

      {/* Config sections */}
      {SECTIONS.map((section, sIdx) => (
        <div
          key={section.title}
          className="bg-bg-surface border border-border-subtle rounded-lg card-glow overflow-hidden"
          style={{ animationDelay: `${sIdx * 40}ms` }}
        >
          {/* Section header */}
          <button
            onClick={() => toggleCollapse(section.title)}
            className="w-full flex items-center justify-between px-5 py-3.5 hover:bg-bg-surface-hover/50 transition-colors"
          >
            <h2 className="text-sm font-semibold text-text-primary font-mono tracking-wider uppercase">
              {section.title}
            </h2>
            <div className="flex items-center gap-3">
              {/* Changed indicator */}
              {section.fields.some((f) => isFieldChanged(section.group, f.key, f.subgroup)) && (
                <span className="inline-block w-1.5 h-1.5 rounded-full bg-accent-amber" title="Has changes" />
              )}
              <span className="text-text-muted text-xs transition-transform duration-200" style={{
                transform: collapsed[section.title] ? 'rotate(-90deg)' : 'rotate(0deg)',
              }}>
                ▼
              </span>
            </div>
          </button>

          {/* Section body */}
          {!collapsed[section.title] && (
            <div className="border-t border-border-subtle/50 px-5 py-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {section.fields.map((field) => {
                const groupObj = config[section.group] as Record<string, unknown>
                const value = field.subgroup
                  ? ((groupObj[field.subgroup] as Record<string, unknown>)?.[field.key] as string | number | undefined)
                  : (groupObj[field.key] as string | number | undefined)
                const errorKey = field.subgroup
                  ? `${String(section.group)}.${field.subgroup}.${field.key}`
                  : `${String(section.group)}.${field.key}`
                const error = errors[errorKey]
                const changed = isFieldChanged(section.group, field.key, field.subgroup)
                const fieldUniqueKey = field.subgroup ? `${field.subgroup}.${field.key}` : field.key

                return (
                  <div key={fieldUniqueKey} className="space-y-1.5">
                    {/* Label */}
                    <label className="flex items-center gap-1.5 text-xs font-mono text-text-secondary">
                      <span className={changed ? 'text-accent-amber' : ''}>{field.label}</span>
                      {field.warning && (
                        <span className="text-accent-amber cursor-help" title={field.warning}>⚠</span>
                      )}
                    </label>

                    {/* Input */}
                    {field.type === 'select' ? (
                      <select
                        value={String(value ?? '')}
                        onChange={(e) => updateField(section.group, field.key, e.target.value, field.subgroup)}
                        disabled={field.readonly}
                        className={`w-full bg-bg-primary border rounded px-3 py-2 text-sm font-mono text-text-primary focus:outline-none focus:border-accent-cyan/50 focus:ring-1 focus:ring-accent-cyan/20 transition-colors appearance-none ${field.readonly ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'
                          } ${error ? 'border-accent-red/60' : changed ? 'border-accent-amber/40' : 'border-border-subtle'
                          }`}
                      >
                        {field.options!.map((opt) => (
                          <option key={opt} value={opt}>{opt}</option>
                        ))}
                      </select>
                    ) : (
                      <input
                        type={field.type}
                        value={value ?? ''}
                        min={field.min}
                        max={field.max}
                        disabled={field.readonly}
                        onChange={(e) =>
                          updateField(
                            section.group,
                            field.key,
                            field.type === 'number' ? parseInt(e.target.value) || 0 : e.target.value,
                            field.subgroup,
                          )
                        }
                        className={`w-full bg-bg-primary border rounded px-3 py-2 text-sm font-mono text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-cyan/50 focus:ring-1 focus:ring-accent-cyan/20 transition-colors ${field.readonly ? 'opacity-50 cursor-not-allowed' : ''
                          } ${error ? 'border-accent-red/60' : changed ? 'border-accent-amber/40' : 'border-border-subtle'
                          }`}
                      />
                    )}

                    {/* Description */}
                    <p className="text-[10px] text-text-muted font-mono leading-tight">{field.description}</p>

                    {/* Error */}
                    {error && (
                      <p className="text-[10px] text-accent-red font-mono animate-fade-in">✕ {error}</p>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </div>
      ))}

      {/* System section — Restart */}
      <div className="bg-bg-surface border border-border-subtle rounded-lg card-glow overflow-hidden">
        <div className="px-5 py-3.5">
          <h2 className="text-sm font-semibold text-text-primary font-mono tracking-wider uppercase mb-4">
            System
          </h2>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-xs text-text-secondary font-mono">Restart the Moxy service to apply config changes</p>
              <p className="text-[10px] text-text-muted font-mono mt-1">All active connections will be dropped and slots re-provisioned</p>
            </div>
            <button
              onClick={() => setShowRestartModal(true)}
              disabled={restarting}
              className="px-4 py-2 rounded text-xs font-mono font-semibold bg-accent-red/10 text-accent-red border border-accent-red/30 hover:bg-accent-red/20 transition-colors disabled:opacity-50 whitespace-nowrap"
            >
              {restarting ? (
                <span className="flex items-center gap-2">
                  <span className="inline-block w-3 h-3 border-2 border-accent-red/40 border-t-accent-red rounded-full animate-spin-slow" />
                  Restarting...
                </span>
              ) : (
                '⟳ Restart Service'
              )}
            </button>
          </div>
        </div>
      </div>

      {/* Restart confirmation modal — portal to escape transform context */}
      {showRestartModal && createPortal(
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm animate-fade-in">
          <div className="bg-bg-surface border border-border-active rounded-xl shadow-2xl max-w-md w-full mx-4 p-6 space-y-4">
            <h3 className="text-lg font-mono font-semibold text-accent-amber flex items-center gap-2">
              <span>⚠</span> Restart Moxy?
            </h3>

            <div className="space-y-2 text-sm text-text-secondary font-mono">
              <p className="text-text-muted text-xs uppercase tracking-wider">This will:</p>
              <ul className="space-y-1.5 text-xs">
                <li className="flex items-start gap-2">
                  <span className="text-accent-red mt-0.5">✕</span>
                  <span>Drop all active proxy connections</span>
                </li>
                <li className="flex items-start gap-2">
                  <span className="text-accent-red mt-0.5">✕</span>
                  <span>Clear traffic stats, DNS cache, and logs</span>
                </li>
                <li className="flex items-start gap-2">
                  <span className="text-accent-red mt-0.5">✕</span>
                  <span>Re-provision all slots (new IPs will be assigned)</span>
                </li>
              </ul>
            </div>

            <p className="text-xs text-accent-green font-mono flex items-center gap-2">
              <span>✓</span> Devices will be automatically re-detected after restart
            </p>

            <div className="flex items-center justify-end gap-3 pt-2 border-t border-border-subtle">
              <button
                onClick={() => setShowRestartModal(false)}
                className="px-4 py-2 rounded text-xs font-mono font-medium border border-border-subtle bg-bg-surface text-text-secondary hover:text-text-primary hover:bg-bg-surface-hover transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleRestart}
                disabled={restarting}
                className="px-4 py-2 rounded text-xs font-mono font-semibold bg-accent-red/20 text-accent-red border border-accent-red/40 hover:bg-accent-red/30 transition-colors disabled:opacity-50"
              >
                {restarting ? (
                  <span className="flex items-center gap-2">
                    <span className="inline-block w-3 h-3 border-2 border-accent-red/40 border-t-accent-red rounded-full animate-spin-slow" />
                    Restarting...
                  </span>
                ) : (
                  'Restart Now'
                )}
              </button>
            </div>
          </div>
        </div>,
        document.body,
      )}

      {/* Toast — portal to escape transform context */}
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
