import { createContext, useContext, useState, useEffect, useCallback } from 'react'
import type { HealthResponse } from '../api/types'

const AUTH_STORAGE_KEY = 'cloudpam_api_key'
const AUTH_NAME_KEY = 'cloudpam_key_name'
const AUTH_ROLE_KEY = 'cloudpam_role'

export interface AuthContextValue {
  token: string | null
  keyName: string | null
  role: string | null
  isAuthenticated: boolean
  authEnabled: boolean
  authChecked: boolean
  login: (apiKey: string) => Promise<void>
  logout: () => void
}

export const AuthContext = createContext<AuthContextValue>({
  token: null,
  keyName: null,
  role: null,
  isAuthenticated: false,
  authEnabled: false,
  authChecked: false,
  login: async () => {},
  logout: () => {},
})

export function useAuth() {
  return useContext(AuthContext)
}

export function useAuthState(): AuthContextValue {
  const [token, setToken] = useState<string | null>(() => localStorage.getItem(AUTH_STORAGE_KEY))
  const [keyName, setKeyName] = useState<string | null>(() => localStorage.getItem(AUTH_NAME_KEY))
  const [role, setRole] = useState<string | null>(() => localStorage.getItem(AUTH_ROLE_KEY))
  const [authEnabled, setAuthEnabled] = useState(false)
  const [authChecked, setAuthChecked] = useState(false)

  // Check if auth is enabled on mount
  useEffect(() => {
    fetch('/healthz')
      .then(r => r.json())
      .then((data: HealthResponse) => {
        setAuthEnabled(data.auth_enabled === true)
      })
      .catch(() => {
        setAuthEnabled(false)
      })
      .finally(() => {
        setAuthChecked(true)
      })
  }, [])

  // Listen for forced logout from API client (401 responses)
  useEffect(() => {
    function handleLogout() {
      setToken(null)
      setKeyName(null)
      setRole(null)
      localStorage.removeItem(AUTH_STORAGE_KEY)
      localStorage.removeItem(AUTH_NAME_KEY)
      localStorage.removeItem(AUTH_ROLE_KEY)
    }
    window.addEventListener('auth:logout', handleLogout)
    return () => window.removeEventListener('auth:logout', handleLogout)
  }, [])

  const login = useCallback(async (apiKey: string) => {
    // Validate the key by making an authenticated request.
    // Use /api/v1/pools (lightweight read) — if auth is enabled and key is bad, we get 401.
    const res = await fetch('/api/v1/pools', {
      headers: { Authorization: `Bearer ${apiKey}` },
    })

    if (res.status === 401 || res.status === 403) {
      throw new Error('Invalid API key')
    }
    if (!res.ok) {
      throw new Error('Authentication failed')
    }

    // Key is valid — store it
    const displayName = apiKey.startsWith('cpam_') ? apiKey.substring(0, 12) + '...' : 'API Key'

    // Try to determine role by attempting to list auth keys (admin-only when RBAC is on)
    let detectedRole = 'viewer'
    try {
      const keysRes = await fetch('/api/v1/auth/keys', {
        headers: { Authorization: `Bearer ${apiKey}` },
      })
      if (keysRes.ok) {
        detectedRole = 'admin'
      }
    } catch {
      // If the request fails, assume non-admin
    }

    setToken(apiKey)
    setKeyName(displayName)
    setRole(detectedRole)
    localStorage.setItem(AUTH_STORAGE_KEY, apiKey)
    localStorage.setItem(AUTH_NAME_KEY, displayName)
    localStorage.setItem(AUTH_ROLE_KEY, detectedRole)
  }, [])

  const logout = useCallback(() => {
    setToken(null)
    setKeyName(null)
    setRole(null)
    localStorage.removeItem(AUTH_STORAGE_KEY)
    localStorage.removeItem(AUTH_NAME_KEY)
    localStorage.removeItem(AUTH_ROLE_KEY)
  }, [])

  return {
    token,
    keyName,
    role,
    isAuthenticated: token !== null,
    authEnabled,
    authChecked,
    login,
    logout,
  }
}
