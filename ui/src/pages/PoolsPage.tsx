import { useEffect, useState } from 'react'
import { Plus, Search, Trash2, ChevronRight } from 'lucide-react'
import { usePools } from '../hooks/usePools'
import { useAccounts } from '../hooks/useAccounts'
import { useToast } from '../hooks/useToast'
import PoolDetailPanel from '../components/PoolDetailPanel'
import StatusBadge from '../components/StatusBadge'
import type { Pool, PoolWithStats, CreatePoolRequest, PoolType, PoolStatus } from '../api/types'
import { formatHostCount, getHostCount, formatTimeAgo } from '../utils/format'
import { get } from '../api/client'

export default function PoolsPage() {
  const { pools, loading, error, fetchPools, fetchHierarchy, createPool, deletePool } = usePools()
  const { accounts, fetchAccounts } = useAccounts()
  const { showToast } = useToast()
  const [search, setSearch] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [selectedPool, setSelectedPool] = useState<PoolWithStats | null>(null)
  const [submitting, setSubmitting] = useState(false)

  // Create form state
  const [form, setForm] = useState<CreatePoolRequest>({
    name: '', cidr: '', type: 'subnet', status: 'active',
  })

  useEffect(() => {
    fetchPools()
    fetchHierarchy()
    fetchAccounts()
  }, [fetchPools, fetchHierarchy, fetchAccounts])

  const filteredPools = pools.filter(p => {
    if (!search) return !p.parent_id // Show root pools when no search
    const q = search.toLowerCase()
    return p.name.toLowerCase().includes(q) ||
      p.cidr.includes(q) ||
      (p.description || '').toLowerCase().includes(q)
  })

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    try {
      await createPool(form)
      showToast('Pool created', 'success')
      setShowCreate(false)
      setForm({ name: '', cidr: '', type: 'subnet', status: 'active' })
      fetchHierarchy()
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to create pool', 'error')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleDelete(pool: Pool) {
    if (!confirm(`Delete pool "${pool.name}" (${pool.cidr})?`)) return
    try {
      await deletePool(pool.id)
      showToast('Pool deleted', 'success')
      fetchHierarchy()
      if (selectedPool?.id === pool.id) setSelectedPool(null)
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to delete pool', 'error')
    }
  }

  async function handleSelectPool(pool: Pool) {
    try {
      const full = await get<PoolWithStats>(`/api/v1/pools/${pool.id}`)
      setSelectedPool(full)
    } catch {
      // Fallback to basic pool data
      setSelectedPool({ ...pool, stats: { total_ips: getHostCount(pool.cidr), used_ips: 0, available_ips: getHostCount(pool.cidr), utilization: 0, child_count: 0, direct_children: 0 }, children: [] })
    }
  }

  return (
    <div className="flex-1 flex overflow-hidden">
      <div className="flex-1 overflow-auto p-6">
        {/* Header */}
        <div className="flex items-center justify-between mb-4">
          <h1 className="text-xl font-semibold text-gray-900">Address Pools</h1>
          <button
            onClick={() => setShowCreate(!showCreate)}
            className="inline-flex items-center gap-1.5 px-3 py-2 bg-blue-600 text-white text-sm rounded hover:bg-blue-700"
          >
            <Plus className="w-4 h-4" />
            New Pool
          </button>
        </div>

        {/* Create form */}
        {showCreate && (
          <form onSubmit={handleCreate} className="bg-white border rounded-lg p-4 mb-4 space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs font-medium text-gray-600 mb-1">Name</label>
                <input
                  required
                  value={form.name}
                  onChange={e => setForm({ ...form, name: e.target.value })}
                  className="w-full px-3 py-1.5 border rounded text-sm"
                  placeholder="e.g., prod-us-east-1"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 mb-1">CIDR</label>
                <input
                  required
                  value={form.cidr}
                  onChange={e => setForm({ ...form, cidr: e.target.value })}
                  className="w-full px-3 py-1.5 border rounded text-sm font-mono"
                  placeholder="e.g., 10.0.0.0/16"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 mb-1">Type</label>
                <select
                  value={form.type}
                  onChange={e => setForm({ ...form, type: e.target.value as PoolType })}
                  className="w-full px-3 py-1.5 border rounded text-sm"
                >
                  <option value="supernet">Supernet</option>
                  <option value="region">Region</option>
                  <option value="environment">Environment</option>
                  <option value="vpc">VPC</option>
                  <option value="subnet">Subnet</option>
                </select>
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 mb-1">Status</label>
                <select
                  value={form.status}
                  onChange={e => setForm({ ...form, status: e.target.value as PoolStatus })}
                  className="w-full px-3 py-1.5 border rounded text-sm"
                >
                  <option value="active">Active</option>
                  <option value="planned">Planned</option>
                  <option value="deprecated">Deprecated</option>
                </select>
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 mb-1">Account</label>
                <select
                  value={form.account_id ?? ''}
                  onChange={e => setForm({ ...form, account_id: e.target.value ? Number(e.target.value) : undefined })}
                  className="w-full px-3 py-1.5 border rounded text-sm"
                >
                  <option value="">None</option>
                  {accounts.map(a => (
                    <option key={a.id} value={a.id}>{a.name} ({a.key})</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 mb-1">Parent Pool</label>
                <select
                  value={form.parent_id ?? ''}
                  onChange={e => setForm({ ...form, parent_id: e.target.value ? Number(e.target.value) : undefined })}
                  className="w-full px-3 py-1.5 border rounded text-sm"
                >
                  <option value="">None (root pool)</option>
                  {pools.map(p => (
                    <option key={p.id} value={p.id}>{p.name} ({p.cidr})</option>
                  ))}
                </select>
              </div>
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 mb-1">Description</label>
              <textarea
                value={form.description || ''}
                onChange={e => setForm({ ...form, description: e.target.value })}
                className="w-full px-3 py-1.5 border rounded text-sm"
                rows={2}
                placeholder="Optional description"
              />
            </div>
            <div className="flex gap-2">
              <button
                type="submit"
                disabled={submitting}
                className="px-4 py-1.5 bg-blue-600 text-white text-sm rounded hover:bg-blue-700 disabled:opacity-50"
              >
                {submitting ? 'Creating...' : 'Create Pool'}
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
            placeholder="Search pools by name, CIDR, or description..."
            className="w-full pl-9 pr-3 py-2 border rounded-lg text-sm"
          />
        </div>

        {/* Error */}
        {error && (
          <div className="bg-red-50 border border-red-200 text-red-700 text-sm p-3 rounded mb-4">
            {error}
          </div>
        )}

        {/* Table */}
        {loading ? (
          <div className="text-center py-12 text-gray-400">Loading pools...</div>
        ) : filteredPools.length === 0 ? (
          <div className="text-center py-12">
            <p className="text-gray-500">{search ? 'No pools match your search' : 'No pools yet. Create one to get started.'}</p>
          </div>
        ) : (
          <div className="bg-white border rounded-lg overflow-hidden">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">CIDR</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">IPs</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
                  <th className="px-4 py-2 text-right text-xs font-medium text-gray-500 uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {filteredPools.map(p => (
                  <tr key={p.id} className="hover:bg-gray-50">
                    <td className="px-4 py-2 text-sm font-medium text-gray-900">{p.name}</td>
                    <td className="px-4 py-2 text-sm font-mono text-gray-600">{p.cidr}</td>
                    <td className="px-4 py-2"><StatusBadge label={p.type} variant="type" /></td>
                    <td className="px-4 py-2"><StatusBadge label={p.status} /></td>
                    <td className="px-4 py-2 text-sm text-gray-500">{formatHostCount(getHostCount(p.cidr))}</td>
                    <td className="px-4 py-2 text-sm text-gray-400">{formatTimeAgo(p.created_at)}</td>
                    <td className="px-4 py-2 text-right">
                      <button
                        onClick={() => handleSelectPool(p)}
                        className="text-blue-600 hover:text-blue-800 p-1"
                        title="View details"
                      >
                        <ChevronRight className="w-4 h-4" />
                      </button>
                      <button
                        onClick={() => handleDelete(p)}
                        className="text-red-400 hover:text-red-600 p-1 ml-1"
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

      {/* Detail panel */}
      {selectedPool && (
        <PoolDetailPanel pool={selectedPool} onClose={() => setSelectedPool(null)} />
      )}
    </div>
  )
}
