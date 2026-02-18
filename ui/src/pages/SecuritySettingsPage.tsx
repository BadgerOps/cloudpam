import { useState, useEffect } from 'react'
import { Shield, Lock, Key, Globe, AlertCircle, Loader2, Users, Fingerprint } from 'lucide-react'
import { useSecuritySettings } from '../hooks/useSettings'
import type { SecuritySettings } from '../hooks/useSettings'
import { useToast } from '../hooks/useToast'

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
        .map(l => l.trim())
        .filter(l => l.length > 0)
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
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white flex items-center gap-2">
            <Shield className="w-6 h-6" />
            Security Settings
          </h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Configure authentication, session, and password policies
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

      <div className="space-y-6">
        {/* Session Management */}
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

        {/* Password Policy */}
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
              <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                Minimum number of characters required for passwords
              </p>
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
              <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                Maximum password length (capped at 72 for bcrypt compatibility)
              </p>
            </div>
          </div>
        </div>

        {/* Login Protection */}
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
              <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                Maximum login attempts per IP address per minute
              </p>
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
                Lock account after this many failed attempts (0 = disabled)
              </p>
            </div>
          </div>
        </div>

        {/* Network */}
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

        {/* Coming Soon sections */}
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 border dark:border-gray-700 opacity-60">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2 mb-2">
            <Users className="w-5 h-5 text-gray-400" />
            Roles &amp; Permissions
            <span className="ml-2 px-2 py-0.5 bg-gray-200 dark:bg-gray-700 text-gray-500 dark:text-gray-400 rounded text-xs font-normal">
              coming soon
            </span>
          </h2>
          <p className="text-sm text-gray-500 dark:text-gray-400">
            Custom roles, fine-grained permissions, and role-based access control configuration.
          </p>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 border dark:border-gray-700 opacity-60">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2 mb-2">
            <Shield className="w-5 h-5 text-gray-400" />
            SSO / OIDC
            <span className="ml-2 px-2 py-0.5 bg-gray-200 dark:bg-gray-700 text-gray-500 dark:text-gray-400 rounded text-xs font-normal">
              coming soon
            </span>
          </h2>
          <p className="text-sm text-gray-500 dark:text-gray-400">
            Single sign-on with OpenID Connect providers (Okta, Azure AD, Google Workspace).
          </p>
        </div>
      </div>
    </div>
  )
}
