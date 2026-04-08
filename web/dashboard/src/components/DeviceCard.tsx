import { useState } from 'react'
import type { Device, Slot } from '../api/types'
import SlotTable from './SlotTable'

interface DeviceCardProps {
  device: Device
  slots: Slot[]
  onProvision: (alias: string, count: number) => Promise<void>
  onDeleteDevice: (alias: string) => Promise<void>
  onSetupDevice: (alias: string) => Promise<void>
  onChangeSlotIP: (name: string) => Promise<void>
  onDeleteSlot: (name: string) => Promise<void>
  host: string
  animationDelay: number
  trafficTotals?: { tx_bytes: number; rx_bytes: number }
}

const deviceStatusStyles: Record<string, { dot: string; text: string; class: string }> = {
  detected: { dot: 'bg-accent-purple animate-pulse-badge', text: 'Detected', class: 'text-accent-purple' },
  online: { dot: 'bg-accent-green', text: 'Online', class: 'text-accent-green' },
  setup: { dot: 'bg-accent-amber animate-pulse-badge', text: 'Setting Up', class: 'text-accent-amber' },
  disconnected: { dot: 'bg-accent-amber animate-pulse-badge', text: 'Disconnected', class: 'text-accent-amber' },
  error: { dot: 'bg-accent-red', text: 'Error', class: 'text-accent-red' },
  offline: { dot: 'bg-text-muted', text: 'Offline', class: 'text-text-muted' },
}

const setupStepLabels: Record<string, string> = {
  screen_unlocked: 'Checking screen lock',
  enabled_tethering: 'Enabling tethering',
  interface_detected: 'Detecting interface',
  enabled_data: 'Enabling mobile data',
  dismissed_dialog: 'Dismissing dialogs',
  disabled_wifi: 'Disabling WiFi',
  dhcp_configured: 'Configuring DHCP',
  ipv6_configured: 'Configuring IPv6',
  ipv6_verified: 'Verifying IPv6',
  isp_probed: 'Probing ISP / DNS',
  carrier_detected: 'Detecting carrier',
  device_info: 'Reading device info',
}

const formatBytes = (bytes: number): string => {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  const value = bytes / Math.pow(1024, i)
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[i]}`
}

export default function DeviceCard({
  device, slots, onProvision, onDeleteDevice, onSetupDevice,
  onChangeSlotIP, onDeleteSlot, host, animationDelay, trafficTotals,
}: DeviceCardProps) {
  const [expanded, setExpanded] = useState(false)
  const [provisionCount, setProvisionCount] = useState(5)
  const [provisioning, setProvisioning] = useState(false)
  const [settingUp, setSettingUp] = useState(false)
  const [deletingDevice, setDeletingDevice] = useState(false)

  const isDetected = device.status === 'detected'
  const isOnline = device.status === 'online'
  const isDisconnected = device.status === 'disconnected'
  const isExpandable = isOnline || isDisconnected
  const status = deviceStatusStyles[device.status] ?? deviceStatusStyles.offline

  const handleSetup = async () => {
    setSettingUp(true)
    try {
      await onSetupDevice(device.alias)
    } finally {
      setSettingUp(false)
    }
  }

  const handleProvision = async () => {
    setProvisioning(true)
    try {
      await onProvision(device.alias, provisionCount)
    } finally {
      setProvisioning(false)
    }
  }

  const handleDelete = async () => {
    if (!window.confirm(`Delete ${device.alias}? This will destroy all its slots and namespaces.`)) return
    setDeletingDevice(true)
    try {
      await onDeleteDevice(device.alias)
    } finally {
      setDeletingDevice(false)
    }
  }

  return (
    <div
      className={`animate-fade-up bg-bg-surface border border-border-subtle rounded-lg overflow-hidden card-glow
        ${isDetected ? 'opacity-70' : ''}`}
      style={{ animationDelay: `${animationDelay}ms` }}
    >
      {/* Header — clickable to expand (only if online) */}
      <button
        onClick={() => isExpandable && setExpanded(!expanded)}
        className={`w-full px-5 py-4 flex items-center justify-between transition-colors text-left
          ${isExpandable ? 'cursor-pointer hover:bg-bg-surface-hover/30' : 'cursor-default'}`}
      >
        <div className="flex items-center gap-4">
          <span className="font-mono text-2xl font-semibold text-accent-cyan">{device.alias}</span>
          <span className="text-sm text-text-secondary">
            {isDetected
              ? device.serial
              : (device.model
                ? `${device.brand ? device.brand + ' ' : ''}${device.model}`
                : (device.carrier || 'Unknown carrier'))}
          </span>
          <span className={`inline-flex items-center gap-1.5 text-xs font-medium ${status.class}`}>
            <span className={`w-1.5 h-1.5 rounded-full ${status.dot}`} />
            {status.text}
          </span>
          {device.status === 'setup' && device.setup_step && (
            <span className="text-xs text-accent-amber/70 font-mono">
              — {setupStepLabels[device.setup_step] || device.setup_step}…
            </span>
          )}
        </div>
        {!isDetected && (
          <div className="flex items-center gap-4">
            <span className="font-mono text-sm text-text-secondary">
              {device.slot_count} slot{device.slot_count !== 1 ? 's' : ''}
            </span>
            {(() => {
              const uniqueIPs = new Set(
                slots.map(s => [...(s.public_ipv4s ?? [])].filter(Boolean).sort().join(','))
                     .filter(p => p !== '')
              ).size
              return uniqueIPs > 0 ? (
                <span className="font-mono text-sm text-accent-purple">
                  {uniqueIPs} IP{uniqueIPs !== 1 ? 's' : ''}
                </span>
              ) : null
            })()}
            {isExpandable && (
              <span className={`text-text-muted transition-transform ${expanded ? 'rotate-180' : ''}`}>
                ▾
              </span>
            )}
          </div>
        )}
      </button>

      {/* Details — only for non-detected devices */}
      {!isDetected && (
        <div className="px-5 pb-3 flex flex-wrap gap-x-6 gap-y-1 text-xs text-text-muted font-mono">
          {device.carrier && (
            <span>carrier: <span className="text-text-secondary">{device.carrier}</span></span>
          )}
          {device.android_version && (
            <span>android: <span className="text-text-secondary">{device.android_version}</span></span>
          )}
          <span>iface: <span className="text-text-secondary">{device.interface}</span></span>
          <span>serial: <span className="text-text-secondary">{device.serial}</span></span>
          {device.nameserver && (
            <span>dns: <span className="text-text-secondary">{device.nameserver}</span></span>
          )}
          {device.nat64_prefix && (
            <span>nat64: <span className="text-text-secondary">{device.nat64_prefix}</span></span>
          )}
          <span>data: <span className="text-accent-amber">{formatBytes(trafficTotals ? trafficTotals.tx_bytes + trafficTotals.rx_bytes : device.total_bytes)}</span>
            <span className="text-text-muted"> (↑{formatBytes(trafficTotals?.tx_bytes ?? device.tx_bytes)} ↓{formatBytes(trafficTotals?.rx_bytes ?? device.rx_bytes)})</span>
          </span>
        </div>
      )}

      {/* Actions */}
      <div className="px-5 pb-4 flex items-center gap-3">
        {isDetected ? (
          /* Detected: Setup button only */
          <button
            onClick={handleSetup}
            disabled={settingUp}
            className="px-4 py-2 text-sm font-medium rounded-lg bg-accent-green/15 text-accent-green
              border border-accent-green/30 hover:bg-accent-green/25
              hover:shadow-[0_0_20px_rgba(74,222,128,0.15)]
              disabled:opacity-50 transition-all cursor-pointer disabled:cursor-wait"
          >
            {settingUp ? (
              <span className="inline-flex items-center gap-2">
                <span className="inline-block w-4 h-4 border-2 border-accent-green border-t-transparent rounded-full animate-spin-slow" />
                Setting Up...
              </span>
            ) : (
              '⚡ Setup Device'
            )}
          </button>
        ) : (
          /* Online/Other: Provision + Delete */
          <>
            <div className="flex items-center gap-2">
              <input
                type="number"
                min={1}
                max={250}
                value={provisionCount}
                onChange={(e) => setProvisionCount(parseInt(e.target.value, 10) || 1)}
                className="w-16 px-2 py-1.5 bg-bg-primary border border-border-subtle rounded text-sm
                  font-mono text-text-primary focus:border-accent-cyan focus:outline-none"
              />
              <button
                onClick={handleProvision}
                disabled={provisioning || !isOnline}
                className="px-3 py-1.5 text-xs font-medium rounded bg-accent-cyan/15 text-accent-cyan
                  hover:bg-accent-cyan/25 disabled:opacity-50 transition-colors cursor-pointer disabled:cursor-wait"
              >
                {provisioning ? (
                  <span className="inline-flex items-center gap-1.5">
                    <span className="inline-block w-3 h-3 border border-accent-cyan border-t-transparent rounded-full animate-spin-slow" />
                    Provisioning
                  </span>
                ) : (
                  'Provision'
                )}
              </button>
            </div>
            <button
              onClick={handleDelete}
              disabled={deletingDevice}
              className="px-3 py-1.5 text-xs font-medium rounded bg-accent-red/10 text-accent-red
                hover:bg-accent-red/20 disabled:opacity-50 transition-colors cursor-pointer disabled:cursor-wait"
            >
              {deletingDevice ? 'Deleting...' : 'Delete Device'}
            </button>
          </>
        )}
      </div>

      {/* Expandable slot table */}
      {expanded && isExpandable && (
        <div className="animate-fade-in border-t border-border-subtle/50 px-5 py-3">
          <SlotTable
            slots={slots}
            onChangeIP={onChangeSlotIP}
            onDelete={onDeleteSlot}
            host={host}
          />
        </div>
      )}
    </div>
  )
}
