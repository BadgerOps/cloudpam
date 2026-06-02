import { useCallback, useState } from 'react'
import { get, post } from '../api/client'
import type {
  NetworkConflict,
  NetworkConflictActionResponse,
  NetworkConflictImportActionRequest,
  NetworkConflictLinkActionRequest,
  NetworkConflictListResponse,
  NetworkViewResponse,
} from '../api/types'

export interface NetworkFilters {
  account_id?: number
  provider?: string
  region?: string
  object_type?: string
  status?: string
  conflict_type?: string
  q?: string
}

function networkQuery(filters?: NetworkFilters) {
  const params = new URLSearchParams()
  if (filters?.account_id) params.set('account_id', String(filters.account_id))
  if (filters?.provider) params.set('provider', filters.provider)
  if (filters?.region) params.set('region', filters.region)
  if (filters?.object_type) params.set('object_type', filters.object_type)
  if (filters?.status) params.set('status', filters.status)
  if (filters?.conflict_type) params.set('conflict_type', filters.conflict_type)
  if (filters?.q) params.set('q', filters.q)
  const query = params.toString()
  return query ? `?${query}` : ''
}

export function useNetworkView() {
  const [flat, setFlat] = useState<NetworkViewResponse | null>(null)
  const [hierarchy, setHierarchy] = useState<NetworkViewResponse | null>(null)
  const [conflicts, setConflicts] = useState<NetworkConflictListResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetchFlat = useCallback(async (filters?: NetworkFilters) => {
    setLoading(true)
    setError(null)
    try {
      const resp = await get<NetworkViewResponse>(`/api/v1/network/flat${networkQuery(filters)}`)
      setFlat(resp)
      return resp
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to load network flat view'
      setError(msg)
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  const fetchHierarchy = useCallback(async (filters?: NetworkFilters) => {
    setLoading(true)
    setError(null)
    try {
      const resp = await get<NetworkViewResponse>(`/api/v1/network/hierarchy${networkQuery(filters)}`)
      setHierarchy(resp)
      return resp
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to load network hierarchy'
      setError(msg)
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  const fetchConflicts = useCallback(async (filters?: NetworkFilters) => {
    setLoading(true)
    setError(null)
    try {
      const resp = await get<NetworkConflictListResponse>(`/api/v1/network/conflicts${networkQuery(filters)}`)
      setConflicts(resp)
      return resp
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to load network conflicts'
      setError(msg)
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  const resolveConflict = useCallback(
    async (id: string, decision: string, reason?: string) => {
      return post<NetworkConflict>(`/api/v1/network/conflicts/${encodeURIComponent(id)}/resolve`, {
        decision,
        reason,
      })
    },
    [],
  )

  const linkConflict = useCallback(
    async (id: string, req: NetworkConflictLinkActionRequest) => {
      return post<NetworkConflictActionResponse>(
        `/api/v1/network/conflicts/${encodeURIComponent(id)}/actions/link`,
        req,
      )
    },
    [],
  )

  const importConflict = useCallback(
    async (id: string, req: NetworkConflictImportActionRequest) => {
      return post<NetworkConflictActionResponse>(
        `/api/v1/network/conflicts/${encodeURIComponent(id)}/actions/import`,
        req,
      )
    },
    [],
  )

  return {
    flat,
    hierarchy,
    conflicts,
    loading,
    error,
    fetchFlat,
    fetchHierarchy,
    fetchConflicts,
    resolveConflict,
    linkConflict,
    importConflict,
  }
}
