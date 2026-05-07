import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Shield, Lock, Key, Globe, AlertCircle, Loader2, Fingerprint, UserCog } from 'lucide-react'
import { useSecuritySettings } from '../hooks/useSettings'
import type { SecuritySettings } from '../hooks/useSettings'
import { useToast } from '../hooks/useToast'

const API_KEY_SCOPE_OPTIONS = [
  'pools:read', 'pools:write',
  'accounts:read', 'accounts:write',
  'keys:read', 'keys:write',
  'discovery:read', 'discovery:write',
  'audit:read',
  '*',
]

const API_KEY_ROLE_LABELS: Record<string, string> = {
  admin: 'Admin',
  operator: 'Operator',
  viewer: 'Viewer',
  auditor: 'Auditor',
}

const API_KEY_SCOPE_OPTIONS_BY_ROLE: Record<string, string[]> = {
  admin: API_KEY_SCOPE_OPTIONS,
  operator: ['pools:read', 'pools:write', 'accounts:read', 'accounts:write', 'discovery:read', 'discovery:write'],
  viewer: ['pools:read', 'accounts:read', 'discovery:read'],
  auditor: ['audit:read'],
}

export default function SecuritySettingsPage() {
  const { settings, loading, error, updateSettings } = useSecuritySettings()
  const { showToast } = useToast()
  const [form, setForm] = useState<SecuritySettings | null>(null)
  const [saving, setSaving] = useState(false)
  const [trustedProxiesText, setTrustedProxiesText] = useState('')

  useEffect(() => {
    if (settings) {
      setForm(settings)
      setTrustedProxiesText((settings.trusted_proxies ?? []).join('\n'))
    }
  }, [settings])

  async function handleSave() {
    if (!form) return
    setSaving(true)
    try {
      const proxies = trustedProxiesText
        .split('\n')
        .map(line => line.trim())
        .filter(Boolean)
      await updateSettings({ ...form, trusted_proxies: proxies })
      showToast('Security settings saved', 'success')
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to save settings', 'error')
    } finally {
      setSaving(false)
    }
  }

  function updateField<K extends keyof SecuritySettings>(key: K, value: SecuritySettings[K]) {
    setForm(prev => prev ? { ...prev, [key]: value } : prev)
  }

  function toggleAPIKeyScope(role: string, scope: string) {
    setForm(prev => {
      if (!prev) return prev
      const policy = { ...(prev.api_key_allowed_scopes_by_role ?? {}) }
      const current = policy[role] ?? []
      policy[role] = current.includes(scope)
        ? current.filter(s => s !== scope)
        : [...current, scope]
      return { ...prev, api_key_allowed_scopes_by_role: policy }
    })
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <Loader2 className="w-6 h-6 animate-spin text-gray-400" />
        <span className="ml-2 text-gray-500 dark:text-gray-400">Loading security settings...</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center gap-2 text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 px-4 py-3 rounded-lg">
        <AlertCircle className="w-4 h-4 flex-shrink-0" />
        {error}
      </div>
    )
  }

  if (!form) return null

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white flex items-center gap-2">
            <Shield className="w-6 h-6" />
            Security Policy
          </h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Session, password, login protection, and network trust settings.
          </p>
        </div>
        <button
          onClick={handleSave}
          disabled={saving}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
        >
          {saving && <Loader2 className="w-4 h-4 animate-spin" />}
          Save Settings
        </button>
      </div>

      <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 border dark:border-gray-700">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <UserCog className="w-5 h-5 text-indigo-500" />
              Identity Administration
            </h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
              Authentication providers, local users, and RBAC are managed from the dedicated Identity page.
            </p>
          </div>
          <Link
            to="/identity"
            className="px-4 py-2 rounded-lg bg-indigo-600 text-white text-sm font-medium hover:bg-indigo-700"
          >
            Open Identity
          </Link>
        </div>
      </div>

      <div className="space-y-6">
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 border dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2 mb-4">
            <Lock className="w-5 h-5 text-blue-500" />
            Session Management
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Session Duration (hours)
              </label>
              <input
                type="number"
                min={1}
                max={720}
                value={form.session_duration_hours}
                onChange={e => updateField('session_duration_hours', parseInt(e.target.value) || 1)}
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
              <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                How long a session cookie remains valid before requiring re-login
              </p>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Max Sessions per User
              </label>
              <input
                type="number"
                min={1}
                max={100}
                value={form.max_sessions_per_user}
                onChange={e => updateField('max_sessions_per_user', parseInt(e.target.value) || 1)}
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
              <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                Maximum concurrent sessions allowed per user account
              </p>
            </div>
          </div>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 border dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2 mb-4">
            <Key className="w-5 h-5 text-amber-500" />
            Password Policy
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Minimum Length
              </label>
              <input
                type="number"
                min={8}
                max={128}
                value={form.password_min_length}
                onChange={e => updateField('password_min_length', parseInt(e.target.value) || 8)}
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Maximum Length
              </label>
              <input
                type="number"
                min={8}
                max={72}
                value={form.password_max_length}
                onChange={e => updateField('password_max_length', Math.min(72, parseInt(e.target.value) || 72))}
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 border dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2 mb-4">
            <Key className="w-5 h-5 text-cyan-500" />
            API Key Policy
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-5">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Default Expiry (days)
              </label>
              <input
                type="number"
                min={0}
                max={3650}
                value={form.api_key_default_expiry_days}
                onChange={e => updateField('api_key_default_expiry_days', parseInt(e.target.value) || 0)}
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
              <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">0 keeps keys non-expiring by default</p>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Max Lifetime (days)
              </label>
              <input
                type="number"
                min={0}
                max={3650}
                value={form.api_key_max_lifetime_days}
                onChange={e => updateField('api_key_max_lifetime_days', parseInt(e.target.value) || 0)}
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
              <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">0 disables forced expiry</p>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Rotation Reminder (days)
              </label>
              <input
                type="number"
                min={0}
                max={365}
                value={form.api_key_rotation_reminder_days}
                onChange={e => updateField('api_key_rotation_reminder_days', parseInt(e.target.value) || 0)}
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
              <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">0 disables reminder audit events</p>
            </div>
          </div>

          <div className="space-y-4">
            {Object.entries(API_KEY_ROLE_LABELS).map(([role, label]) => (
              <div key={role}>
                <div className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{label} allowed scopes</div>
                <div className="flex flex-wrap gap-2">
                  {(API_KEY_SCOPE_OPTIONS_BY_ROLE[role] ?? []).map(scope => {
                    const selected = (form.api_key_allowed_scopes_by_role?.[role] ?? []).includes(scope)
                    return (
                      <button
                        key={`${role}-${scope}`}
                        type="button"
                        onClick={() => toggleAPIKeyScope(role, scope)}
                        className={`px-2.5 py-1 rounded text-xs font-medium border transition-colors ${
                          selected
                            ? 'bg-cyan-100 dark:bg-cyan-900/40 text-cyan-700 dark:text-cyan-300 border-cyan-300 dark:border-cyan-700'
                            : 'bg-gray-50 dark:bg-gray-700 text-gray-600 dark:text-gray-400 border-gray-200 dark:border-gray-600'
                        }`}
                      >
                        {scope}
                      </button>
                    )
                  })}
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 border dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2 mb-4">
            <Fingerprint className="w-5 h-5 text-red-500" />
            Login Protection
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Rate Limit (attempts/minute)
              </label>
              <input
                type="number"
                min={1}
                max={60}
                value={form.login_rate_limit_per_minute}
                onChange={e => updateField('login_rate_limit_per_minute', parseInt(e.target.value) || 5)}
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Account Lockout Threshold
              </label>
              <input
                type="number"
                min={0}
                max={100}
                value={form.account_lockout_attempts}
                onChange={e => updateField('account_lockout_attempts', parseInt(e.target.value) || 0)}
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
              <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                Set to 0 to disable account lockout
              </p>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Lockout Cooldown (minutes)
              </label>
              <input
                type="number"
                min={1}
                max={1440}
                value={form.account_lockout_cooldown_minutes}
                onChange={e => updateField('account_lockout_cooldown_minutes', parseInt(e.target.value) || 15)}
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 border dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2 mb-4">
            <Globe className="w-5 h-5 text-green-500" />
            Network
          </h2>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              Trusted Proxy CIDRs
            </label>
            <textarea
              rows={4}
              value={trustedProxiesText}
              onChange={e => setTrustedProxiesText(e.target.value)}
              placeholder="10.0.0.0/8&#10;172.16.0.0/12&#10;192.168.0.0/16"
              className="w-full px-3 py-2 border rounded-lg text-sm font-mono dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
            />
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              One CIDR per line. These proxies are trusted for X-Forwarded-For header parsing.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
