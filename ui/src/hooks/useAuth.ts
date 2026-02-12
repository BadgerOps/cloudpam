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
  login: (apiKey: string) => Promise<void>
  logout: () => void
}

export const AuthContext = createContext<AuthContextValue>({
  token: null,
  keyName: null,
  role: null,
  isAuthenticated: false,
  authEnabled: false,
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

  // Check if auth is enabled on mount
  useEffect(() => {
    fetch('/healthz')
      .then(r => r.json())
      .then((data: HealthResponse) => {
        setAuthEnabled(data.auth_enabled === true)
      })
      .catch(() => {
        // Default to false if health check fails
        setAuthEnabled(false)
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
    // Store the key and try to validate it
    localStorage.setItem(AUTH_STORAGE_KEY, apiKey)

    try {
      const res = await fetch('/api/v1/auth/keys', {
        headers: { Authorization: `Bearer ${apiKey}` },
      })
      if (!res.ok) {
        localStorage.removeItem(AUTH_STORAGE_KEY)
        throw new Error(res.status === 401 ? 'Invalid API key' : 'Authentication failed')
      }

      // Derive role from scopes in the response
      // The key itself doesn't expose its scopes, but if it can list keys, it's admin
      const name = apiKey.startsWith('cpam_') ? apiKey.substring(0, 12) + '...' : 'API Key'
      const detectedRole = 'admin' // If it can access /auth/keys, it has admin permissions

      setToken(apiKey)
      setKeyName(name)
      setRole(detectedRole)
      localStorage.setItem(AUTH_NAME_KEY, name)
      localStorage.setItem(AUTH_ROLE_KEY, detectedRole)
    } catch (err) {
      localStorage.removeItem(AUTH_STORAGE_KEY)
      setToken(null)
      throw err
    }
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
    login,
    logout,
  }
}
