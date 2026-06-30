import { useEffect, useState, type FormEvent } from 'react'
import { Plus, Search, Trash2, ChevronRight, Pencil, X } from 'lucide-react'
import { usePools } from '../hooks/usePools'
import { useAccounts } from '../hooks/useAccounts'
import { useToast } from '../hooks/useToast'
import PoolDetailPanel from '../components/PoolDetailPanel'
import StatusBadge from '../components/StatusBadge'
import type { Pool, PoolWithStats, CreatePoolRequest, UpdatePoolRequest, PoolType, PoolStatus } from '../api/types'
import { formatHostCount, getHostCount, formatTimeAgo } from '../utils/format'
import { get } from '../api/client'

type PoolFormErrors = {
  name?: string
  cidr?: string
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback
}

function validatePoolName(name: string): string | undefined {
  return name.trim() ? undefined : 'Name is required'
}

function validateIPv4CIDR(cidr: string): string | undefined {
  const trimmed = cidr.trim()
  if (!trimmed) return 'CIDR is required'

  const parts = trimmed.split('/')
  if (parts.length !== 2) return 'Enter an IPv4 CIDR such as 10.0.0.0/16'

  const [addr, prefixText] = parts
  const prefix = Number(prefixText)
  if (!Number.isInteger(prefix) || prefix < 8 || prefix > 32) {
    return 'Prefix length must be between 8 and 32'
  }

  const octets = addr.split('.')
  if (octets.length !== 4) return 'Enter an IPv4 CIDR such as 10.0.0.0/16'
  for (const octet of octets) {
    if (!/^\d+$/.test(octet)) return 'CIDR must contain numeric IPv4 octets'
    const value = Number(octet)
    if (value < 0 || value > 255) return 'IPv4 octets must be between 0 and 255'
  }

  return undefined
}

function validateCreatePool(form: CreatePoolRequest): PoolFormErrors {
  const errors: PoolFormErrors = {}
  const nameError = validatePoolName(form.name)
  const cidrError = validateIPv4CIDR(form.cidr)
  if (nameError) errors.name = nameError
  if (cidrError) errors.cidr = cidrError
  return errors
}

function hasErrors(errors: PoolFormErrors): boolean {
  return Object.values(errors).some(Boolean)
}

function fieldClass(hasError: boolean, extra = ''): string {
  return [
    'w-full px-3 py-1.5 border rounded text-sm dark:bg-gray-700 dark:text-gray-100',
    hasError ? 'border-red-400 dark:border-red-600' : 'dark:border-gray-600',
    extra,
  ].filter(Boolean).join(' ')
}

function FieldError({ id, message }: { id: string; message?: string }) {
  if (!message) return null
  return <p id={id} className="mt-1 text-xs text-red-600 dark:text-red-300">{message}</p>
}

function FormError({ message }: { message: string | null }) {
  if (!message) return null
  return (
    <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm p-3 rounded" role="alert">
      {message}
    </div>
  )
}

export default function PoolsPage() {
  const { pools, loading, error, fetchPools, fetchHierarchy, createPool, updatePool, deletePool } = usePools()
  const { accounts, fetchAccounts } = useAccounts()
  const { showToast } = useToast()
  const [search, setSearch] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [selectedPool, setSelectedPool] = useState<PoolWithStats | null>(null)
  const [editingPool, setEditingPool] = useState<Pool | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [createErrors, setCreateErrors] = useState<PoolFormErrors>({})
  const [createFormError, setCreateFormError] = useState<string | null>(null)

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

  function updateCreateField<K extends keyof CreatePoolRequest>(key: K, value: CreatePoolRequest[K]) {
    setForm(prev => ({ ...prev, [key]: value }))
    if (key === 'name' || key === 'cidr') {
      setCreateErrors(prev => ({ ...prev, [key]: undefined }))
    }
    setCreateFormError(null)
  }

  function resetCreateForm() {
    setShowCreate(false)
    setForm({ name: '', cidr: '', type: 'subnet', status: 'active' })
    setCreateErrors({})
    setCreateFormError(null)
  }

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    const errors = validateCreatePool(form)
    setCreateErrors(errors)
    setCreateFormError(null)
    if (hasErrors(errors)) {
      return
    }

    const payload: CreatePoolRequest = {
      ...form,
      name: form.name.trim(),
      cidr: form.cidr.trim(),
      description: form.description?.trim() || undefined,
    }

    setSubmitting(true)
    try {
      await createPool(payload)
      showToast('Pool created', 'success')
      resetCreateForm()
      fetchHierarchy()
    } catch (err) {
      const message = errorMessage(err, 'Failed to create pool')
      setCreateFormError(message)
      showToast(message, 'error')
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

  async function handleEditSaved(updated: Pool) {
    setEditingPool(null)
    await fetchPools()
    await fetchHierarchy()
    if (selectedPool?.id === updated.id) {
      await handleSelectPool(updated)
    }
  }

  return (
    <div className="flex-1 flex overflow-hidden">
      <div className="flex-1 overflow-auto p-6">
        {/* Header */}
        <div className="flex items-center justify-between mb-4">
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Address Pools</h1>
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
          <form onSubmit={handleCreate} noValidate className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg p-4 mb-4 space-y-3">
            <FormError message={createFormError} />
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label htmlFor="create-pool-name" className="block text-xs font-medium text-gray-600 dark:text-gray-300 mb-1">Name</label>
                <input
                  id="create-pool-name"
                  required
                  value={form.name}
                  onChange={e => updateCreateField('name', e.target.value)}
                  className={fieldClass(Boolean(createErrors.name))}
                  placeholder="e.g., prod-us-east-1"
                  aria-invalid={Boolean(createErrors.name)}
                  aria-describedby={createErrors.name ? 'create-pool-name-error' : undefined}
                />
                <FieldError id="create-pool-name-error" message={createErrors.name} />
              </div>
              <div>
                <label htmlFor="create-pool-cidr" className="block text-xs font-medium text-gray-600 dark:text-gray-300 mb-1">CIDR</label>
                <input
                  id="create-pool-cidr"
                  required
                  value={form.cidr}
                  onChange={e => updateCreateField('cidr', e.target.value)}
                  className={fieldClass(Boolean(createErrors.cidr), 'font-mono')}
                  placeholder="e.g., 10.0.0.0/16"
                  aria-invalid={Boolean(createErrors.cidr)}
                  aria-describedby={createErrors.cidr ? 'create-pool-cidr-error' : undefined}
                />
                <FieldError id="create-pool-cidr-error" message={createErrors.cidr} />
              </div>
              <div>
                <label htmlFor="create-pool-type" className="block text-xs font-medium text-gray-600 dark:text-gray-300 mb-1">Type</label>
                <select
                  id="create-pool-type"
                  value={form.type}
                  onChange={e => updateCreateField('type', e.target.value as PoolType)}
                  className={fieldClass(false)}
                >
                  <option value="supernet">Supernet</option>
                  <option value="region">Region</option>
                  <option value="environment">Environment</option>
                  <option value="vpc">VPC</option>
                  <option value="subnet">Subnet</option>
                </select>
              </div>
              <div>
                <label htmlFor="create-pool-status" className="block text-xs font-medium text-gray-600 dark:text-gray-300 mb-1">Status</label>
                <select
                  id="create-pool-status"
                  value={form.status}
                  onChange={e => updateCreateField('status', e.target.value as PoolStatus)}
                  className={fieldClass(false)}
                >
                  <option value="active">Active</option>
                  <option value="planned">Planned</option>
                  <option value="deprecated">Deprecated</option>
                </select>
              </div>
              <div>
                <label htmlFor="create-pool-account" className="block text-xs font-medium text-gray-600 dark:text-gray-300 mb-1">Account</label>
                <select
                  id="create-pool-account"
                  value={form.account_id ?? ''}
                  onChange={e => updateCreateField('account_id', e.target.value ? Number(e.target.value) : undefined)}
                  className={fieldClass(false)}
                >
                  <option value="">None</option>
                  {accounts.map(a => (
                    <option key={a.id} value={a.id}>{a.name} ({a.key})</option>
                  ))}
                </select>
              </div>
              <div>
                <label htmlFor="create-pool-parent" className="block text-xs font-medium text-gray-600 dark:text-gray-300 mb-1">Parent Pool</label>
                <select
                  id="create-pool-parent"
                  value={form.parent_id ?? ''}
                  onChange={e => updateCreateField('parent_id', e.target.value ? Number(e.target.value) : undefined)}
                  className={fieldClass(false)}
                >
                  <option value="">None (root pool)</option>
                  {pools.map(p => (
                    <option key={p.id} value={p.id}>{p.name} ({p.cidr})</option>
                  ))}
                </select>
              </div>
            </div>
            <div>
              <label htmlFor="create-pool-description" className="block text-xs font-medium text-gray-600 dark:text-gray-300 mb-1">Description</label>
              <textarea
                id="create-pool-description"
                value={form.description || ''}
                onChange={e => updateCreateField('description', e.target.value)}
                className={fieldClass(false)}
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
                onClick={resetCreateForm}
                className="px-4 py-1.5 border dark:border-gray-600 text-sm rounded hover:bg-gray-50 dark:hover:bg-gray-700/50 dark:text-gray-300"
              >
                Cancel
              </button>
            </div>
          </form>
        )}

        {/* Search */}
        <div className="relative mb-4">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 dark:text-gray-500" />
          <input
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder="Search pools by name, CIDR, or description..."
            className="w-full pl-9 pr-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
          />
        </div>

        {/* Error */}
        {error && (
          <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm p-3 rounded mb-4">
            {error}
          </div>
        )}

        {/* Table */}
        {loading ? (
          <div className="text-center py-12 text-gray-400 dark:text-gray-500">Loading pools...</div>
        ) : filteredPools.length === 0 ? (
          <div className="text-center py-12">
            <p className="text-gray-500 dark:text-gray-400">{search ? 'No pools match your search' : 'No pools yet. Create one to get started.'}</p>
          </div>
        ) : (
          <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg overflow-hidden">
            <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
              <thead className="bg-gray-50 dark:bg-gray-800">
                <tr>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Name</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">CIDR</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Type</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Status</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">IPs</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Created</th>
                  <th className="px-4 py-2 text-right text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100 dark:divide-gray-700">
                {filteredPools.map(p => (
                  <tr key={p.id} className="hover:bg-gray-50 dark:hover:bg-gray-700/50">
                    <td className="px-4 py-2 text-sm font-medium text-gray-900 dark:text-gray-100">{p.name}</td>
                    <td className="px-4 py-2 text-sm font-mono text-gray-600 dark:text-gray-300">{p.cidr}</td>
                    <td className="px-4 py-2"><StatusBadge label={p.type} variant="type" /></td>
                    <td className="px-4 py-2"><StatusBadge label={p.status} /></td>
                    <td className="px-4 py-2 text-sm text-gray-500 dark:text-gray-400">{formatHostCount(getHostCount(p.cidr))}</td>
                    <td className="px-4 py-2 text-sm text-gray-400 dark:text-gray-500">{formatTimeAgo(p.created_at)}</td>
                    <td className="px-4 py-2 text-right">
                      <button
                        onClick={() => setEditingPool(p)}
                        className="text-gray-400 dark:text-gray-500 hover:text-blue-600 dark:hover:text-blue-400 p-1"
                        title="Edit pool"
                      >
                        <Pencil className="w-4 h-4" />
                      </button>
                      <button
                        onClick={() => handleSelectPool(p)}
                        className="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 p-1"
                        title="View details"
                      >
                        <ChevronRight className="w-4 h-4" />
                      </button>
                      <button
                        onClick={() => handleDelete(p)}
                        className="text-red-400 dark:text-red-500 hover:text-red-600 dark:hover:text-red-400 p-1 ml-1"
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
        <PoolDetailPanel
          pool={selectedPool}
          onClose={() => setSelectedPool(null)}
          onEdit={() => setEditingPool(selectedPool)}
        />
      )}

      {editingPool && (
        <EditPoolModal
          pool={editingPool}
          accounts={accounts}
          updatePool={updatePool}
          onClose={() => setEditingPool(null)}
          onSaved={handleEditSaved}
        />
      )}
    </div>
  )
}

function EditPoolModal({
  pool,
  accounts,
  updatePool,
  onClose,
  onSaved,
}: {
  pool: Pool
  accounts: { id: number; name: string; key: string }[]
  updatePool: (id: number, data: UpdatePoolRequest) => Promise<Pool>
  onClose: () => void
  onSaved: (pool: Pool) => void
}) {
  const { showToast } = useToast()
  const [name, setName] = useState(pool.name)
  const [accountId, setAccountId] = useState<string>(pool.account_id ? String(pool.account_id) : '')
  const [type, setType] = useState<PoolType>(pool.type)
  const [status, setStatus] = useState<PoolStatus>(pool.status)
  const [description, setDescription] = useState(pool.description || '')
  const [saving, setSaving] = useState(false)
  const [errors, setErrors] = useState<PoolFormErrors>({})
  const [formError, setFormError] = useState<string | null>(null)

  function updateName(value: string) {
    setName(value)
    setErrors(prev => ({ ...prev, name: undefined }))
    setFormError(null)
  }

  async function handleSave() {
    const trimmedName = name.trim()
    const nameError = validatePoolName(trimmedName)
    if (nameError) {
      setErrors({ name: nameError })
      setFormError(null)
      return
    }

    setSaving(true)
    try {
      const updated = await updatePool(pool.id, {
        name: trimmedName,
        account_id: accountId ? Number(accountId) : null,
        type,
        status,
        description: description.trim(),
      })
      showToast(`Updated ${updated.name}`, 'success')
      onSaved(updated)
    } catch (err) {
      const message = errorMessage(err, 'Failed to update pool')
      setFormError(message)
      showToast(message, 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md mx-4">
        <div className="flex items-center justify-between px-6 py-4 border-b dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">Edit Pool</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" title="Close">
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="px-6 py-4 space-y-4">
          <FormError message={formError} />
          <div className="text-sm text-gray-500 dark:text-gray-400 font-mono bg-gray-50 dark:bg-gray-900 px-3 py-2 rounded">
            {pool.cidr}
          </div>
          <div>
            <label htmlFor="edit-pool-name" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Name</label>
            <input
              id="edit-pool-name"
              value={name}
              onChange={e => updateName(e.target.value)}
              className={[
                'w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100',
                errors.name ? 'border-red-400 dark:border-red-600' : 'dark:border-gray-600',
              ].join(' ')}
              aria-invalid={Boolean(errors.name)}
              aria-describedby={errors.name ? 'edit-pool-name-error' : undefined}
            />
            <FieldError id="edit-pool-name-error" message={errors.name} />
          </div>
          <div>
            <label htmlFor="edit-pool-account" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Account</label>
            <select
              id="edit-pool-account"
              value={accountId}
              onChange={e => { setAccountId(e.target.value); setFormError(null) }}
              className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
            >
              <option value="">None</option>
              {accounts.map(a => (
                <option key={a.id} value={a.id}>{a.name} ({a.key})</option>
              ))}
            </select>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label htmlFor="edit-pool-type" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Type</label>
              <select
                id="edit-pool-type"
                value={type}
                onChange={e => { setType(e.target.value as PoolType); setFormError(null) }}
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
              <label htmlFor="edit-pool-status" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Status</label>
              <select
                id="edit-pool-status"
                value={status}
                onChange={e => { setStatus(e.target.value as PoolStatus); setFormError(null) }}
                className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
              >
                <option value="active">Active</option>
                <option value="planned">Planned</option>
                <option value="deprecated">Deprecated</option>
              </select>
            </div>
          </div>
          <div>
            <label htmlFor="edit-pool-description" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Description</label>
            <textarea
              id="edit-pool-description"
              value={description}
              onChange={e => { setDescription(e.target.value); setFormError(null) }}
              rows={3}
              className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
            />
          </div>
        </div>
        <div className="flex justify-end gap-3 px-6 py-4 border-t dark:border-gray-700">
          <button
            onClick={onClose}
            disabled={saving}
            className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 border dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700"
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={saving}
            className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded-lg disabled:opacity-50"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}
