import { useState, useEffect, useRef, useCallback } from 'react'

interface UsePollingOptions {
  intervalMs: number
  enabled?: boolean
}

interface UsePollingResult<T> {
  data: T | null
  loading: boolean
  error: string | null
  refresh: () => void
}

export function usePolling<T>(
  fetchFn: () => Promise<T>,
  options: UsePollingOptions
): UsePollingResult<T> {
  const { intervalMs, enabled = true } = options
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const fetchRef = useRef(fetchFn)
  fetchRef.current = fetchFn

  const doFetch = useCallback(async () => {
    try {
      const result = await fetchRef.current()
      setData(result)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!enabled) return

    doFetch()
    const id = setInterval(doFetch, intervalMs)
    return () => clearInterval(id)
  }, [doFetch, intervalMs, enabled])

  return { data, loading, error, refresh: doFetch }
}
