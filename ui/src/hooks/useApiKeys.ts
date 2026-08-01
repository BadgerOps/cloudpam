import { useState, useEffect, useCallback } from 'react'
import { get, post, del } from '../api/client'
import { useAuth } from './useAuth'
import type { ApiKeyInfo, ApiKeyCreateRequest, ApiKeyCreateResponse } from '../api/types'

interface ApiKeysListResponse {
  keys: ApiKeyInfo[]
}

export function useApiKeys() {
  const [keys, setKeys] = useState<ApiKeyInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // internal/api/auth_handlers.go guards each verb separately: GET needs
  // apikeys:list or apikeys:read, POST needs apikeys:create and DELETE needs
  // apikeys:delete. Reaching the configuration area (settings:read) grants none
  // of those, so mirror the server here instead of firing requests that 403.
  const { hasPermission } = useAuth()
  const canList = hasPermission('apikeys:list') || hasPermission('apikeys:read')
  const canCreate = hasPermission('apikeys:create')
  const canRevoke = hasPermission('apikeys:delete')

  const refresh = useCallback(async () => {
    if (!canList) {
      setKeys([])
      setError(null)
      setLoading(false)
      return
    }
    setLoading(true)
    setError(null)
    try {
      const data = await get<ApiKeysListResponse>('/api/v1/auth/keys')
      setKeys(data.keys ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load API keys')
    } finally {
      setLoading(false)
    }
  }, [canList])

  useEffect(() => { refresh() }, [refresh])

  const create = useCallback(async (req: ApiKeyCreateRequest): Promise<ApiKeyCreateResponse> => {
    if (!canCreate) throw new Error('apikeys:create permission required to create API keys')
    const res = await post<ApiKeyCreateResponse>('/api/v1/auth/keys', req)
    await refresh()
    return res
  }, [canCreate, refresh])

  const revoke = useCallback(async (id: string) => {
    if (!canRevoke) throw new Error('apikeys:delete permission required to revoke API keys')
    // Backend uses DELETE to revoke (soft delete)
    await del(`/api/v1/auth/keys/${id}`)
    await refresh()
  }, [canRevoke, refresh])

  return { keys, loading, error, canList, canCreate, canRevoke, create, revoke, refresh }
}
