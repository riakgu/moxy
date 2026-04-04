import { useState, useEffect, useCallback } from 'react'
import { listDevices } from '../api/devices'
import type { Device } from '../api/types'

export function useDevices(intervalMs = 10000) {
  const [data, setData] = useState<Device[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

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
    refetch()
    const id = setInterval(refetch, intervalMs)
    return () => clearInterval(id)
  }, [refetch, intervalMs])

  return { data, loading, error, refetch }
}
