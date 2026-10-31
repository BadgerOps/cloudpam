import { useCallback, useEffect, useState } from 'react'
import { post } from '../api/client'
import type { FragmentationAnalysis, GapAnalysis } from '../api/types'
import { useLatestRequest } from './useLatestRequest'

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
  const [gapsError, setGapsError] = useState<string | null>(null)
  const [fragmentationError, setFragmentationError] = useState<string | null>(null)
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
        setGapsError(errorMessage(err, 'Failed to load gap analysis'))
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
        setFragmentationError(errorMessage(err, 'Failed to load fragmentation analysis'))
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

  return {
    gaps,
    fragmentation,
    loading: gapsLoading || fragmentationLoading,
    gapsError,
    fragmentationError,
    retry,
  }
}
