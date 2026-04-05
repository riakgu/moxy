import { useState, useEffect, useCallback, useRef } from 'react'
import { listDevices, scanDevices } from '../api/devices'
import type { Device } from '../api/types'

export function useDevices(intervalMs = 10000) {
  const [data, setData] = useState<Device[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const hasScanned = useRef(false)

  const refetch = useCallback(async () => {
    try {
      const devices = await listDevices()
      setData(devices)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to fetch devices')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    // Auto-scan on first page load to detect connected devices
    const init = async () => {
      if (!hasScanned.current) {
        hasScanned.current = true
        try {
          await scanDevices()
        } catch {
          // Scan failure is non-critical — just show whatever devices exist
        }
      }
      await refetch()
    }
    init()
    const id = setInterval(refetch, intervalMs)
    return () => clearInterval(id)
  }, [refetch, intervalMs])

  return { data, loading, error, refetch }
}
