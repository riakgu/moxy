import { useState, useEffect, useRef } from 'react'
import type { Device, Slot, LogEntry, TrafficList, DNSCacheStats, SystemStats } from '../api/types'

interface SSEState {
  devices: Device[]
  slots: Slot[]
  logs: LogEntry[]
  traffic: TrafficList | null
  dnsStats: DNSCacheStats | null
  systemStats: SystemStats | null
  connected: boolean
  error: string | null
}

const MAX_LOG_ENTRIES = 2000

export function useSSE(): SSEState {
  const [devices, setDevices] = useState<Device[]>([])
  const [slots, setSlots] = useState<Slot[]>([])
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [traffic, setTraffic] = useState<TrafficList | null>(null)
  const [dnsStats, setDnsStats] = useState<DNSCacheStats | null>(null)
  const [systemStats, setSystemStats] = useState<SystemStats | null>(null)
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    const es = new EventSource('/api/events')
    esRef.current = es

    es.addEventListener('init', (e: MessageEvent) => {
      const data = JSON.parse(e.data) as { devices: Device[]; slots: Slot[]; logs?: LogEntry[]; traffic?: TrafficList; dns_stats?: DNSCacheStats }
      setDevices(data.devices || [])
      setSlots(data.slots || [])
      setLogs(data.logs || [])
      setTraffic(data.traffic || null)
      setDnsStats(data.dns_stats || null)
      setConnected(true)
      setError(null)
    })

    es.addEventListener('device_updated', (e: MessageEvent) => {
      const device = JSON.parse(e.data) as Device
      setDevices((prev) => {
        const idx = prev.findIndex((d) => d.serial === device.serial)
        if (idx >= 0) {
          const next = [...prev]
          next[idx] = device
          return next
        }
        return [...prev, device]
      })
    })

    es.addEventListener('device_removed', (e: MessageEvent) => {
      const { serial, alias } = JSON.parse(e.data) as { serial: string; alias?: string }
      setDevices((prev) => prev.filter((d) => d.serial !== serial))
      if (alias) {
        setSlots((prev) => prev.filter((s) => s.device_alias !== alias))
      }
    })

    es.addEventListener('slot_updated', (e: MessageEvent) => {
      const slot = JSON.parse(e.data) as Slot
      setSlots((prev) => {
        const idx = prev.findIndex((s) => s.name === slot.name)
        if (idx >= 0) {
          const next = [...prev]
          next[idx] = slot
          return next
        }
        return [...prev, slot]
      })
    })

    es.addEventListener('slot_removed', (e: MessageEvent) => {
      const { name } = JSON.parse(e.data) as { name: string }
      setSlots((prev) => prev.filter((s) => s.name !== name))
    })

    es.addEventListener('log_entry', (e: MessageEvent) => {
      const entry = JSON.parse(e.data) as LogEntry
      setLogs((prev) => {
        const next = [...prev, entry]
        if (next.length > MAX_LOG_ENTRIES) {
          return next.slice(next.length - MAX_LOG_ENTRIES)
        }
        return next
      })
    })

    es.addEventListener('traffic_snapshot', (e: MessageEvent) => {
      const data = JSON.parse(e.data) as TrafficList
      setTraffic(data)
    })

    es.addEventListener('dns_stats', (e: MessageEvent) => {
      const data = JSON.parse(e.data) as DNSCacheStats
      setDnsStats(data)
    })

    es.addEventListener('system_stats', (e: MessageEvent) => {
      const data = JSON.parse(e.data) as SystemStats
      setSystemStats(data)
    })

    es.onopen = () => {
      setConnected(true)
      setError(null)
    }

    es.onerror = () => {
      setConnected(false)
      setError('Connection lost — reconnecting...')
      // EventSource auto-reconnects
    }

    return () => {
      es.close()
      esRef.current = null
    }
  }, [])

  return { devices, slots, logs, traffic, dnsStats, systemStats, connected, error }
}
