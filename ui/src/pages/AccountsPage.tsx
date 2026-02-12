import { useEffect, useState } from 'react'
import { Plus, Search, Trash2 } from 'lucide-react'
import { useAccounts } from '../hooks/useAccounts'
import { useToast } from '../hooks/useToast'
import StatusBadge from '../components/StatusBadge'
import type { CreateAccountRequest } from '../api/types'
import { formatTimeAgo } from '../utils/format'

export default function AccountsPage() {
  const { accounts, loading, error, fetchAccounts, createAccount, deleteAccount } = useAccounts()
  const { showToast } = useToast()
  const [search, setSearch] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const [form, setForm] = useState<CreateAccountRequest>({
    key: '', name: '', provider: 'aws', tier: 'dev',
  })

  useEffect(() => { fetchAccounts() }, [fetchAccounts])

  const filtered = accounts.filter(a => {
    if (!search) return true
    const q = search.toLowerCase()
    return a.name.toLowerCase().includes(q) ||
      a.key.toLowerCase().includes(q) ||
      (a.provider || '').toLowerCase().includes(q)
  })

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    try {
      await createAccount(form)
      showToast('Account created', 'success')
      setShowCreate(false)
      setForm({ key: '', name: '', provider: 'aws', tier: 'dev' })
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to create account', 'error')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleDelete(id: number, name: string) {
    if (!confirm(`Delete account "${name}"?`)) return
    try {
      await deleteAccount(id)
      showToast('Account deleted', 'success')
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to delete account', 'error')
    }
  }

  return (
    <div className="flex-1 overflow-auto p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-xl font-semibold text-gray-900">Cloud Accounts</h1>
        <button
          onClick={() => setShowCreate(!showCreate)}
          className="inline-flex items-center gap-1.5 px-3 py-2 bg-blue-600 text-white text-sm rounded hover:bg-blue-700"
        >
          <Plus className="w-4 h-4" />
          New Account
        </button>
      </div>

      {/* Create form */}
      {showCreate && (
        <form onSubmit={handleCreate} className="bg-white border rounded-lg p-4 mb-4 space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-gray-600 mb-1">Key (required)</label>
              <input
                required
                value={form.key}
                onChange={e => setForm({ ...form, key: e.target.value })}
                className="w-full px-3 py-1.5 border rounded text-sm font-mono"
                placeholder="e.g., aws:123456789012"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 mb-1">Name (required)</label>
              <input
                required
                value={form.name}
                onChange={e => setForm({ ...form, name: e.target.value })}
                className="w-full px-3 py-1.5 border rounded text-sm"
                placeholder="e.g., Production AWS"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 mb-1">Provider</label>
              <select
                value={form.provider}
                onChange={e => setForm({ ...form, provider: e.target.value })}
                className="w-full px-3 py-1.5 border rounded text-sm"
              >
                <option value="aws">AWS</option>
                <option value="gcp">GCP</option>
                <option value="azure">Azure</option>
                <option value="">Other</option>
              </select>
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 mb-1">Tier</label>
              <select
                value={form.tier}
                onChange={e => setForm({ ...form, tier: e.target.value })}
                className="w-full px-3 py-1.5 border rounded text-sm"
              >
                <option value="prd">Production</option>
                <option value="stg">Staging</option>
                <option value="dev">Development</option>
                <option value="sbx">Sandbox</option>
              </select>
            </div>
          </div>
          <div className="flex gap-2">
            <button
              type="submit"
              disabled={submitting}
              className="px-4 py-1.5 bg-blue-600 text-white text-sm rounded hover:bg-blue-700 disabled:opacity-50"
            >
              {submitting ? 'Creating...' : 'Create Account'}
            </button>
            <button
              type="button"
              onClick={() => setShowCreate(false)}
              className="px-4 py-1.5 border text-sm rounded hover:bg-gray-50"
            >
              Cancel
            </button>
          </div>
        </form>
      )}

      {/* Search */}
      <div className="relative mb-4">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
        <input
          value={search}
          onChange={e => setSearch(e.target.value)}
          placeholder="Search accounts by name, key, or provider..."
          className="w-full pl-9 pr-3 py-2 border rounded-lg text-sm"
        />
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 text-red-700 text-sm p-3 rounded mb-4">{error}</div>
      )}

      {loading ? (
        <div className="text-center py-12 text-gray-400">Loading accounts...</div>
      ) : filtered.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-gray-500">{search ? 'No accounts match your search' : 'No accounts yet.'}</p>
        </div>
      ) : (
        <div className="bg-white border rounded-lg overflow-hidden">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Key</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Provider</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Tier</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Regions</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
                <th className="px-4 py-2 text-right text-xs font-medium text-gray-500 uppercase">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {filtered.map(a => (
                <tr key={a.id} className="hover:bg-gray-50">
                  <td className="px-4 py-2 text-sm font-medium text-gray-900">{a.name}</td>
                  <td className="px-4 py-2 text-sm font-mono text-gray-600">{a.key}</td>
                  <td className="px-4 py-2"><StatusBadge label={a.provider || 'other'} variant="provider" /></td>
                  <td className="px-4 py-2">{a.tier && <StatusBadge label={a.tier} variant="tier" />}</td>
                  <td className="px-4 py-2 text-sm text-gray-500">{(a.regions || []).join(', ') || '-'}</td>
                  <td className="px-4 py-2 text-sm text-gray-400">{formatTimeAgo(a.created_at)}</td>
                  <td className="px-4 py-2 text-right">
                    <button
                      onClick={() => handleDelete(a.id, a.name)}
                      className="text-red-400 hover:text-red-600 p-1"
                      title="Delete"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
