import { createContext, useContext, useState, useEffect, useCallback } from 'react'
import type { HealthResponse, UserInfo, LoginResponse, MeResponse } from '../api/types'

// Non-sensitive display metadata persisted in localStorage for UX continuity.
const AUTH_NAME_KEY = 'cloudpam_key_name'
const AUTH_ROLE_KEY = 'cloudpam_role'
const AUTH_TYPE_KEY = 'cloudpam_auth_type'

export interface AuthContextValue {
  keyName: string | null
  role: string | null
  authType: 'session' | 'api_key' | null
  currentUser: UserInfo | null
  isAuthenticated: boolean
  authEnabled: boolean
  localAuthEnabled: boolean
  authChecked: boolean
  loginWithPassword: (username: string, password: string) => Promise<void>
  logout: () => void
}

export const AuthContext = createContext<AuthContextValue>({
  keyName: null,
  role: null,
  authType: null,
  currentUser: null,
  isAuthenticated: false,
  authEnabled: false,
  localAuthEnabled: false,
  authChecked: false,
  loginWithPassword: async () => {},
  logout: () => {},
})

export function useAuth() {
  return useContext(AuthContext)
}

export function useAuthState(): AuthContextValue {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [keyName, setKeyName] = useState<string | null>(() => localStorage.getItem(AUTH_NAME_KEY))
  const [role, setRole] = useState<string | null>(() => localStorage.getItem(AUTH_ROLE_KEY))
  const [authType, setAuthType] = useState<'session' | 'api_key' | null>(
    () => (localStorage.getItem(AUTH_TYPE_KEY) as 'session' | 'api_key') || null
  )
  const [currentUser, setCurrentUser] = useState<UserInfo | null>(null)
  const [authEnabled, setAuthEnabled] = useState(false)
  const [localAuthEnabled, setLocalAuthEnabled] = useState(false)
  const [authChecked, setAuthChecked] = useState(false)

  // Check health + validate existing session cookie on mount
  useEffect(() => {
    let cancelled = false

    async function init() {
      try {
        const healthRes = await fetch('/healthz')
        const health: HealthResponse = await healthRes.json()
        if (cancelled) return
        setAuthEnabled(health.auth_enabled === true)
        setLocalAuthEnabled(health.local_auth_enabled === true)

        // Check if there's a valid session cookie
        const meRes = await fetch('/api/v1/auth/me', {
          credentials: 'same-origin',
        })
        if (meRes.ok) {
          const me: MeResponse = await meRes.json()
          if (cancelled) return
          setIsAuthenticated(true)
          setRole(me.role)
          setAuthType(me.auth_type)
          localStorage.setItem(AUTH_ROLE_KEY, me.role)
          localStorage.setItem(AUTH_TYPE_KEY, me.auth_type)

          if (me.auth_type === 'session' && me.user) {
            setCurrentUser(me.user)
            setKeyName(me.user.display_name || me.user.username)
            localStorage.setItem(AUTH_NAME_KEY, me.user.display_name || me.user.username)
          }
        } else if (meRes.status === 401) {
          clearAuth()
        }
      } catch {
        // Network error — don't clear auth
      } finally {
        if (!cancelled) setAuthChecked(true)
      }
    }

    init()
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function clearAuth() {
    setIsAuthenticated(false)
    setKeyName(null)
    setRole(null)
    setAuthType(null)
    setCurrentUser(null)
    localStorage.removeItem(AUTH_NAME_KEY)
    localStorage.removeItem(AUTH_ROLE_KEY)
    localStorage.removeItem(AUTH_TYPE_KEY)
  }

  // Listen for forced logout from API client (401 responses)
  useEffect(() => {
    function handleLogout() { clearAuth() }
    window.addEventListener('auth:logout', handleLogout)
    return () => window.removeEventListener('auth:logout', handleLogout)
  }, [])

  const loginWithPassword = useCallback(async (username: string, password: string) => {
    const res = await fetch('/api/v1/auth/login', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    })

    if (!res.ok) {
      const body = await res.json().catch(() => ({ error: 'Login failed' }))
      throw new Error(body.error || 'Login failed')
    }

    const data: LoginResponse = await res.json()

    setIsAuthenticated(true)
    setCurrentUser(data.user)
    setRole(data.user.role)
    setKeyName(data.user.display_name || data.user.username)
    setAuthType('session')

    localStorage.setItem(AUTH_NAME_KEY, data.user.display_name || data.user.username)
    localStorage.setItem(AUTH_ROLE_KEY, data.user.role)
    localStorage.setItem(AUTH_TYPE_KEY, 'session')
  }, [])

  const logout = useCallback(async () => {
    try {
      await fetch('/api/v1/auth/logout', {
        method: 'POST',
        credentials: 'same-origin',
      })
    } catch {
      // Ignore errors
    }
    clearAuth()
  }, [])

  return {
    keyName,
    role,
    authType,
    currentUser,
    isAuthenticated,
    authEnabled,
    localAuthEnabled,
    authChecked,
    loginWithPassword,
    logout,
  }
}
