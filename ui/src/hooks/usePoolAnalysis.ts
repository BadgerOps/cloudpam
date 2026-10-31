import { useCallback, useEffect, useState } from 'react'
import { post } from '../api/client'
import type { FragmentationAnalysis, GapAnalysis } from '../api/types'
import { useLatestRequest } from './useLatestRequest'

interface PoolError {
  poolId: number
  message: string
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback
}

/**
 * Loads gap and fragmentation analysis for one pool. Both requests start
 * together and settle independently, so one failing never hides the other.
 * Responses for a pool that is no longer selected are discarded.
 */
export function usePoolAnalysis(poolId: number | null) {
  const [gaps, setGaps] = useState<GapAnalysis | null>(null)
  const [fragmentation, setFragmentation] = useState<FragmentationAnalysis | null>(null)
  const [gapsLoading, setGapsLoading] = useState(poolId !== null)
  const [fragmentationLoading, setFragmentationLoading] = useState(poolId !== null)
  // Errors carry the pool they belong to so that, like the results, an error
  // from the previous pool is never rendered for the current one.
  const [gapsError, setGapsError] = useState<PoolError | null>(null)
  const [fragmentationError, setFragmentationError] = useState<PoolError | null>(null)
  const { begin, isCurrent } = useLatestRequest()

  const load = useCallback((id: number | null) => {
    const token = begin()
    setGaps(null)
    setFragmentation(null)
    setGapsError(null)
    setFragmentationError(null)
    if (id === null) {
      setGapsLoading(false)
      setFragmentationLoading(false)
      return
    }
    setGapsLoading(true)
    setFragmentationLoading(true)

    // Start both requests before awaiting either.
    const gapsRequest = post<GapAnalysis>('/api/v1/analysis/gaps', { pool_id: id })
    const fragmentationRequest = post<FragmentationAnalysis>('/api/v1/analysis/fragmentation', { pool_ids: [id] })

    gapsRequest.then(
      result => {
        if (!isCurrent(token)) return
        setGaps(result)
        setGapsLoading(false)
      },
      (err: unknown) => {
        if (!isCurrent(token)) return
        setGapsError({ poolId: id, message: errorMessage(err, 'Failed to load gap analysis') })
        setGapsLoading(false)
      },
    )
    fragmentationRequest.then(
      result => {
        if (!isCurrent(token)) return
        setFragmentation(result)
        setFragmentationLoading(false)
      },
      (err: unknown) => {
        if (!isCurrent(token)) return
        setFragmentationError({ poolId: id, message: errorMessage(err, 'Failed to load fragmentation analysis') })
        setFragmentationLoading(false)
      },
    )
  }, [begin, isCurrent])

  useEffect(() => {
    load(poolId)
    // Drop in-flight responses once the pool changes or the panel unmounts.
    return () => { begin() }
  }, [poolId, load, begin])

  const retry = useCallback(() => {
    load(poolId)
  }, [poolId, load])

  // The clearing in load() runs in an effect, after the first render for a
  // new pool; filter here so that render never shows the previous pool.
  return {
    gaps: gaps?.pool_id === poolId ? gaps : null,
    fragmentation: fragmentation?.pool_id === poolId ? fragmentation : null,
    loading: gapsLoading || fragmentationLoading,
    gapsError: gapsError?.poolId === poolId ? gapsError.message : null,
    fragmentationError: fragmentationError?.poolId === poolId ? fragmentationError.message : null,
    retry,
  }
}
