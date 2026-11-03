import { useState, useCallback } from 'react'
import { get, patch, post } from '../api/client'
import { useLatestRequest } from './useLatestRequest'
import type {
  DiscoveryImportApplyResponse,
  DriftListResponse,
  RunDriftDetectionResponse,
  DriftItem,
} from '../api/types'

export function useDrift() {
  const [data, setData] = useState<DriftListResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { begin, isCurrent } = useLatestRequest()

  const fetch = useCallback(
    async (filters?: {
      account_id?: number
      type?: string
      severity?: string
      status?: string
      page?: number
      page_size?: number
    }) => {
      const token = begin()
      setLoading(true)
      setError(null)
      try {
        const params = new URLSearchParams()
        if (filters?.account_id) params.set('account_id', String(filters.account_id))
        if (filters?.type) params.set('type', filters.type)
        if (filters?.severity) params.set('severity', filters.severity)
        if (filters?.status) params.set('status', filters.status)
        if (filters?.page) params.set('page', String(filters.page))
        if (filters?.page_size) params.set('page_size', String(filters.page_size))

        const qs = params.toString()
        const resp = await get<DriftListResponse>(
          `/api/v1/drift${qs ? '?' + qs : ''}`,
        )
        if (!isCurrent(token)) return
        setData(resp)
      } catch (err) {
        if (!isCurrent(token)) return
        setError(
          err instanceof Error ? err.message : 'Failed to load drift items',
        )
      } finally {
        if (isCurrent(token)) setLoading(false)
      }
    },
    [begin, isCurrent],
  )

  const detect = useCallback(
    async (accountIds?: number[]) => {
      const body: Record<string, unknown> = {}
      if (accountIds && accountIds.length > 0) body.account_ids = accountIds
      const resp = await post<RunDriftDetectionResponse>(
        '/api/v1/drift/detect',
        body,
      )
      return resp
    },
    [],
  )

  const resolve = useCallback(async (id: string) => {
    const resp = await post<DriftItem>(
      `/api/v1/drift/${id}/resolve`,
      {},
    )
    return resp
  }, [])

  const ignore = useCallback(async (id: string, reason?: string) => {
    const resp = await post<DriftItem>(
      `/api/v1/drift/${id}/ignore`,
      { reason: reason || '' },
    )
    return resp
  }, [])

  // applySuggestion runs the item's suggested action through the existing
  // discovery or pool endpoint, then resolves the item.
  const applySuggestion = useCallback(async (item: DriftItem) => {
    const { suggested_action: action, suggested_pool_id: poolId, resource_id: resourceId } = item
    if (action === 'link_pool' && resourceId && poolId) {
      await post(`/api/v1/discovery/resources/${resourceId}/link`, { pool_id: poolId })
    } else if (action === 'create_pool' && resourceId) {
      const resp = await post<DiscoveryImportApplyResponse>('/api/v1/discovery/import/apply', {
        account_id: item.account_id,
        resource_ids: [resourceId],
      })
      if (resp.pools_created + resp.resources_linked === 0) {
        const issues = resp.preview.items.flatMap((i) => [...i.issues, ...(i.evidence ?? [])])
        throw new Error(`Import skipped: ${[...resp.errors, ...issues].join('; ') || 'no change made'}`)
      }
    } else if (action === 'deprecate_pool' && poolId) {
      await patch(`/api/v1/pools/${poolId}`, { status: 'deprecated' })
    } else {
      throw new Error('This drift item has no suggestion to apply')
    }
    return post<DriftItem>(`/api/v1/drift/${item.id}/resolve`, {})
  }, [])

  return { data, loading, error, fetch, detect, resolve, ignore, applySuggestion }
}
