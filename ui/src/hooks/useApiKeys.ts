import { useState, useEffect, useCallback } from 'react'
import { get, post, del } from '../api/client'
import type { ApiKeyInfo, ApiKeyCreateRequest, ApiKeyCreateResponse } from '../api/types'

interface ApiKeysListResponse {
  keys: ApiKeyInfo[]
}

export function useApiKeys() {
  const [keys, setKeys] = useState<ApiKeyInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
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
  }, [])

  useEffect(() => { refresh() }, [refresh])

  const create = useCallback(async (req: ApiKeyCreateRequest): Promise<ApiKeyCreateResponse> => {
    const res = await post<ApiKeyCreateResponse>('/api/v1/auth/keys', req)
    await refresh()
    return res
  }, [refresh])

  const revoke = useCallback(async (id: string) => {
    // Backend uses DELETE to revoke (soft delete)
    await del(`/api/v1/auth/keys/${id}`)
    await refresh()
  }, [refresh])

  return { keys, loading, error, create, revoke, refresh }
}
