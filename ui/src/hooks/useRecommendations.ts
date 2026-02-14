import { useState, useCallback } from 'react'
import { get, post } from '../api/client'
import type {
  RecommendationsListResponse,
  GenerateRecommendationsResponse,
  Recommendation,
} from '../api/types'

export function useRecommendations() {
  const [data, setData] = useState<RecommendationsListResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetch = useCallback(
    async (filters?: {
      pool_id?: number
      type?: string
      status?: string
      priority?: string
      page?: number
      page_size?: number
    }) => {
      setLoading(true)
      setError(null)
      try {
        const params = new URLSearchParams()
        if (filters?.pool_id) params.set('pool_id', String(filters.pool_id))
        if (filters?.type) params.set('type', filters.type)
        if (filters?.status) params.set('status', filters.status)
        if (filters?.priority) params.set('priority', filters.priority)
        if (filters?.page) params.set('page', String(filters.page))
        if (filters?.page_size)
          params.set('page_size', String(filters.page_size))

        const qs = params.toString()
        const resp = await get<RecommendationsListResponse>(
          `/api/v1/recommendations${qs ? '?' + qs : ''}`,
        )
        setData(resp)
      } catch (err) {
        setError(
          err instanceof Error ? err.message : 'Failed to load recommendations',
        )
      } finally {
        setLoading(false)
      }
    },
    [],
  )

  const generate = useCallback(
    async (
      poolIds: number[],
      includeChildren: boolean,
      desiredPrefixLen?: number,
    ) => {
      const body: Record<string, unknown> = {
        pool_ids: poolIds,
        include_children: includeChildren,
      }
      if (desiredPrefixLen) body.desired_prefix_len = desiredPrefixLen
      const resp = await post<GenerateRecommendationsResponse>(
        '/api/v1/recommendations/generate',
        body,
      )
      return resp
    },
    [],
  )

  const apply = useCallback(
    async (id: string, name?: string, accountId?: number) => {
      const body: Record<string, unknown> = {}
      if (name) body.name = name
      if (accountId) body.account_id = accountId
      const resp = await post<Recommendation>(
        `/api/v1/recommendations/${id}/apply`,
        body,
      )
      return resp
    },
    [],
  )

  const dismiss = useCallback(async (id: string, reason?: string) => {
    const resp = await post<Recommendation>(
      `/api/v1/recommendations/${id}/dismiss`,
      { reason: reason || '' },
    )
    return resp
  }, [])

  return { data, loading, error, fetch, generate, apply, dismiss }
}
