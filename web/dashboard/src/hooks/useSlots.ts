import { useState, useEffect, useCallback } from 'react'
import { listSlots } from '../api/slots'
import type { Slot } from '../api/types'

export function useSlots(intervalMs = 10000) {
  const [data, setData] = useState<Slot[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    try {
      const slots = await listSlots()
      setData(slots)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to fetch slots')
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
