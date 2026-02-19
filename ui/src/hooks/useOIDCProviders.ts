import { useState, useEffect, useCallback } from 'react'

interface OIDCProvider {
  id: string
  name: string
}

interface OIDCProvidersResponse {
  providers: OIDCProvider[]
}

export function useOIDCProviders() {
  const [providers, setProviders] = useState<OIDCProvider[]>([])
  const [loading, setLoading] = useState(true)

  const fetchProviders = useCallback(async () => {
    try {
      const response = await fetch('/api/v1/auth/oidc/providers')
      if (!response.ok) {
        setProviders([])
        return
      }
      const data: OIDCProvidersResponse = await response.json()
      setProviders(data.providers || [])
    } catch {
      // Silently fail — no providers means no SSO buttons
      setProviders([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchProviders() }, [fetchProviders])
  return { providers, loading }
}
