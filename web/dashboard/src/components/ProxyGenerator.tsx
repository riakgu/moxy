import { useState, useMemo } from 'react'
import type { Device, Slot } from '../api/types'
import CopyButton from './CopyButton'

interface ProxyGeneratorProps {
  devices: Device[]
  slots: Slot[]
}

type Tier = 'shared' | 'device' | 'slot'
type Protocol = 'socks5' | 'http'
type IpVersion = 'ipv4' | 'ipv6'

function extractDeviceIndex(alias: string): number {
  const match = alias.match(/^dev(\d+)$/)
  return match ? parseInt(match[1], 10) : 1
}

function extractSlotIndex(name: string): number {
  const match = name.match(/^slot(\d+)$/)
  return match ? parseInt(match[1], 10) : 0
}

const IPV4_PROXY_PORT = 1080
const IPV4_SLOT_PORT_START = 10000
const IPV6_PROXY_PORT = 2080
const IPV6_SLOT_PORT_START = 20000

export default function ProxyGenerator({ devices, slots }: ProxyGeneratorProps) {
  const [tier, setTier] = useState<Tier>('shared')
  const [protocol, setProtocol] = useState<Protocol>('socks5')
  const [ipVersion, setIpVersion] = useState<IpVersion>('ipv4')
  const [selectedDevice, setSelectedDevice] = useState('')
  const [selectedSlot, setSelectedSlot] = useState('')

  const host = window.location.hostname || 'localhost'

  // Auto-select first device/slot when data arrives
  const activeDevice = selectedDevice || devices[0]?.alias || ''
  const activeSlot = selectedSlot || slots[0]?.name || ''

  const proxyPort = ipVersion === 'ipv6' ? IPV6_PROXY_PORT : IPV4_PROXY_PORT
  const slotPortStart = ipVersion === 'ipv6' ? IPV6_SLOT_PORT_START : IPV4_SLOT_PORT_START

  const port = useMemo(() => {
    switch (tier) {
      case 'shared':
        return proxyPort
      case 'device':
        return proxyPort + extractDeviceIndex(activeDevice)
      case 'slot':
        return slotPortStart + extractSlotIndex(activeSlot)
    }
  }, [tier, activeDevice, activeSlot, proxyPort, slotPortStart])

  const connectionString = `${protocol}://${host}:${port}`
  const curlCommand = `curl -x ${connectionString} https://api.ipify.org`

  // Bulk copy: all slot proxy strings (one per line)
  const allSlotProxies = useMemo(() => {
    return slots
      .map((s) => `${protocol}://${host}:${slotPortStart + extractSlotIndex(s.name)}`)
      .join('\n')
  }, [slots, protocol, host, slotPortStart])

  // Warning check
  const warning = useMemo(() => {
    if (tier === 'device') {
      const dev = devices.find((d) => d.alias === activeDevice)
      if (dev && dev.status !== 'online') return `${activeDevice} is ${dev.status}`
    }
    if (tier === 'slot') {
      const s = slots.find((sl) => sl.name === activeSlot)
      if (s && s.status !== 'healthy') return `${activeSlot} is ${s.status}`
    }
    return null
  }, [tier, activeDevice, activeSlot, devices, slots])

  const tierOptions: { value: Tier; label: string; desc: string }[] = [
    { value: 'shared', label: 'Shared', desc: 'All devices' },
    { value: 'device', label: 'Device', desc: 'One device' },
    { value: 'slot', label: 'Per-Slot', desc: 'Specific IP' },
  ]

  return (
    <div className="animate-fade-up border-t-2 border-accent-cyan/30 bg-bg-surface/50 rounded-lg p-6"
      style={{ animationDelay: '300ms' }}
    >
      <h2 className="font-mono text-lg font-semibold text-text-primary mb-5 tracking-wide">
        <span className="text-accent-cyan">▸</span> Proxy Generator
      </h2>

      <div className="space-y-5">
        {/* Tier selection */}
        <div>
          <p className="text-xs text-text-muted uppercase tracking-wider mb-2 font-medium">Routing Tier</p>
          <div className="grid grid-cols-3 gap-3">
            {tierOptions.map((opt) => (
              <button
                key={opt.value}
                onClick={() => setTier(opt.value)}
                className={`p-3 rounded-lg border text-left transition-all cursor-pointer ${
                  tier === opt.value
                    ? 'border-accent-cyan bg-accent-cyan/10 text-accent-cyan'
                    : 'border-border-subtle bg-bg-surface hover:border-border-active text-text-secondary'
                }`}
              >
                <span className="block text-sm font-medium">{opt.label}</span>
                <span className="block text-xs text-text-muted mt-0.5">{opt.desc}</span>
              </button>
            ))}
          </div>
        </div>

        {/* IP Version toggle */}
        <div>
          <p className="text-xs text-text-muted uppercase tracking-wider mb-2 font-medium">IP Version</p>
          <div className="inline-flex rounded-lg overflow-hidden border border-border-subtle">
            {(['ipv4', 'ipv6'] as IpVersion[]).map((v) => (
              <button
                key={v}
                onClick={() => setIpVersion(v)}
                className={`px-4 py-2 text-sm font-mono font-medium uppercase transition-colors cursor-pointer ${
                  ipVersion === v
                    ? 'bg-accent-cyan/15 text-accent-cyan'
                    : 'bg-bg-surface text-text-muted hover:text-text-secondary'
                }`}
              >
                {v}
              </button>
            ))}
          </div>
        </div>

        {/* Device/Slot dropdown when applicable */}
        {tier === 'device' && (
          <div>
            <p className="text-xs text-text-muted uppercase tracking-wider mb-2 font-medium">Device</p>
            <select
              value={activeDevice}
              onChange={(e) => setSelectedDevice(e.target.value)}
              className="w-full px-3 py-2 bg-bg-primary border border-border-subtle rounded font-mono text-sm
                text-text-primary focus:border-accent-cyan focus:outline-none"
            >
              {devices.map((d) => (
                <option key={d.alias} value={d.alias}>
                  {d.alias} — {d.carrier || d.serial} ({d.status})
                </option>
              ))}
            </select>
          </div>
        )}

        {tier === 'slot' && (
          <div>
            <p className="text-xs text-text-muted uppercase tracking-wider mb-2 font-medium">Slot</p>
            <select
              value={activeSlot}
              onChange={(e) => setSelectedSlot(e.target.value)}
              className="w-full px-3 py-2 bg-bg-primary border border-border-subtle rounded font-mono text-sm
                text-text-primary focus:border-accent-cyan focus:outline-none"
            >
              {slots.map((s) => (
                <option key={s.name} value={s.name}>
                  {s.name} — {s.ipv4_address || 'no IP'} ({s.status})
                </option>
              ))}
            </select>
          </div>
        )}

        {/* Protocol toggle */}
        <div>
          <p className="text-xs text-text-muted uppercase tracking-wider mb-2 font-medium">Protocol</p>
          <div className="inline-flex rounded-lg overflow-hidden border border-border-subtle">
            {(['socks5', 'http'] as Protocol[]).map((p) => (
              <button
                key={p}
                onClick={() => setProtocol(p)}
                className={`px-4 py-2 text-sm font-mono font-medium uppercase transition-colors cursor-pointer ${
                  protocol === p
                    ? 'bg-accent-cyan/15 text-accent-cyan'
                    : 'bg-bg-surface text-text-muted hover:text-text-secondary'
                }`}
              >
                {p}
              </button>
            ))}
          </div>
        </div>

        {/* Warning */}
        {warning && (
          <div className="flex items-center gap-2 px-3 py-2 rounded bg-accent-amber/10 border border-accent-amber/20">
            <span className="text-accent-amber text-sm">⚠</span>
            <span className="text-xs text-accent-amber font-medium">{warning}</span>
          </div>
        )}

        {/* Output */}
        <div className="space-y-3">
          <div>
            <p className="text-xs text-text-muted uppercase tracking-wider mb-1.5 font-medium">Connection String</p>
            <div className="flex items-center gap-2 bg-bg-primary border border-border-subtle rounded px-4 py-3">
              <code className="flex-1 font-mono text-sm text-accent-cyan glow-cyan break-all">
                {connectionString}
              </code>
              <CopyButton text={connectionString} label="Copy" />
            </div>
          </div>

          <div>
            <p className="text-xs text-text-muted uppercase tracking-wider mb-1.5 font-medium">cURL Example</p>
            <div className="flex items-center gap-2 bg-bg-primary border border-border-subtle rounded px-4 py-3">
              <code className="flex-1 font-mono text-xs text-text-secondary break-all">
                {curlCommand}
              </code>
              <CopyButton text={curlCommand} label="Copy" />
            </div>
          </div>

          {/* Bulk copy — per-slot mode */}
          {tier === 'slot' && slots.length > 0 && (
            <div>
              <p className="text-xs text-text-muted uppercase tracking-wider mb-1.5 font-medium">All Per-Slot Proxies</p>
              <div className="bg-bg-primary border border-border-subtle rounded px-4 py-3">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-xs text-text-muted font-mono">{slots.length} proxies • one per line</span>
                  <CopyButton text={allSlotProxies} label={`Copy All (${slots.length})`} />
                </div>
                <pre className="font-mono text-xs text-text-secondary max-h-32 overflow-y-auto whitespace-pre">
{allSlotProxies}
                </pre>
              </div>
            </div>
          )}
        </div>

        {/* Port info */}
        <p className="text-xs text-text-muted font-mono">
          Port {port} • {ipVersion.toUpperCase()} • Both SOCKS5 and HTTP work on the same port (auto-detected)
        </p>
      </div>
    </div>
  )
}
