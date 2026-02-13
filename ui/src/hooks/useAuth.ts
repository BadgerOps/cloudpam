import { createContext, useContext, useState, useEffect, useCallback } from 'react'
import type { HealthResponse, UserInfo, LoginResponse, MeResponse } from '../api/types'

const AUTH_STORAGE_KEY = 'cloudpam_api_key'
const AUTH_NAME_KEY = 'cloudpam_key_name'
const AUTH_ROLE_KEY = 'cloudpam_role'
const AUTH_TYPE_KEY = 'cloudpam_auth_type'

export interface AuthContextValue {
  token: string | null
  keyName: string | null
  role: string | null
  authType: 'session' | 'api_key' | null
  currentUser: UserInfo | null
  isAuthenticated: boolean
  authEnabled: boolean
  localAuthEnabled: boolean
  authChecked: boolean
  loginWithPassword: (username: string, password: string) => Promise<void>
  loginWithApiKey: (apiKey: string) => Promise<void>
  logout: () => void
}

export const AuthContext = createContext<AuthContextValue>({
  token: null,
  keyName: null,
  role: null,
  authType: null,
  currentUser: null,
  isAuthenticated: false,
  authEnabled: false,
  localAuthEnabled: false,
  authChecked: false,
  loginWithPassword: async () => {},
  loginWithApiKey: async () => {},
  logout: () => {},
})

export function useAuth() {
  return useContext(AuthContext)
}

export function useAuthState(): AuthContextValue {
  const [token, setToken] = useState<string | null>(() => sessionStorage.getItem(AUTH_STORAGE_KEY))
  const [keyName, setKeyName] = useState<string | null>(() => localStorage.getItem(AUTH_NAME_KEY))
  const [role, setRole] = useState<string | null>(() => localStorage.getItem(AUTH_ROLE_KEY))
  const [authType, setAuthType] = useState<'session' | 'api_key' | null>(
    () => (localStorage.getItem(AUTH_TYPE_KEY) as 'session' | 'api_key') || null
  )
  const [currentUser, setCurrentUser] = useState<UserInfo | null>(null)
  const [authEnabled, setAuthEnabled] = useState(false)
  const [localAuthEnabled, setLocalAuthEnabled] = useState(false)
  const [authChecked, setAuthChecked] = useState(false)

  // Check health + validate existing session on mount
  useEffect(() => {
    let cancelled = false

    async function init() {
      try {
        const healthRes = await fetch('/healthz')
        const health: HealthResponse = await healthRes.json()
        if (cancelled) return
        setAuthEnabled(health.auth_enabled === true)
        setLocalAuthEnabled(health.local_auth_enabled === true)

        // If we have an existing session (cookie) or API key, validate it
        const storedToken = sessionStorage.getItem(AUTH_STORAGE_KEY)
        const headers: Record<string, string> = {}
        if (storedToken && storedToken !== '__session__') {
          headers['Authorization'] = `Bearer ${storedToken}`
        }

        const meRes = await fetch('/api/v1/auth/me', {
          credentials: 'same-origin',
          headers,
        })
        if (meRes.ok) {
          const me: MeResponse = await meRes.json()
          if (cancelled) return
          setRole(me.role)
          setAuthType(me.auth_type)
          localStorage.setItem(AUTH_ROLE_KEY, me.role)
          localStorage.setItem(AUTH_TYPE_KEY, me.auth_type)

          if (me.auth_type === 'session' && me.user) {
            setCurrentUser(me.user)
            setKeyName(me.user.display_name || me.user.username)
            localStorage.setItem(AUTH_NAME_KEY, me.user.display_name || me.user.username)
            if (!storedToken) {
              setToken('__session__')
              sessionStorage.setItem(AUTH_STORAGE_KEY, '__session__')
            }
          } else if (me.auth_type === 'api_key') {
            setKeyName(me.key_name || localStorage.getItem(AUTH_NAME_KEY))
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
    setToken(null)
    setKeyName(null)
    setRole(null)
    setAuthType(null)
    setCurrentUser(null)
    sessionStorage.removeItem(AUTH_STORAGE_KEY)
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

    setCurrentUser(data.user)
    setRole(data.user.role)
    setKeyName(data.user.display_name || data.user.username)
    setAuthType('session')
    setToken('__session__')

    sessionStorage.setItem(AUTH_STORAGE_KEY, '__session__')
    localStorage.setItem(AUTH_NAME_KEY, data.user.display_name || data.user.username)
    localStorage.setItem(AUTH_ROLE_KEY, data.user.role)
    localStorage.setItem(AUTH_TYPE_KEY, 'session')
  }, [])

  const loginWithApiKey = useCallback(async (apiKey: string) => {
    const res = await fetch('/api/v1/auth/me', {
      headers: { Authorization: `Bearer ${apiKey}` },
    })

    if (res.status === 401 || res.status === 403) {
      throw new Error('Invalid API key')
    }
    if (!res.ok) {
      throw new Error('Authentication failed')
    }

    const me: MeResponse = await res.json()

    // Store a non-sensitive marker instead of the raw API key.
    setToken('__api_key__')
    setKeyName(me.key_name || apiKey.substring(0, 12) + '...')
    setRole(me.role)
    setAuthType('api_key')
    setCurrentUser(null)

    // Avoid persisting the API key itself in sessionStorage.
    sessionStorage.setItem(AUTH_STORAGE_KEY, '__api_key__')
    localStorage.setItem(AUTH_NAME_KEY, me.key_name || apiKey.substring(0, 12) + '...')
    localStorage.setItem(AUTH_ROLE_KEY, me.role)
    localStorage.setItem(AUTH_TYPE_KEY, 'api_key')
  }, [])

  const logout = useCallback(async () => {
    if (authType === 'session') {
      try {
        await fetch('/api/v1/auth/logout', {
          method: 'POST',
          credentials: 'same-origin',
        })
      } catch {
        // Ignore errors
      }
    }
    clearAuth()
  }, [authType])

  return {
    token,
    keyName,
    role,
    authType,
    currentUser,
    isAuthenticated: token !== null,
    authEnabled,
    localAuthEnabled,
    authChecked,
    loginWithPassword,
    loginWithApiKey,
    logout,
  }
}
