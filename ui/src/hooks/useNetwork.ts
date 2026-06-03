import { useCallback, useState } from 'react'
import { get, post } from '../api/client'
import type {
  NetworkConflict,
  NetworkConflictActionResponse,
  NetworkConflictImportActionRequest,
  NetworkConflictLinkActionRequest,
  NetworkConflictListResponse,
  NetworkConflictPlaceholderParentActionRequest,
  NetworkObjectListResponse,
  NetworkRelationship,
  NetworkRelationshipListResponse,
  NetworkViewResponse,
  ResolveNetworkRelationshipRequest,
} from '../api/types'

export interface NetworkFilters {
  account_id?: number
  provider?: string
  region?: string
  object_type?: string
  status?: string
  conflict_type?: string
  schema_policy?: string
  q?: string
}

export interface NetworkRelationshipFilters {
  type?: string
  source_kind?: string
  source_id?: string
  target_kind?: string
  target_id?: string
  resolution_state?: string
}

function networkQuery(filters?: NetworkFilters) {
  const params = new URLSearchParams()
  if (filters?.account_id) params.set('account_id', String(filters.account_id))
  if (filters?.provider) params.set('provider', filters.provider)
  if (filters?.region) params.set('region', filters.region)
  if (filters?.object_type) params.set('object_type', filters.object_type)
  if (filters?.status) params.set('status', filters.status)
  if (filters?.conflict_type) params.set('conflict_type', filters.conflict_type)
  if (filters?.schema_policy) params.set('schema_policy', filters.schema_policy)
  if (filters?.q) params.set('q', filters.q)
  const query = params.toString()
  return query ? `?${query}` : ''
}

function relationshipQuery(filters?: NetworkRelationshipFilters) {
  const params = new URLSearchParams()
  if (filters?.type) params.set('type', filters.type)
  if (filters?.source_kind) params.set('source_kind', filters.source_kind)
  if (filters?.source_id) params.set('source_id', filters.source_id)
  if (filters?.target_kind) params.set('target_kind', filters.target_kind)
  if (filters?.target_id) params.set('target_id', filters.target_id)
  if (filters?.resolution_state) params.set('resolution_state', filters.resolution_state)
  const query = params.toString()
  return query ? `?${query}` : ''
}

function objectQuery(filters?: NetworkFilters) {
  const params = new URLSearchParams()
  if (filters?.account_id) params.set('account_id', String(filters.account_id))
  if (filters?.provider) params.set('provider', filters.provider)
  if (filters?.region) params.set('region', filters.region)
  if (filters?.object_type) params.set('object_type', filters.object_type)
  if (filters?.status) params.set('state', filters.status)
  if (filters?.q) params.set('q', filters.q)
  const query = params.toString()
  return query ? `?${query}` : ''
}

export function useNetworkView() {
  const [flat, setFlat] = useState<NetworkViewResponse | null>(null)
  const [hierarchy, setHierarchy] = useState<NetworkViewResponse | null>(null)
  const [conflicts, setConflicts] = useState<NetworkConflictListResponse | null>(null)
  const [objects, setObjects] = useState<NetworkObjectListResponse | null>(null)
  const [relationships, setRelationships] = useState<NetworkRelationshipListResponse | null>(null)
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

  const fetchObjects = useCallback(async (filters?: NetworkFilters) => {
    setLoading(true)
    setError(null)
    try {
      const resp = await get<NetworkObjectListResponse>(`/api/v1/network/objects${objectQuery(filters)}`)
      setObjects(resp)
      return resp
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to load network objects'
      setError(msg)
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  const fetchRelationships = useCallback(async (filters?: NetworkRelationshipFilters) => {
    setLoading(true)
    setError(null)
    try {
      const resp = await get<NetworkRelationshipListResponse>(`/api/v1/network/relationships${relationshipQuery(filters)}`)
      setRelationships(resp)
      return resp
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to load network relationships'
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

  const createPlaceholderParentConflict = useCallback(
    async (id: string, req: NetworkConflictPlaceholderParentActionRequest) => {
      return post<NetworkConflictActionResponse>(
        `/api/v1/network/conflicts/${encodeURIComponent(id)}/actions/create_placeholder_parent`,
        req,
      )
    },
    [],
  )

  const resolveNetworkRelationship = useCallback(
    async (req: ResolveNetworkRelationshipRequest) => {
      return post<NetworkRelationship>('/api/v1/network/relationships/resolve', req)
    },
    [],
  )

  return {
    flat,
    hierarchy,
    conflicts,
    objects,
    relationships,
    loading,
    error,
    fetchFlat,
    fetchHierarchy,
    fetchConflicts,
    fetchObjects,
    fetchRelationships,
    resolveConflict,
    linkConflict,
    importConflict,
    createPlaceholderParentConflict,
    resolveNetworkRelationship,
  }
}
