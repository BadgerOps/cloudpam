import { useState, useCallback } from 'react'
import { get, post } from '../api/client'
import type {
  DriftListResponse,
  RunDriftDetectionResponse,
  DriftItem,
} from '../api/types'

export function useDrift() {
  const [data, setData] = useState<DriftListResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetch = useCallback(
    async (filters?: {
      account_id?: number
      type?: string
      severity?: string
      status?: string
      page?: number
      page_size?: number
    }) => {
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
        setData(resp)
      } catch (err) {
        setError(
          err instanceof Error ? err.message : 'Failed to load drift items',
        )
      } finally {
        setLoading(false)
      }
    },
    [],
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

  return { data, loading, error, fetch, detect, resolve, ignore }
}
