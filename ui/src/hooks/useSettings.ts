import { useState, useEffect, useCallback } from 'react'
import { get, patch } from '../api/client'
import type { NetworkSchemaPolicy } from '../api/types'
import { useAuth } from './useAuth'

export interface SecuritySettings {
  session_duration_hours: number
  max_sessions_per_user: number
  password_min_length: number
  password_max_length: number
  login_rate_limit_per_minute: number
  account_lockout_attempts: number
  account_lockout_cooldown_minutes: number
  trusted_proxies: string[]
  local_auth_enabled: boolean
  api_key_default_expiry_days: number
  api_key_max_lifetime_days: number
  api_key_rotation_reminder_days: number
  api_key_allowed_scopes_by_role: Record<string, string[]>
  audit_retention_days: number
}

export function useSecuritySettings() {
  const [settings, setSettings] = useState<SecuritySettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // GET /api/v1/settings/security requires settings:read and
  // PATCH requires settings:write — mirror both here so the UI never issues a
  // request it is not entitled to make.
  const { hasPermission } = useAuth()
  const canRead = hasPermission('settings:read')
  const canEdit = hasPermission('settings:write')

  const fetchSettings = useCallback(async () => {
    if (!canRead) {
      setSettings(null)
      setError(null)
      setLoading(false)
      return
    }
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
  }, [canRead])

  const updateSettings = useCallback(async (updated: SecuritySettings) => {
    if (!canEdit) {
      throw new Error('settings:write permission required to change security settings')
    }
    const data = await patch<SecuritySettings>('/api/v1/settings/security', updated)
    setSettings(data)
    return data
  }, [canEdit])

  useEffect(() => { fetchSettings() }, [fetchSettings])

  return { settings, loading, error, canRead, canEdit, updateSettings, refetch: fetchSettings }
}

const NETWORK_SCHEMA_POLICY_PATH = '/api/v1/settings/network-schema-policy'

// useNetworkSchemaPolicy loads and edits the stored network schema policy,
// the one the Discovery network views apply when a request names none.
// GET requires settings:read and PATCH requires settings:write.
export function useNetworkSchemaPolicy() {
  const [policy, setPolicy] = useState<NetworkSchemaPolicy | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const { hasPermission } = useAuth()
  const canRead = hasPermission('settings:read')
  const canEdit = hasPermission('settings:write')

  const fetchPolicy = useCallback(async () => {
    if (!canRead) {
      setPolicy(null)
      setError(null)
      setLoading(false)
      return
    }
    try {
      setLoading(true)
      const data = await get<NetworkSchemaPolicy>(NETWORK_SCHEMA_POLICY_PATH)
      setPolicy(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load network schema policy')
    } finally {
      setLoading(false)
    }
  }, [canRead])

  const updatePolicy = useCallback(async (update: Partial<NetworkSchemaPolicy>) => {
    if (!canEdit) {
      throw new Error('settings:write permission required to change the network schema policy')
    }
    const data = await patch<NetworkSchemaPolicy>(NETWORK_SCHEMA_POLICY_PATH, update)
    setPolicy(data)
    return data
  }, [canEdit])

  useEffect(() => { fetchPolicy() }, [fetchPolicy])

  return { policy, loading, error, canRead, canEdit, updatePolicy, refetch: fetchPolicy }
}
