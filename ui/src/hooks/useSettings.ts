import { useState, useEffect, useCallback } from 'react'
import { get, patch } from '../api/client'

export interface SecuritySettings {
  session_duration_hours: number
  max_sessions_per_user: number
  password_min_length: number
  password_max_length: number
  login_rate_limit_per_minute: number
  account_lockout_attempts: number
  trusted_proxies: string[]
  local_auth_enabled: boolean
}

export function useSecuritySettings() {
  const [settings, setSettings] = useState<SecuritySettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchSettings = useCallback(async () => {
    try {
      setLoading(true)
      const data = await get<SecuritySettings>('/api/v1/settings/security')
      setSettings(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load security settings')
    } finally {
      setLoading(false)
    }
  }, [])

  const updateSettings = useCallback(async (updated: SecuritySettings) => {
    const data = await patch<SecuritySettings>('/api/v1/settings/security', updated)
    setSettings(data)
    return data
  }, [])

  useEffect(() => { fetchSettings() }, [fetchSettings])

  return { settings, loading, error, updateSettings, refetch: fetchSettings }
}
