import { useState, useEffect, useCallback } from 'react'
import { get, post, patch, del } from '../api/client'

export interface OIDCProvider {
  id: string
  name: string
  issuer_url: string
  client_id: string
  client_secret?: string
  scopes: string
  role_mapping: Record<string, string>
  default_role: string
  auto_provision: boolean
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface OIDCProviderCreate {
  name: string
  issuer_url: string
  client_id: string
  client_secret: string
  scopes?: string
  role_mapping?: Record<string, string>
  default_role?: string
  auto_provision?: boolean
  enabled?: boolean
}

export interface OIDCProviderUpdate {
  name?: string
  issuer_url?: string
  client_id?: string
  client_secret?: string
  scopes?: string
  role_mapping?: Record<string, string>
  default_role?: string
  auto_provision?: boolean
  enabled?: boolean
}

interface ProvidersResponse {
  providers: OIDCProvider[]
}

export interface TestResult {
  success: boolean
  message: string
}

export function useOIDCAdmin() {
  const [providers, setProviders] = useState<OIDCProvider[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchProviders = useCallback(async () => {
    try {
      setLoading(true)
      const data = await get<ProvidersResponse>('/api/v1/settings/oidc/providers')
      setProviders(data.providers || [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load OIDC providers')
      setProviders([])
    } finally {
      setLoading(false)
    }
  }, [])

  const createProvider = useCallback(async (provider: OIDCProviderCreate) => {
    const data = await post<OIDCProvider>('/api/v1/settings/oidc/providers', provider)
    setProviders(prev => [...prev, data])
    return data
  }, [])

  const updateProvider = useCallback(async (id: string, updates: OIDCProviderUpdate) => {
    const data = await patch<OIDCProvider>(`/api/v1/settings/oidc/providers/${id}`, updates)
    setProviders(prev => prev.map(p => p.id === id ? data : p))
    return data
  }, [])

  const deleteProvider = useCallback(async (id: string) => {
    await del(`/api/v1/settings/oidc/providers/${id}`)
    setProviders(prev => prev.filter(p => p.id !== id))
  }, [])

  const testProvider = useCallback(async (id: string) => {
    return await post<TestResult>(`/api/v1/settings/oidc/providers/${id}/test`, {})
  }, [])

  useEffect(() => { fetchProviders() }, [fetchProviders])

  return {
    providers,
    loading,
    error,
    createProvider,
    updateProvider,
    deleteProvider,
    testProvider,
    refetch: fetchProviders,
  }
}
