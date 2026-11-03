import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Shield, Lock, Key, Globe, AlertCircle, Loader2, Fingerprint, UserCog, ScrollText } from 'lucide-react'
import { useSecuritySettings } from '../hooks/useSettings'
import type { SecuritySettings } from '../hooks/useSettings'
import { useToast } from '../hooks/useToast'
import { useAuth } from '../hooks/useAuth'
import { Field } from '../components/primitives'

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

type NumberKey = {
  [K in keyof SecuritySettings]: SecuritySettings[K] extends number ? K : never
}[keyof SecuritySettings]

interface NumberSpec {
  label: string
  min: number
  max: number
  fallback: number
  hint?: string
}

const NUMBER_FIELDS: Record<NumberKey, NumberSpec> = {
  session_duration_hours: {
    label: 'Session Duration (hours)', min: 1, max: 720, fallback: 1,
    hint: 'How long a session cookie remains valid before requiring re-login',
  },
  max_sessions_per_user: {
    label: 'Max Sessions per User', min: 1, max: 100, fallback: 1,
    hint: 'Maximum concurrent sessions allowed per user account',
  },
  password_min_length: { label: 'Minimum Length', min: 8, max: 128, fallback: 8 },
  password_max_length: { label: 'Maximum Length', min: 8, max: 72, fallback: 72 },
  api_key_default_expiry_days: {
    label: 'Default Expiry (days)', min: 0, max: 3650, fallback: 0,
    hint: '0 keeps keys non-expiring by default',
  },
  api_key_max_lifetime_days: {
    label: 'Max Lifetime (days)', min: 0, max: 3650, fallback: 0,
    hint: '0 disables forced expiry',
  },
  api_key_rotation_reminder_days: {
    label: 'Rotation Reminder (days)', min: 0, max: 365, fallback: 0,
    hint: '0 disables reminder audit events',
  },
  login_rate_limit_per_minute: { label: 'Rate Limit (attempts/minute)', min: 1, max: 60, fallback: 5 },
  account_lockout_attempts: {
    label: 'Account Lockout Threshold', min: 0, max: 100, fallback: 0,
    hint: 'Set to 0 to disable account lockout',
  },
  account_lockout_cooldown_minutes: { label: 'Lockout Cooldown (minutes)', min: 1, max: 1440, fallback: 15 },
  audit_retention_days: {
    label: 'Retention (days)', min: 0, max: 3650, fallback: 0,
    hint: 'Events older than this are purged hourly. 0 keeps events forever. Export the log from the Audit page first if you need a copy.',
  },
}

const INPUT_CLASS = 'w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500'
const CARD_CLASS = 'bg-white dark:bg-gray-800 rounded-lg shadow p-6 border dark:border-gray-700'
const CARD_TITLE_CLASS = 'text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2 mb-4'

export default function SecuritySettingsPage() {
  const { settings, loading, error, updateSettings } = useSecuritySettings()
  const { showToast } = useToast()
  // PATCH /api/v1/settings/security requires settings:write; settings:read only
  // grants the GET used to render this page.
  const { hasPermission } = useAuth()
  const canEdit = hasPermission('settings:write')
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
    if (!form || !canEdit) return
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

  function numberField(key: NumberKey) {
    const spec = NUMBER_FIELDS[key]
    return (
      <Field label={spec.label} hint={spec.hint}>
        <input
          type="number"
          min={spec.min}
          max={spec.max}
          value={form?.[key] ?? spec.fallback}
          onChange={e => updateField(key, Math.min(spec.max, parseInt(e.target.value) || spec.fallback))}
          className={INPUT_CLASS}
        />
      </Field>
    )
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
        {canEdit ? (
          <button
            onClick={handleSave}
            disabled={saving}
            className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
          >
            {saving && <Loader2 className="w-4 h-4 animate-spin" />}
            Save Settings
          </button>
        ) : (
          <span className="text-sm text-gray-500 dark:text-gray-400">
            Read-only — settings:write required to change security policy
          </span>
        )}
      </div>

      <div className={CARD_CLASS}>
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
        <div className={CARD_CLASS}>
          <h2 className={CARD_TITLE_CLASS}>
            <Lock className="w-5 h-5 text-blue-500" />
            Session Management
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {numberField('session_duration_hours')}
            {numberField('max_sessions_per_user')}
          </div>
        </div>

        <div className={CARD_CLASS}>
          <h2 className={CARD_TITLE_CLASS}>
            <Key className="w-5 h-5 text-amber-500" />
            Password Policy
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {numberField('password_min_length')}
            {numberField('password_max_length')}
          </div>
        </div>

        <div className={CARD_CLASS}>
          <h2 className={CARD_TITLE_CLASS}>
            <Key className="w-5 h-5 text-cyan-500" />
            API Key Policy
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-5">
            {numberField('api_key_default_expiry_days')}
            {numberField('api_key_max_lifetime_days')}
            {numberField('api_key_rotation_reminder_days')}
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

        <div className={CARD_CLASS}>
          <h2 className={CARD_TITLE_CLASS}>
            <ScrollText className="w-5 h-5 text-amber-500" />
            Audit Log
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {numberField('audit_retention_days')}
          </div>
        </div>

        <div className={CARD_CLASS}>
          <h2 className={CARD_TITLE_CLASS}>
            <Fingerprint className="w-5 h-5 text-red-500" />
            Login Protection
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {numberField('login_rate_limit_per_minute')}
            {numberField('account_lockout_attempts')}
            {numberField('account_lockout_cooldown_minutes')}
          </div>
        </div>

        <div className={CARD_CLASS}>
          <h2 className={CARD_TITLE_CLASS}>
            <Globe className="w-5 h-5 text-green-500" />
            Network
          </h2>
          <Field
            label="Trusted Proxy CIDRs"
            hint="One CIDR per line. These proxies are trusted for X-Forwarded-For header parsing."
          >
            <textarea
              rows={4}
              value={trustedProxiesText}
              onChange={e => setTrustedProxiesText(e.target.value)}
              placeholder="10.0.0.0/8&#10;172.16.0.0/12&#10;192.168.0.0/16"
              className={`${INPUT_CLASS} font-mono`}
            />
          </Field>
        </div>
      </div>
    </div>
  )
}
