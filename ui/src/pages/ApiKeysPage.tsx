import { useState } from 'react'
import { Key, Plus, Ban, Copy, Check, AlertCircle } from 'lucide-react'
import { useApiKeys } from '../hooks/useApiKeys'
import type { ApiKeyCreateResponse } from '../api/types'

// Must match backend validScopes in auth_handlers.go createAPIKey
const SCOPE_OPTIONS = [
  'pools:read', 'pools:write',
  'accounts:read', 'accounts:write',
  'keys:read', 'keys:write',
  'discovery:read', 'discovery:write',
  'audit:read',
  '*',
]

const SCOPE_LABELS: Record<string, string> = {
  'pools:read': 'Pools Read',
  'pools:write': 'Pools Write',
  'accounts:read': 'Accounts Read',
  'accounts:write': 'Accounts Write',
  'keys:read': 'Keys Read',
  'keys:write': 'Keys Write',
  'discovery:read': 'Discovery Read',
  'discovery:write': 'Discovery Write',
  'audit:read': 'Audit Read',
  '*': 'Admin (all)',
}

export default function ApiKeysPage() {
  const { keys, loading, error, create, revoke } = useApiKeys()
  const [showCreate, setShowCreate] = useState(false)
  const [newKeyName, setNewKeyName] = useState('')
  const [selectedScopes, setSelectedScopes] = useState<string[]>(['pools:read', 'accounts:read'])
  const [expiryDays, setExpiryDays] = useState<number | undefined>(undefined)
  const [createdKey, setCreatedKey] = useState<ApiKeyCreateResponse | null>(null)
  const [copied, setCopied] = useState(false)
  const [createError, setCreateError] = useState('')

  async function handleCreate() {
    if (!newKeyName.trim()) return
    setCreateError('')
    try {
      const res = await create({
        name: newKeyName.trim(),
        scopes: selectedScopes,
        expires_in_days: expiryDays,
      })
      setCreatedKey(res)
      setNewKeyName('')
      setSelectedScopes(['pools:read', 'accounts:read'])
      setExpiryDays(undefined)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : 'Failed to create key')
    }
  }

  function toggleScope(scope: string) {
    if (scope === '*') {
      setSelectedScopes(prev => prev.includes('*') ? [] : ['*'])
      return
    }
    setSelectedScopes(prev => {
      const filtered = prev.filter(s => s !== '*')
      return filtered.includes(scope) ? filtered.filter(s => s !== scope) : [...filtered, scope]
    })
  }

  async function copyKey(key: string) {
    await navigator.clipboard.writeText(key)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  function formatDate(d?: string | null) {
    if (!d) return '—'
    return new Date(d).toLocaleDateString()
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">API Keys</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Manage API keys for programmatic access
          </p>
        </div>
        <button
          onClick={() => { setShowCreate(true); setCreatedKey(null) }}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700"
        >
          <Plus className="w-4 h-4" />
          Create Key
        </button>
      </div>

      {error && (
        <div className="mb-4 flex items-center gap-2 text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 px-4 py-3 rounded-lg">
          <AlertCircle className="w-4 h-4 flex-shrink-0" />
          {error}
        </div>
      )}

      {/* Created key banner */}
      {createdKey && (
        <div className="mb-4 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg p-4">
          <p className="text-sm font-medium text-green-800 dark:text-green-300 mb-2">
            API key created. Copy it now — it won&apos;t be shown again.
          </p>
          <div className="flex items-center gap-2">
            <code className="flex-1 bg-white dark:bg-gray-800 border dark:border-gray-700 rounded px-3 py-2 text-sm font-mono text-gray-900 dark:text-gray-100 select-all">
              {createdKey.key}
            </code>
            <button
              onClick={() => copyKey(createdKey.key)}
              className="p-2 text-gray-500 hover:text-gray-700 dark:hover:text-gray-300"
            >
              {copied ? <Check className="w-4 h-4 text-green-600" /> : <Copy className="w-4 h-4" />}
            </button>
          </div>
        </div>
      )}

      {/* Create form */}
      {showCreate && !createdKey && (
        <div className="mb-6 bg-white dark:bg-gray-800 rounded-lg shadow p-4 border dark:border-gray-700">
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-3">New API Key</h3>

          <div className="space-y-3">
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Name</label>
              <input
                value={newKeyName}
                onChange={e => setNewKeyName(e.target.value)}
                placeholder="e.g. CI Pipeline"
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
                autoFocus
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Scopes</label>
              <div className="flex flex-wrap gap-2">
                {SCOPE_OPTIONS.map(scope => (
                  <button
                    key={scope}
                    onClick={() => toggleScope(scope)}
                    className={`px-2.5 py-1 rounded text-xs font-medium border transition-colors ${
                      selectedScopes.includes(scope)
                        ? 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 border-blue-300 dark:border-blue-700'
                        : 'bg-gray-50 dark:bg-gray-700 text-gray-600 dark:text-gray-400 border-gray-200 dark:border-gray-600'
                    }`}
                  >
                    {SCOPE_LABELS[scope] ?? scope}
                  </button>
                ))}
              </div>
            </div>

            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                Expires in (days, optional)
              </label>
              <input
                type="number"
                min={1}
                value={expiryDays ?? ''}
                onChange={e => setExpiryDays(e.target.value ? Number(e.target.value) : undefined)}
                placeholder="No expiration"
                className="w-48 px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
            </div>

            {createError && (
              <p className="text-sm text-red-600 dark:text-red-400">{createError}</p>
            )}

            <div className="flex gap-2 pt-1">
              <button
                onClick={handleCreate}
                disabled={!newKeyName.trim()}
                className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
              >
                Create
              </button>
              <button
                onClick={() => setShowCreate(false)}
                className="px-4 py-2 text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 text-sm"
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Keys table */}
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
              <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Name</th>
              <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Prefix</th>
              <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Scopes</th>
              <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Created</th>
              <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Expires</th>
              <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Status</th>
              <th className="text-right px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Actions</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-gray-500 dark:text-gray-400">
                  Loading...
                </td>
              </tr>
            ) : keys.length === 0 ? (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-gray-500 dark:text-gray-400">
                  <Key className="w-8 h-8 mx-auto mb-2 opacity-40" />
                  No API keys found
                </td>
              </tr>
            ) : (
              keys.map(k => (
                <tr key={k.id} className="border-b dark:border-gray-700 last:border-0 hover:bg-gray-50 dark:hover:bg-gray-750">
                  <td className="px-4 py-3 font-medium text-gray-900 dark:text-white">{k.name}</td>
                  <td className="px-4 py-3 font-mono text-gray-600 dark:text-gray-400">{k.prefix}...</td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {k.scopes.slice(0, 3).map(s => (
                        <span key={s} className="px-1.5 py-0.5 bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 rounded text-xs">
                          {s}
                        </span>
                      ))}
                      {k.scopes.length > 3 && (
                        <span className="px-1.5 py-0.5 text-gray-500 dark:text-gray-400 text-xs">
                          +{k.scopes.length - 3}
                        </span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-gray-600 dark:text-gray-400">{formatDate(k.created_at)}</td>
                  <td className="px-4 py-3 text-gray-600 dark:text-gray-400">{formatDate(k.expires_at)}</td>
                  <td className="px-4 py-3">
                    {k.revoked ? (
                      <span className="px-2 py-0.5 bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400 rounded text-xs font-medium">
                        Revoked
                      </span>
                    ) : (
                      <span className="px-2 py-0.5 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400 rounded text-xs font-medium">
                        Active
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-right">
                    {!k.revoked && (
                      <button
                        onClick={() => revoke(k.id)}
                        title="Revoke key"
                        className="p-1.5 text-gray-400 hover:text-red-600 dark:hover:text-red-400"
                      >
                        <Ban className="w-4 h-4" />
                      </button>
                    )}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
