import { useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { CheckCircle, Loader2, Plus, Pencil, Shield, Trash2, Users, KeyRound } from 'lucide-react'
import { useToast } from '../hooks/useToast'
import { useSecuritySettings } from '../hooks/useSettings'
import { useOIDCAdmin } from '../hooks/useOIDCAdmin'
import type { OIDCProvider, OIDCProviderCreate, OIDCProviderUpdate } from '../hooks/useOIDCAdmin'
import UsersAdminPanel from '../components/UsersAdminPanel'

const TABS = [
  { id: 'providers', label: 'Providers' },
  { id: 'users', label: 'Users' },
  { id: 'rbac', label: 'RBAC' },
] as const

const BUILTIN_RBAC = [
  {
    role: 'admin',
    summary: 'Full access across identity, settings, discovery, IPAM, audit, and API key management.',
    permissions: ['pools:*', 'accounts:*', 'apikeys:*', 'audit:read', 'users:*', 'discovery:*', 'settings:*'],
  },
  {
    role: 'operator',
    summary: 'Read/write access for IPAM and discovery operations, but no identity or settings administration.',
    permissions: ['pools:*', 'accounts:*', 'discovery:create/read/update/list'],
  },
  {
    role: 'viewer',
    summary: 'Read-only access for IPAM and discovery data.',
    permissions: ['pools:read/list', 'accounts:read/list', 'discovery:read/list'],
  },
  {
    role: 'auditor',
    summary: 'Audit log access only.',
    permissions: ['audit:read/list'],
  },
] as const

interface ProviderFormProps {
  provider?: OIDCProvider | null
  onSave: (payload: OIDCProviderCreate | OIDCProviderUpdate) => Promise<void>
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
    provider?.role_mapping ? Object.entries(provider.role_mapping).map(([group, role]) => ({ group, role })) : []
  )
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function updateMapping(index: number, field: 'group' | 'role', value: string) {
    setRoleMappingEntries(prev => prev.map((entry, idx) => idx === index ? { ...entry, [field]: value } : entry))
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    if (!name.trim() || !issuerUrl.trim() || !clientId.trim() || (!isEdit && !clientSecret.trim())) {
      setError('Name, issuer URL, client ID, and client secret are required.')
      return
    }
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
        if (clientSecret.trim()) updates.client_secret = clientSecret.trim()
        await onSave(updates)
      } else {
        await onSave({
          name: name.trim(),
          issuer_url: issuerUrl.trim(),
          client_id: clientId.trim(),
          client_secret: clientSecret.trim(),
          scopes,
          default_role: defaultRole,
          auto_provision: autoProvision,
          enabled,
          role_mapping: roleMapping,
        })
      }
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save provider')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4" onClick={onClose}>
      <div className="w-full max-w-2xl rounded-xl bg-white dark:bg-gray-800 shadow-xl max-h-[90vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white">{isEdit ? 'Edit Provider' : 'Add Provider'}</h2>
            <button type="button" onClick={onClose} className="text-sm text-gray-500 dark:text-gray-400">Close</button>
          </div>

          {error && (
            <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
              {error}
            </div>
          )}

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <label className="text-sm">
              <span className="block mb-1 text-gray-700 dark:text-gray-300">Name</span>
              <input value={name} onChange={e => setName(e.target.value)} className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white" />
            </label>
            <label className="text-sm">
              <span className="block mb-1 text-gray-700 dark:text-gray-300">Issuer URL</span>
              <input value={issuerUrl} onChange={e => setIssuerUrl(e.target.value)} className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white" />
            </label>
            <label className="text-sm">
              <span className="block mb-1 text-gray-700 dark:text-gray-300">Client ID</span>
              <input value={clientId} onChange={e => setClientId(e.target.value)} className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white" />
            </label>
            <label className="text-sm">
              <span className="block mb-1 text-gray-700 dark:text-gray-300">Client Secret {isEdit ? '(optional)' : ''}</span>
              <input type="password" value={clientSecret} onChange={e => setClientSecret(e.target.value)} className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white" />
            </label>
            <label className="text-sm">
              <span className="block mb-1 text-gray-700 dark:text-gray-300">Scopes</span>
              <input value={scopes} onChange={e => setScopes(e.target.value)} className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white" />
            </label>
            <label className="text-sm">
              <span className="block mb-1 text-gray-700 dark:text-gray-300">Default Role</span>
              <select value={defaultRole} onChange={e => setDefaultRole(e.target.value)} className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white">
                <option value="admin">Admin</option>
                <option value="operator">Operator</option>
                <option value="viewer">Viewer</option>
                <option value="auditor">Auditor</option>
              </select>
            </label>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium text-gray-900 dark:text-white">Role Mapping</h3>
              <button type="button" onClick={() => setRoleMappingEntries(prev => [...prev, { group: '', role: 'viewer' }])} className="text-sm text-blue-600 dark:text-blue-400">
                Add mapping
              </button>
            </div>
            {roleMappingEntries.map((entry, idx) => (
              <div key={idx} className="flex items-center gap-2">
                <input
                  value={entry.group}
                  onChange={e => updateMapping(idx, 'group', e.target.value)}
                  placeholder="Group or claim value"
                  className="flex-1 px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white"
                />
                <select
                  value={entry.role}
                  onChange={e => updateMapping(idx, 'role', e.target.value)}
                  className="px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white"
                >
                  <option value="admin">Admin</option>
                  <option value="operator">Operator</option>
                  <option value="viewer">Viewer</option>
                  <option value="auditor">Auditor</option>
                </select>
                <button type="button" onClick={() => setRoleMappingEntries(prev => prev.filter((_, entryIdx) => entryIdx !== idx))} className="p-2 text-red-600">
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            ))}
          </div>

          <div className="flex items-center gap-6 text-sm text-gray-700 dark:text-gray-300">
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={autoProvision} onChange={e => setAutoProvision(e.target.checked)} />
              Auto-provision users
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={enabled} onChange={e => setEnabled(e.target.checked)} />
              Enabled
            </label>
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <button type="button" onClick={onClose} className="px-4 py-2 rounded-lg border border-gray-300 dark:border-gray-600 text-sm text-gray-700 dark:text-gray-300">
              Cancel
            </button>
            <button type="submit" disabled={saving} className="px-4 py-2 rounded-lg bg-blue-600 text-white text-sm font-medium hover:bg-blue-700 disabled:opacity-50">
              {saving ? 'Saving…' : isEdit ? 'Update Provider' : 'Create Provider'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default function IdentityPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const activeTab = (searchParams.get('tab') as typeof TABS[number]['id'] | null) ?? 'providers'
  const { showToast } = useToast()
  const { settings, loading: settingsLoading, updateSettings } = useSecuritySettings()
  const oidcAdmin = useOIDCAdmin()
  const [editingProvider, setEditingProvider] = useState<OIDCProvider | null>(null)
  const [showProviderForm, setShowProviderForm] = useState(false)
  const [deletingProvider, setDeletingProvider] = useState<OIDCProvider | null>(null)
  const [testingID, setTestingID] = useState<string | null>(null)
  const [testingMessage, setTestingMessage] = useState<string | null>(null)

  const tabs = useMemo(() => TABS, [])

  async function toggleLocalAuth() {
    if (!settings) return
    try {
      await updateSettings({ ...settings, local_auth_enabled: !settings.local_auth_enabled })
      showToast(`Local authentication ${settings.local_auth_enabled ? 'disabled' : 'enabled'}`, 'success')
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to update local authentication', 'error')
    }
  }

  async function saveProvider(payload: OIDCProviderCreate | OIDCProviderUpdate) {
    if (editingProvider) {
      await oidcAdmin.updateProvider(editingProvider.id, payload)
      showToast('Provider updated', 'success')
    } else {
      await oidcAdmin.createProvider(payload as OIDCProviderCreate)
      showToast('Provider created', 'success')
    }
    setShowProviderForm(false)
    setEditingProvider(null)
  }

  async function deleteProvider() {
    if (!deletingProvider) return
    await oidcAdmin.deleteProvider(deletingProvider.id)
    showToast('Provider deleted', 'success')
    setDeletingProvider(null)
  }

  async function testProvider(id: string) {
    try {
      setTestingID(id)
      const result = await oidcAdmin.testProvider(id)
      setTestingMessage(result.message)
      showToast(result.message, result.success ? 'success' : 'error')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Provider test failed'
      setTestingMessage(message)
      showToast(message, 'error')
    } finally {
      setTestingID(null)
    }
  }

  return (
    <div className="p-6 space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Identity</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
          Authentication providers, local users, and built-in RBAC.
        </p>
      </div>

      <div className="flex gap-2 border-b border-gray-200 dark:border-gray-700">
        {tabs.map(tab => (
          <button
            key={tab.id}
            onClick={() => setSearchParams({ tab: tab.id })}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              activeTab === tab.id
                ? 'border-blue-600 text-blue-600 dark:text-blue-400'
                : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {activeTab === 'providers' && (
        <div className="space-y-6">
          <section className="bg-white dark:bg-gray-800 rounded-xl shadow border dark:border-gray-700 p-6">
            <div className="flex items-center justify-between gap-4">
              <div>
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
                  <KeyRound className="w-5 h-5 text-indigo-500" />
                  Authentication Providers
                </h2>
                <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                  Configure OIDC sign-in providers and local password access.
                </p>
              </div>
              <button
                onClick={() => { setEditingProvider(null); setShowProviderForm(true) }}
                className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-blue-600 text-white text-sm font-medium hover:bg-blue-700"
              >
                <Plus className="w-4 h-4" />
                Add Provider
              </button>
            </div>

            <div className="mt-4 rounded-lg border border-gray-200 dark:border-gray-700 p-4 flex items-center justify-between gap-4">
              <div>
                <p className="font-medium text-gray-900 dark:text-white">Local Authentication</p>
                <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                  Allow local username and password sign-in alongside SSO providers.
                </p>
              </div>
              <button
                type="button"
                disabled={settingsLoading || !settings}
                onClick={toggleLocalAuth}
                className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                  settings?.local_auth_enabled ? 'bg-blue-600' : 'bg-gray-300 dark:bg-gray-600'
                }`}
              >
                <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                  settings?.local_auth_enabled ? 'translate-x-6' : 'translate-x-1'
                }`} />
              </button>
            </div>

            {testingMessage && (
              <div className="mt-4 rounded-lg bg-blue-50 text-blue-700 dark:bg-blue-950/30 dark:text-blue-300 px-3 py-2 text-sm">
                {testingMessage}
              </div>
            )}

            <div className="mt-6">
              {oidcAdmin.loading ? (
                <div className="flex items-center gap-2 text-gray-500 dark:text-gray-400">
                  <Loader2 className="w-5 h-5 animate-spin" />
                  Loading providers…
                </div>
              ) : oidcAdmin.error ? (
                <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
                  {oidcAdmin.error}
                </div>
              ) : oidcAdmin.providers.length === 0 ? (
                <div className="rounded-lg border border-dashed border-gray-300 dark:border-gray-700 px-4 py-6 text-sm text-gray-500 dark:text-gray-400">
                  No providers configured yet.
                </div>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b dark:border-gray-700">
                        <th className="text-left pb-3 font-medium text-gray-500 dark:text-gray-400">Provider</th>
                        <th className="text-left pb-3 font-medium text-gray-500 dark:text-gray-400">Issuer</th>
                        <th className="text-left pb-3 font-medium text-gray-500 dark:text-gray-400">Default Role</th>
                        <th className="text-left pb-3 font-medium text-gray-500 dark:text-gray-400">Status</th>
                        <th className="text-right pb-3 font-medium text-gray-500 dark:text-gray-400">Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {oidcAdmin.providers.map(provider => (
                        <tr key={provider.id} className="border-b dark:border-gray-700 last:border-0">
                          <td className="py-3">
                            <div className="font-medium text-gray-900 dark:text-white">{provider.name}</div>
                            <div className="text-xs text-gray-500 dark:text-gray-400">
                              {provider.auto_provision ? 'Auto-provisioning enabled' : 'Manual provisioning'}
                            </div>
                          </td>
                          <td className="py-3 text-gray-600 dark:text-gray-400">{provider.issuer_url}</td>
                          <td className="py-3 text-gray-600 dark:text-gray-400 uppercase">{provider.default_role}</td>
                          <td className="py-3">
                            {provider.enabled ? (
                              <span className="inline-flex items-center gap-1 rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700 dark:bg-green-900/30 dark:text-green-300">
                                <CheckCircle className="w-3 h-3" />
                                Enabled
                              </span>
                            ) : (
                              <span className="inline-flex items-center gap-1 rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-700 dark:text-gray-300">
                                Disabled
                              </span>
                            )}
                          </td>
                          <td className="py-3">
                            <div className="flex items-center justify-end gap-2">
                              <button onClick={() => testProvider(provider.id)} className="p-2 text-gray-500 hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400" title="Test provider">
                                {testingID === provider.id ? <Loader2 className="w-4 h-4 animate-spin" /> : <Shield className="w-4 h-4" />}
                              </button>
                              <button onClick={() => { setEditingProvider(provider); setShowProviderForm(true) }} className="p-2 text-gray-500 hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400" title="Edit provider">
                                <Pencil className="w-4 h-4" />
                              </button>
                              <button onClick={() => setDeletingProvider(provider)} className="p-2 text-gray-500 hover:text-red-600 dark:text-gray-400 dark:hover:text-red-400" title="Delete provider">
                                <Trash2 className="w-4 h-4" />
                              </button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </section>
        </div>
      )}

      {activeTab === 'users' && <UsersAdminPanel embedded />}

      {activeTab === 'rbac' && (
        <div className="space-y-4">
          <section className="bg-white dark:bg-gray-800 rounded-xl shadow border dark:border-gray-700 p-6">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <Users className="w-5 h-5 text-blue-500" />
              Built-In Roles
            </h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
              CloudPAM currently uses fixed roles defined in the server. Custom role editing can land later without changing this layout.
            </p>
            <div className="mt-6 grid gap-4 md:grid-cols-2">
              {BUILTIN_RBAC.map(role => (
                <div key={role.role} className="rounded-lg border border-gray-200 dark:border-gray-700 p-4">
                  <h3 className="text-base font-semibold text-gray-900 dark:text-white uppercase">{role.role}</h3>
                  <p className="mt-2 text-sm text-gray-600 dark:text-gray-400">{role.summary}</p>
                  <div className="mt-3 flex flex-wrap gap-2">
                    {role.permissions.map(permission => (
                      <span key={permission} className="rounded-full bg-blue-50 px-2.5 py-1 text-xs font-medium text-blue-700 dark:bg-blue-950/30 dark:text-blue-300">
                        {permission}
                      </span>
                    ))}
                  </div>
                </div>
              ))}
            </div>
            <div className="mt-6 rounded-lg bg-gray-50 dark:bg-gray-900/40 px-4 py-3 text-sm text-gray-600 dark:text-gray-400">
              Session, password, rate-limit, and proxy policy stay under <Link to="/config/security" className="text-blue-600 dark:text-blue-400">Configuration → Security Policy</Link>.
            </div>
          </section>
        </div>
      )}

      {showProviderForm && (
        <ProviderFormModal
          provider={editingProvider}
          onSave={saveProvider}
          onClose={() => {
            setShowProviderForm(false)
            setEditingProvider(null)
          }}
        />
      )}

      {deletingProvider && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4" onClick={() => setDeletingProvider(null)}>
          <div className="w-full max-w-md rounded-xl bg-white dark:bg-gray-800 shadow-xl p-6" onClick={e => e.stopPropagation()}>
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Delete Provider</h2>
            <p className="mt-2 text-sm text-gray-600 dark:text-gray-400">
              Delete <span className="font-medium text-gray-900 dark:text-white">{deletingProvider.name}</span>? Users will no longer be able to sign in with it.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button onClick={() => setDeletingProvider(null)} className="px-4 py-2 rounded-lg border border-gray-300 dark:border-gray-600 text-sm text-gray-700 dark:text-gray-300">
                Cancel
              </button>
              <button onClick={deleteProvider} className="px-4 py-2 rounded-lg bg-red-600 text-white text-sm font-medium hover:bg-red-700">
                Delete
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
