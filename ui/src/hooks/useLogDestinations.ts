import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiRequestError, get } from '../api/client'
import type { LogDestination, LogDestinationsResponse } from '../api/types'

const PATH = '/api/v1/settings/log-destinations'

// useLogDestinations reads the audit forwarding targets and their delivery
// health. Pass hasPermission('settings:read') as enabled so the hook never
// issues a request the caller is not entitled to make. While enabled it
// re-reads every pollMs so the page stays live; 0 turns polling off.
// loading is true only until the first response; refreshing covers every
// in-flight read.
export function useLogDestinations(enabled: boolean, pollMs = 30000) {
  const [destinations, setDestinations] = useState<LogDestination[] | null>(null)
  const [refreshing, setRefreshing] = useState(enabled)
  const [error, setError] = useState<string | null>(null)
  const [refreshedAt, setRefreshedAt] = useState<Date | null>(null)
  const loadSeq = useRef(0)

  const refresh = useCallback(async () => {
    const seq = ++loadSeq.current
    if (!enabled) {
      setDestinations(null)
      setError(null)
      setRefreshing(false)
      return
    }
    setRefreshing(true)
    try {
      const data = await get<LogDestinationsResponse>(PATH)
      if (seq !== loadSeq.current) return
      setDestinations(data?.destinations ?? [])
      setRefreshedAt(new Date())
      setError(null)
    } catch (err) {
      if (seq !== loadSeq.current) return
      if (err instanceof ApiRequestError) {
        setError(err.apiError.error)
      } else {
        setError(err instanceof Error ? err.message : 'Failed to load log destinations')
      }
    } finally {
      if (seq === loadSeq.current) setRefreshing(false)
    }
  }, [enabled])

  useEffect(() => { void refresh() }, [refresh])

  useEffect(() => {
    if (!enabled || pollMs <= 0) return
    const id = window.setInterval(() => { void refresh() }, pollMs)
    return () => window.clearInterval(id)
  }, [enabled, pollMs, refresh])

  return {
    destinations,
    loading: refreshing && destinations === null && error === null,
    refreshing,
    error,
    refreshedAt,
    refresh,
  }
}
