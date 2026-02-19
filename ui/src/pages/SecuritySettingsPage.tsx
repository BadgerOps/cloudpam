import { useState, useEffect } from 'react'
import { Shield, Lock, Key, Globe, AlertCircle, Loader2, Users, Fingerprint, Plus, Pencil, Trash2, Zap, X, CheckCircle, XCircle } from 'lucide-react'
import { useSecuritySettings } from '../hooks/useSettings'
import type { SecuritySettings } from '../hooks/useSettings'
import { useOIDCAdmin } from '../hooks/useOIDCAdmin'
import type { OIDCProvider, OIDCProviderCreate, OIDCProviderUpdate, TestResult } from '../hooks/useOIDCAdmin'
import { useToast } from '../hooks/useToast'

// ---------- OIDC Provider Form Modal ----------

interface ProviderFormProps {
  provider?: OIDCProvider | null
  onSave: (data: OIDCProviderCreate | OIDCProviderUpdate) => Promise<void>
  onClose: () => void
}

function ProviderFormModal({ provider, onSave, onClose }: ProviderFormProps) {
  const isEdit = !!provider
  const [name, setName] = useState(provider?.name ?? '')
  const [issuerUrl, setIssuerUrl] = useState(provider?.issuer_url ?? '')
  const [clientId, setClientId] = useState(provider?.client_id ?? '')
  const [clientSecret, setClientSecret] = useState('')
  const [scopes, setScopes] = useState(provider?.scopes ?? 'openid profile email')
  const [defaultRole, setDefaultRole] = useState(provider?.default_role ?? 'viewer')
  const [autoProvision, setAutoProvision] = useState(provider?.auto_provision ?? true)
  const [enabled, setEnabled] = useState(provider?.enabled ?? true)
  const [roleMappingEntries, setRoleMappingEntries] = useState<Array<{ group: string; role: string }>>(
    provider?.role_mapping
      ? Object.entries(provider.role_mapping).map(([group, role]) => ({ group, role }))
      : []
  )
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  function addRoleMapping() {
    setRoleMappingEntries(prev => [...prev, { group: '', role: 'viewer' }])
  }

  function removeRoleMapping(idx: number) {
    setRoleMappingEntries(prev => prev.filter((_, i) => i !== idx))
  }

  function updateRoleMapping(idx: number, field: 'group' | 'role', value: string) {
    setRoleMappingEntries(prev => prev.map((entry, i) => i === idx ? { ...entry, [field]: value } : entry))
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setFormError(null)

    if (!name.trim()) { setFormError('Name is required'); return }
    if (!issuerUrl.trim()) { setFormError('Issuer URL is required'); return }
    if (!clientId.trim()) { setFormError('Client ID is required'); return }
    if (!isEdit && !clientSecret.trim()) { setFormError('Client Secret is required for new providers'); return }

    const roleMapping: Record<string, string> = {}
    for (const entry of roleMappingEntries) {
      if (entry.group.trim()) {
        roleMapping[entry.group.trim()] = entry.role
      }
    }

    setSaving(true)
    try {
      if (isEdit) {
        const updates: OIDCProviderUpdate = {
          name: name.trim(),
          issuer_url: issuerUrl.trim(),
          client_id: clientId.trim(),
          scopes,
          default_role: defaultRole,
          auto_provision: autoProvision,
          enabled,
          role_mapping: roleMapping,
        }
        if (clientSecret.trim()) {
          updates.client_secret = clientSecret.trim()
        }
        await onSave(updates)
      } else {
        const create: OIDCProviderCreate = {
          name: name.trim(),
          issuer_url: issuerUrl.trim(),
          client_id: clientId.trim(),
          client_secret: clientSecret.trim(),
          scopes,
          default_role: defaultRole,
          auto_provision: autoProvision,
          enabled,
          role_mapping: roleMapping,
        }
        await onSave(create)
      }
      onClose()
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Failed to save provider')
    } finally {
      setSaving(false)
    }
  }

  const inputClass = 'w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500'
  const labelClass = 'block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1'

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg max-h-[90vh] overflow-y-auto mx-4" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-6 py-4 border-b dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
            {isEdit ? 'Edit Provider' : 'Add OIDC Provider'}
          </h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
            <X className="w-5 h-5" />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="px-6 py-4 space-y-4">
          {formError && (
            <div className="flex items-center gap-2 text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 px-3 py-2 rounded-lg">
              <AlertCircle className="w-4 h-4 flex-shrink-0" />
              {formError}
            </div>
          )}

          <div>
            <label className={labelClass}>Name *</label>
            <input type="text" value={name} onChange={e => setName(e.target.value)} placeholder="e.g. Okta, Azure AD" className={inputClass} />
          </div>

          <div>
            <label className={labelClass}>Issuer URL *</label>
            <input type="url" value={issuerUrl} onChange={e => setIssuerUrl(e.target.value)} placeholder="https://accounts.google.com" className={inputClass} />
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              The OIDC discovery endpoint will be resolved from this URL
            </p>
          </div>

          <div>
            <label className={labelClass}>Client ID *</label>
            <input type="text" value={clientId} onChange={e => setClientId(e.target.value)} className={inputClass} />
          </div>

          <div>
            <label className={labelClass}>Client Secret {isEdit ? '(leave blank to keep current)' : '*'}</label>
            <input type="password" value={clientSecret} onChange={e => setClientSecret(e.target.value)} className={inputClass} />
          </div>

          <div>
            <label className={labelClass}>Scopes</label>
            <input type="text" value={scopes} onChange={e => setScopes(e.target.value)} className={inputClass} />
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Space-separated OIDC scopes
            </p>
          </div>

          <div>
            <label className={labelClass}>Default Role</label>
            <select value={defaultRole} onChange={e => setDefaultRole(e.target.value)} className={inputClass}>
              <option value="admin">Admin</option>
              <option value="operator">Operator</option>
              <option value="viewer">Viewer</option>
              <option value="auditor">Auditor</option>
            </select>
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Role assigned to users when no role mapping matches
            </p>
          </div>

          {/* Role Mapping */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <label className={labelClass}>Role Mapping</label>
              <button type="button" onClick={addRoleMapping} className="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400 flex items-center gap-1">
                <Plus className="w-3 h-3" /> Add mapping
              </button>
            </div>
            {roleMappingEntries.length === 0 && (
              <p className="text-xs text-gray-500 dark:text-gray-400">
                No role mappings configured. All users will get the default role.
              </p>
            )}
            {roleMappingEntries.map((entry, idx) => (
              <div key={idx} className="flex items-center gap-2 mb-2">
                <input
                  type="text"
                  value={entry.group}
                  onChange={e => updateRoleMapping(idx, 'group', e.target.value)}
                  placeholder="Group name"
                  className="flex-1 px-2 py-1.5 border rounded text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white"
                />
                <select
                  value={entry.role}
                  onChange={e => updateRoleMapping(idx, 'role', e.target.value)}
                  className="px-2 py-1.5 border rounded text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white"
                >
                  <option value="admin">Admin</option>
                  <option value="operator">Operator</option>
                  <option value="viewer">Viewer</option>
                  <option value="auditor">Auditor</option>
                </select>
                <button type="button" onClick={() => removeRoleMapping(idx)} className="text-red-400 hover:text-red-600">
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            ))}
          </div>

          {/* Toggles */}
          <div className="flex items-center gap-6">
            <label className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300 cursor-pointer">
              <input type="checkbox" checked={autoProvision} onChange={e => setAutoProvision(e.target.checked)} className="rounded" />
              Auto-provision users
            </label>
            <label className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300 cursor-pointer">
              <input type="checkbox" checked={enabled} onChange={e => setEnabled(e.target.checked)} className="rounded" />
              Enabled
            </label>
          </div>

          <div className="flex justify-end gap-3 pt-2 border-t dark:border-gray-700">
            <button type="button" onClick={onClose} className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600">
              Cancel
            </button>
            <button type="submit" disabled={saving} className="flex items-center gap-2 px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50">
              {saving && <Loader2 className="w-4 h-4 animate-spin" />}
              {isEdit ? 'Update Provider' : 'Add Provider'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ---------- Delete Confirmation Modal ----------

interface DeleteConfirmProps {
  providerName: string
  onConfirm: () => Promise<void>
  onClose: () => void
}

function DeleteConfirmModal({ providerName, onConfirm, onClose }: DeleteConfirmProps) {
  const [deleting, setDeleting] = useState(false)

  async function handleDelete() {
    setDeleting(true)
    try {
      await onConfirm()
      onClose()
    } catch {
      setDeleting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md mx-4" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-4">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">Delete Provider</h3>
          <p className="text-sm text-gray-600 dark:text-gray-400">
            Are you sure you want to delete <span className="font-medium text-gray-900 dark:text-white">{providerName}</span>? Users will no longer be able to sign in with this provider.
          </p>
        </div>
        <div className="flex justify-end gap-3 px-6 py-4 border-t dark:border-gray-700">
          <button onClick={onClose} className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600">
            Cancel
          </button>
          <button onClick={handleDelete} disabled={deleting} className="flex items-center gap-2 px-4 py-2 text-sm font-medium text-white bg-red-600 rounded-lg hover:bg-red-700 disabled:opacity-50">
            {deleting && <Loader2 className="w-4 h-4 animate-spin" />}
            Delete
          </button>
        </div>
      </div>
    </div>
  )
}

// ---------- Local Auth Toggle Confirmation ----------

interface LocalAuthConfirmProps {
  enabling: boolean
  onConfirm: () => void
  onClose: () => void
}

function LocalAuthConfirmModal({ enabling, onConfirm, onClose }: LocalAuthConfirmProps) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md mx-4" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-4">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">
            {enabling ? 'Enable' : 'Disable'} Local Authentication
          </h3>
          <p className="text-sm text-gray-600 dark:text-gray-400">
            {enabling
              ? 'Users will be able to sign in with local username and password in addition to any configured SSO providers.'
              : 'Users will only be able to sign in through configured SSO/OIDC providers. Make sure at least one provider is enabled and working before disabling local auth.'
            }
          </p>
        </div>
        <div className="flex justify-end gap-3 px-6 py-4 border-t dark:border-gray-700">
          <button onClick={onClose} className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600">
            Cancel
          </button>
          <button onClick={() => { onConfirm(); onClose() }} className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700">
            {enabling ? 'Enable' : 'Disable'} Local Auth
          </button>
        </div>
      </div>
    </div>
  )
}

// ---------- Main Page ----------

export default function SecuritySettingsPage() {
  const { settings, loading, error, updateSettings } = useSecuritySettings()
  const { showToast } = useToast()
  const oidcAdmin = useOIDCAdmin()
  const [form, setForm] = useState<SecuritySettings | null>(null)
  const [saving, setSaving] = useState(false)
  const [trustedProxiesText, setTrustedProxiesText] = useState('')

  // OIDC modals
  const [showProviderForm, setShowProviderForm] = useState(false)
  const [editingProvider, setEditingProvider] = useState<OIDCProvider | null>(null)
  const [deletingProvider, setDeletingProvider] = useState<OIDCProvider | null>(null)
  const [testingId, setTestingId] = useState<string | null>(null)
  const [testResult, setTestResult] = useState<{ id: string; result: TestResult } | null>(null)

  // Local auth toggle
  const [showLocalAuthConfirm, setShowLocalAuthConfirm] = useState(false)
  const [pendingLocalAuth, setPendingLocalAuth] = useState(false)

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

  function handleLocalAuthToggle(newValue: boolean) {
    setPendingLocalAuth(newValue)
    setShowLocalAuthConfirm(true)
  }

  function confirmLocalAuthToggle() {
    updateField('local_auth_enabled', pendingLocalAuth)
  }

  async function handleTestProvider(id: string) {
    setTestingId(id)
    setTestResult(null)
    try {
      const result = await oidcAdmin.testProvider(id)
      setTestResult({ id, result })
    } catch (err) {
      setTestResult({ id, result: { success: false, message: err instanceof Error ? err.message : 'Test failed' } })
    } finally {
      setTestingId(null)
    }
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

        {/* SSO / OIDC */}
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 border dark:border-gray-700">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <Shield className="w-5 h-5 text-indigo-500" />
              SSO / OIDC
            </h2>
            <button
              onClick={() => { setEditingProvider(null); setShowProviderForm(true) }}
              className="flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium text-white bg-indigo-600 rounded-lg hover:bg-indigo-700"
            >
              <Plus className="w-4 h-4" />
              Add Provider
            </button>
          </div>

          <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">
            Configure OpenID Connect providers for single sign-on (Okta, Azure AD, Google Workspace, etc.)
          </p>

          {/* Local Auth Toggle */}
          <div className="flex items-center justify-between p-3 mb-4 bg-gray-50 dark:bg-gray-750 rounded-lg border dark:border-gray-600">
            <div>
              <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Local Authentication</p>
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Allow users to sign in with local username and password
              </p>
            </div>
            <button
              type="button"
              onClick={() => handleLocalAuthToggle(!form.local_auth_enabled)}
              className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                form.local_auth_enabled ? 'bg-blue-600' : 'bg-gray-300 dark:bg-gray-600'
              }`}
            >
              <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                form.local_auth_enabled ? 'translate-x-6' : 'translate-x-1'
              }`} />
            </button>
          </div>

          {/* Provider List */}
          {oidcAdmin.loading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="w-5 h-5 animate-spin text-gray-400" />
              <span className="ml-2 text-sm text-gray-500 dark:text-gray-400">Loading providers...</span>
            </div>
          ) : oidcAdmin.error ? (
            <div className="flex items-center gap-2 text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 px-3 py-2 rounded-lg">
              <AlertCircle className="w-4 h-4 flex-shrink-0" />
              {oidcAdmin.error}
            </div>
          ) : oidcAdmin.providers.length === 0 ? (
            <div className="text-center py-8 text-sm text-gray-500 dark:text-gray-400">
              No OIDC providers configured. Click &quot;Add Provider&quot; to set up single sign-on.
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b dark:border-gray-700 text-left">
                    <th className="pb-2 font-medium text-gray-500 dark:text-gray-400">Name</th>
                    <th className="pb-2 font-medium text-gray-500 dark:text-gray-400">Issuer URL</th>
                    <th className="pb-2 font-medium text-gray-500 dark:text-gray-400 text-center">Status</th>
                    <th className="pb-2 font-medium text-gray-500 dark:text-gray-400 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {oidcAdmin.providers.map(provider => (
                    <tr key={provider.id} className="border-b dark:border-gray-700 last:border-0">
                      <td className="py-3 font-medium text-gray-900 dark:text-white">{provider.name}</td>
                      <td className="py-3 text-gray-600 dark:text-gray-400 truncate max-w-[200px]" title={provider.issuer_url}>
                        {provider.issuer_url}
                      </td>
                      <td className="py-3 text-center">
                        {provider.enabled ? (
                          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400">
                            <CheckCircle className="w-3 h-3" /> Enabled
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-400">
                            <XCircle className="w-3 h-3" /> Disabled
                          </span>
                        )}
                      </td>
                      <td className="py-3">
                        <div className="flex items-center justify-end gap-1">
                          <button
                            onClick={() => handleTestProvider(provider.id)}
                            disabled={testingId === provider.id}
                            title="Test connection"
                            className="p-1.5 text-gray-400 hover:text-amber-600 dark:hover:text-amber-400 rounded hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-50"
                          >
                            {testingId === provider.id ? <Loader2 className="w-4 h-4 animate-spin" /> : <Zap className="w-4 h-4" />}
                          </button>
                          <button
                            onClick={() => { setEditingProvider(provider); setShowProviderForm(true) }}
                            title="Edit provider"
                            className="p-1.5 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 rounded hover:bg-gray-100 dark:hover:bg-gray-700"
                          >
                            <Pencil className="w-4 h-4" />
                          </button>
                          <button
                            onClick={() => setDeletingProvider(provider)}
                            title="Delete provider"
                            className="p-1.5 text-gray-400 hover:text-red-600 dark:hover:text-red-400 rounded hover:bg-gray-100 dark:hover:bg-gray-700"
                          >
                            <Trash2 className="w-4 h-4" />
                          </button>
                        </div>
                        {/* Test result inline */}
                        {testResult && testResult.id === provider.id && (
                          <div className={`mt-1 text-xs px-2 py-1 rounded ${
                            testResult.result.success
                              ? 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-400'
                              : 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-400'
                          }`}>
                            {testResult.result.success ? 'Connection successful' : testResult.result.message}
                          </div>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {/* Coming Soon - Roles & Permissions */}
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
      </div>

      {/* Modals */}
      {showProviderForm && (
        <ProviderFormModal
          provider={editingProvider}
          onSave={async (data) => {
            if (editingProvider) {
              await oidcAdmin.updateProvider(editingProvider.id, data as OIDCProviderUpdate)
              showToast('Provider updated', 'success')
            } else {
              await oidcAdmin.createProvider(data as OIDCProviderCreate)
              showToast('Provider added', 'success')
            }
          }}
          onClose={() => { setShowProviderForm(false); setEditingProvider(null) }}
        />
      )}

      {deletingProvider && (
        <DeleteConfirmModal
          providerName={deletingProvider.name}
          onConfirm={async () => {
            await oidcAdmin.deleteProvider(deletingProvider.id)
            showToast('Provider deleted', 'success')
          }}
          onClose={() => setDeletingProvider(null)}
        />
      )}

      {showLocalAuthConfirm && (
        <LocalAuthConfirmModal
          enabling={pendingLocalAuth}
          onConfirm={confirmLocalAuthToggle}
          onClose={() => setShowLocalAuthConfirm(false)}
        />
      )}
    </div>
  )
}
