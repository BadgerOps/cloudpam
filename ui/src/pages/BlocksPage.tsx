import { useEffect, useState, useMemo, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Search, LayoutGrid, Pencil, Trash2, X } from 'lucide-react'
import { useBlocks } from '../hooks/useBlocks'
import { useAccounts } from '../hooks/useAccounts'
import { useToast } from '../hooks/useToast'
import { patch, del } from '../api/client'
import StatusBadge from '../components/StatusBadge'
import { formatHostCount, getHostCount } from '../utils/format'
import type { Block, PoolType, PoolStatus, UpdatePoolRequest } from '../api/types'

export default function BlocksPage() {
  const navigate = useNavigate()
  const { blocks, loading, error, fetchBlocks } = useBlocks()
  const { accounts, fetchAccounts } = useAccounts()
  const { showToast } = useToast()
  const [search, setSearch] = useState('')
  const [accountFilter, setAccountFilter] = useState<string>('')
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [editBlock, setEditBlock] = useState<Block | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Block | null>(null)
  const [showBulkDelete, setShowBulkDelete] = useState(false)
  const [actionLoading, setActionLoading] = useState(false)

  useEffect(() => {
    fetchBlocks()
    fetchAccounts()
  }, [fetchBlocks, fetchAccounts])

  // Clear selection when filters change
  useEffect(() => {
    setSelectedIds(new Set())
  }, [search, accountFilter])

  const filtered = useMemo(() => {
    return blocks.filter(b => {
      if (accountFilter && String(b.account_id) !== accountFilter) return false
      if (!search) return true
      const q = search.toLowerCase()
      return b.name.toLowerCase().includes(q) ||
        b.cidr.includes(q) ||
        (b.account_name || '').toLowerCase().includes(q) ||
        (b.parent_name || '').toLowerCase().includes(q)
    })
  }, [blocks, search, accountFilter])

  const filteredIds = useMemo(() => new Set(filtered.map(b => b.id)), [filtered])
  const isAllSelected = filtered.length > 0 && filtered.every(b => selectedIds.has(b.id))

  const toggleSelect = useCallback((id: number) => {
    setSelectedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const toggleSelectAll = useCallback(() => {
    if (isAllSelected) {
      setSelectedIds(new Set())
    } else {
      setSelectedIds(new Set(filtered.map(b => b.id)))
    }
  }, [isAllSelected, filtered])

  // Summary stats
  const summaryStats = useMemo(() => {
    const totalBlocks = filtered.length
    let totalIPs = 0
    const uniqueAccounts = new Set<number>()
    for (const b of filtered) {
      totalIPs += getHostCount(b.cidr)
      if (b.account_id) uniqueAccounts.add(b.account_id)
    }
    return { totalBlocks, totalIPs, uniqueAccounts: uniqueAccounts.size }
  }, [filtered])

  async function handleDelete(block: Block) {
    setActionLoading(true)
    try {
      await del(`/api/v1/pools/${block.id}`)
      showToast(`Deleted ${block.name}`, 'success')
      setDeleteTarget(null)
      setSelectedIds(prev => {
        const next = new Set(prev)
        next.delete(block.id)
        return next
      })
      await fetchBlocks()
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Delete failed', 'error')
    } finally {
      setActionLoading(false)
    }
  }

  async function handleBulkDelete() {
    setActionLoading(true)
    const ids = [...selectedIds].filter(id => filteredIds.has(id))
    let deleted = 0
    let failed = 0
    for (const id of ids) {
      try {
        await del(`/api/v1/pools/${id}`)
        deleted++
      } catch {
        failed++
      }
    }
    showToast(
      `Deleted ${deleted} block${deleted !== 1 ? 's' : ''}${failed > 0 ? `, ${failed} failed` : ''}`,
      failed > 0 ? 'error' : 'success',
    )
    setSelectedIds(new Set())
    setShowBulkDelete(false)
    setActionLoading(false)
    await fetchBlocks()
  }

  // Count only selected items visible in current filter
  const visibleSelectedCount = [...selectedIds].filter(id => filteredIds.has(id)).length

  return (
    <div className="flex-1 overflow-auto p-6">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Allocated Blocks</h1>
      </div>

      {/* Filters */}
      <div className="flex gap-3 mb-4">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 dark:text-gray-500" />
          <input
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder="Search blocks by name, CIDR, account, or parent..."
            className="w-full pl-9 pr-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
          />
        </div>
        <select
          value={accountFilter}
          onChange={e => setAccountFilter(e.target.value)}
          className="px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
        >
          <option value="">All Accounts</option>
          {accounts.map(a => (
            <option key={a.id} value={a.id}>{a.name}</option>
          ))}
        </select>
      </div>

      {/* Action bar */}
      {visibleSelectedCount > 0 && (
        <div className="flex items-center gap-3 mb-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg px-4 py-2">
          <span className="text-sm font-medium text-blue-700 dark:text-blue-300">
            {visibleSelectedCount} block{visibleSelectedCount !== 1 ? 's' : ''} selected
          </span>
          <button
            onClick={() => setShowBulkDelete(true)}
            className="flex items-center gap-1 px-3 py-1 text-sm bg-red-600 hover:bg-red-700 text-white rounded"
          >
            <Trash2 className="w-3.5 h-3.5" />
            Delete Selected
          </button>
          <button
            onClick={() => setSelectedIds(new Set())}
            className="text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200"
          >
            Clear Selection
          </button>
        </div>
      )}

      {/* Summary */}
      <div className="grid grid-cols-3 gap-4 mb-4">
        <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg p-3 text-center">
          <div className="text-lg font-bold text-gray-900 dark:text-gray-100">{summaryStats.totalBlocks}</div>
          <div className="text-xs text-gray-500 dark:text-gray-400">Total Blocks</div>
        </div>
        <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg p-3 text-center">
          <div className="text-lg font-bold text-gray-900 dark:text-gray-100">{formatHostCount(summaryStats.totalIPs)}</div>
          <div className="text-xs text-gray-500 dark:text-gray-400">Total IPs</div>
        </div>
        <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg p-3 text-center">
          <div className="text-lg font-bold text-gray-900 dark:text-gray-100">{summaryStats.uniqueAccounts}</div>
          <div className="text-xs text-gray-500 dark:text-gray-400">Unique Accounts</div>
        </div>
      </div>

      {error && (
        <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm p-3 rounded mb-4">{error}</div>
      )}

      {loading ? (
        <div className="text-center py-12 text-gray-400 dark:text-gray-500">Loading blocks...</div>
      ) : filtered.length === 0 ? (
        <div className="text-center py-12">
          <LayoutGrid className="w-12 h-12 mx-auto mb-2 text-gray-300 dark:text-gray-600" />
          <p className="text-gray-500 dark:text-gray-400">No blocks found</p>
          <button
            onClick={() => navigate('/pools')}
            className="mt-2 text-sm text-blue-600 hover:text-blue-800"
          >
            Go to Pools to create allocations
          </button>
        </div>
      ) : (
        <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg overflow-hidden">
          <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
            <thead className="bg-gray-50 dark:bg-gray-800">
              <tr>
                <th className="px-4 py-2 text-left">
                  <input
                    type="checkbox"
                    checked={isAllSelected}
                    onChange={toggleSelectAll}
                    className="rounded border-gray-300 dark:border-gray-600"
                  />
                </th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Name</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">CIDR</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Parent</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Account</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Tier</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">IPs</th>
                <th className="px-4 py-2 text-right text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100 dark:divide-gray-700">
              {filtered.map(b => {
                const hostCount = getHostCount(b.cidr)
                return (
                  <tr key={b.id} className={`hover:bg-gray-50 dark:hover:bg-gray-700/50 ${selectedIds.has(b.id) ? 'bg-blue-50 dark:bg-blue-900/10' : ''}`}>
                    <td className="px-4 py-2">
                      <input
                        type="checkbox"
                        checked={selectedIds.has(b.id)}
                        onChange={() => toggleSelect(b.id)}
                        className="rounded border-gray-300 dark:border-gray-600"
                      />
                    </td>
                    <td className="px-4 py-2 text-sm font-medium text-gray-900 dark:text-gray-100">{b.name}</td>
                    <td className="px-4 py-2 text-sm font-mono text-gray-600 dark:text-gray-300">{b.cidr}</td>
                    <td className="px-4 py-2 text-sm text-gray-500 dark:text-gray-400">{b.parent_name || '-'}</td>
                    <td className="px-4 py-2 text-sm text-gray-500 dark:text-gray-400">{b.account_name || '-'}</td>
                    <td className="px-4 py-2">
                      {b.account_tier && <StatusBadge label={b.account_tier} variant="tier" />}
                    </td>
                    <td className="px-4 py-2 text-sm text-gray-500 dark:text-gray-400">{formatHostCount(hostCount)}</td>
                    <td className="px-4 py-2 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <button
                          onClick={() => setEditBlock(b)}
                          title="Edit block"
                          className="p-1 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400"
                        >
                          <Pencil className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => setDeleteTarget(b)}
                          title="Delete block"
                          className="p-1 text-gray-400 hover:text-red-600 dark:hover:text-red-400"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Edit Modal */}
      {editBlock && (
        <EditBlockModal
          block={editBlock}
          accounts={accounts}
          onClose={() => setEditBlock(null)}
          onSaved={() => {
            setEditBlock(null)
            fetchBlocks()
          }}
        />
      )}

      {/* Single Delete Confirmation */}
      {deleteTarget && (
        <ConfirmDialog
          title="Delete Block"
          message={`Are you sure you want to delete "${deleteTarget.name}" (${deleteTarget.cidr})?`}
          confirmLabel="Delete"
          loading={actionLoading}
          onConfirm={() => handleDelete(deleteTarget)}
          onCancel={() => setDeleteTarget(null)}
        />
      )}

      {/* Bulk Delete Confirmation */}
      {showBulkDelete && (
        <ConfirmDialog
          title="Delete Selected Blocks"
          message={`Are you sure you want to delete ${visibleSelectedCount} selected block${visibleSelectedCount !== 1 ? 's' : ''}? This cannot be undone.`}
          confirmLabel={`Delete ${visibleSelectedCount} Block${visibleSelectedCount !== 1 ? 's' : ''}`}
          loading={actionLoading}
          onConfirm={handleBulkDelete}
          onCancel={() => setShowBulkDelete(false)}
        />
      )}
    </div>
  )
}

function EditBlockModal({
  block,
  accounts,
  onClose,
  onSaved,
}: {
  block: Block
  accounts: { id: number; name: string }[]
  onClose: () => void
  onSaved: () => void
}) {
  const { showToast } = useToast()
  const [name, setName] = useState(block.name)
  const [accountId, setAccountId] = useState<string>(block.account_id ? String(block.account_id) : '')
  const [type, setType] = useState(block.type || 'subnet')
  const [status, setStatus] = useState(block.status || 'active')
  const [description, setDescription] = useState('')
  const [saving, setSaving] = useState(false)

  async function handleSave() {
    setSaving(true)
    try {
      const data: UpdatePoolRequest = {}
      if (name !== block.name) data.name = name
      if (accountId !== (block.account_id ? String(block.account_id) : '')) {
        data.account_id = accountId ? Number(accountId) : null
      }
      if (type !== (block.type || 'subnet')) data.type = type as PoolType
      if (status !== (block.status || 'active')) data.status = status as PoolStatus
      if (description) data.description = description

      await patch(`/api/v1/pools/${block.id}`, data)
      showToast(`Updated ${name}`, 'success')
      onSaved()
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Update failed', 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md mx-4">
        <div className="flex items-center justify-between px-6 py-4 border-b dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">Edit Block</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="px-6 py-4 space-y-4">
          <div className="text-sm text-gray-500 dark:text-gray-400 font-mono bg-gray-50 dark:bg-gray-900 px-3 py-2 rounded">
            {block.cidr}
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Name</label>
            <input
              value={name}
              onChange={e => setName(e.target.value)}
              className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Account</label>
            <select
              value={accountId}
              onChange={e => setAccountId(e.target.value)}
              className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
            >
              <option value="">None</option>
              {accounts.map(a => (
                <option key={a.id} value={a.id}>{a.name}</option>
              ))}
            </select>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Type</label>
              <select
                value={type}
                onChange={e => setType(e.target.value)}
                className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
              >
                <option value="supernet">Supernet</option>
                <option value="region">Region</option>
                <option value="environment">Environment</option>
                <option value="vpc">VPC</option>
                <option value="subnet">Subnet</option>
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Status</label>
              <select
                value={status}
                onChange={e => setStatus(e.target.value)}
                className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
              >
                <option value="planned">Planned</option>
                <option value="active">Active</option>
                <option value="deprecated">Deprecated</option>
              </select>
            </div>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Description</label>
            <textarea
              value={description}
              onChange={e => setDescription(e.target.value)}
              rows={3}
              placeholder="Optional description..."
              className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
            />
          </div>
        </div>
        <div className="flex justify-end gap-3 px-6 py-4 border-t dark:border-gray-700">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 border dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700"
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={saving || !name.trim()}
            className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded-lg disabled:opacity-50"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}

function ConfirmDialog({
  title,
  message,
  confirmLabel,
  loading,
  onConfirm,
  onCancel,
}: {
  title: string
  message: string
  confirmLabel: string
  loading: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-sm mx-4">
        <div className="px-6 py-4">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-2">{title}</h3>
          <p className="text-sm text-gray-600 dark:text-gray-400">{message}</p>
        </div>
        <div className="flex justify-end gap-3 px-6 py-4 border-t dark:border-gray-700">
          <button
            onClick={onCancel}
            disabled={loading}
            className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 border dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            disabled={loading}
            className="px-4 py-2 text-sm bg-red-600 hover:bg-red-700 text-white rounded-lg disabled:opacity-50"
          >
            {loading ? 'Deleting...' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
