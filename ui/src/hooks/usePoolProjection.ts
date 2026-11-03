import { useCallback, useEffect, useState } from 'react'
import { get } from '../api/client'
import type { GrowthProjection, PoolStats } from '../api/types'
import { useLatestRequest } from './useLatestRequest'

interface PoolResult<T> {
  poolId: number
  value: T
}

/**
 * Loads the growth projection that GET /pools/{id}/stats attaches to a pool's
 * stats. Results and errors carry their pool id so a late response for the
 * previous pool is never shown for the current one.
 */
export function usePoolProjection(poolId: number | null) {
  const [projection, setProjection] = useState<PoolResult<GrowthProjection | null> | null>(null)
  const [error, setError] = useState<PoolResult<string> | null>(null)
  const [loading, setLoading] = useState(poolId !== null)
  const { begin, isCurrent } = useLatestRequest()

  const load = useCallback((id: number | null) => {
    const token = begin()
    setProjection(null)
    setError(null)
    if (id === null) {
      setLoading(false)
      return
    }
    setLoading(true)
    get<PoolStats>(`/api/v1/pools/${id}/stats`).then(
      stats => {
        if (!isCurrent(token)) return
        setProjection({ poolId: id, value: stats.projection ?? null })
        setLoading(false)
      },
      (err: unknown) => {
        if (!isCurrent(token)) return
        const message = err instanceof Error && err.message ? err.message : 'Failed to load growth projection'
        setError({ poolId: id, value: message })
        setLoading(false)
      },
    )
  }, [begin, isCurrent])

  useEffect(() => {
    load(poolId)
    return () => { begin() }
  }, [poolId, load, begin])

  const retry = useCallback(() => {
    load(poolId)
  }, [poolId, load])

  return {
    projection: projection?.poolId === poolId ? projection.value : null,
    error: error?.poolId === poolId ? error.value : null,
    loading,
    retry,
  }
}
