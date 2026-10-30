import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Pencil, Plus, Search, Trash2, X } from 'lucide-react'
import { useAccounts } from '../hooks/useAccounts'
import { useToast } from '../hooks/useToast'
import StatusBadge from '../components/StatusBadge'
import FormError from '../components/FormError'
import type { Account, UpdateAccountRequest } from '../api/types'
import { formatTimeAgo } from '../utils/format'

interface AccountForm {
  key: string
  name: string
  provider: string
  tier: string
  environment: string
  platform: string
  regions: string
  external_id: string
  description: string
}

const NEW_ACCOUNT_FORM: AccountForm = {
  key: '', name: '', provider: 'aws', tier: 'dev', environment: '', platform: '', regions: '', external_id: '', description: '',
}

const PROVIDER_OPTIONS: [string, string][] = [['aws', 'AWS'], ['gcp', 'GCP'], ['azure', 'Azure'], ['', 'Other']]
const TIER_OPTIONS: [string, string][] = [['prd', 'Production'], ['stg', 'Staging'], ['dev', 'Development'], ['sbx', 'Sandbox'], ['', 'None']]

function formFromAccount(a: Account): AccountForm {
  return {
    key: a.key,
    name: a.name,
    provider: a.provider ?? '',
    tier: a.tier ?? '',
    environment: a.environment ?? '',
    platform: a.platform ?? '',
    regions: (a.regions ?? []).join(', '),
    external_id: a.external_id ?? '',
    description: a.description ?? '',
  }
}

// PATCH replaces every optional field, so edits always send the whole form.
function writableFields(f: AccountForm): UpdateAccountRequest {
  return {
    name: f.name.trim(),
    provider: f.provider,
    external_id: f.external_id.trim(),
    description: f.description.trim(),
    platform: f.platform.trim(),
    tier: f.tier,
    environment: f.environment.trim(),
    regions: f.regions.split(',').map(r => r.trim()).filter(Boolean),
  }
}

function withCurrent(options: [string, string][], current: string): [string, string][] {
  return options.some(([value]) => value === current) ? options : [...options, [current, current]]
}

const inputClass = 'w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100'
const labelClass = 'block text-xs font-medium text-gray-600 dark:text-gray-300 mb-1'

function AccountEditor({
  title,
  initial,
  editing,
  submitLabel,
  submittingLabel,
  onSubmit,
  onCancel,
  onBusyChange,
}: {
  title: string
  initial: AccountForm
  editing: boolean
  submitLabel: string
  submittingLabel: string
  onSubmit: (form: AccountForm) => Promise<void>
  onCancel: () => void
  onBusyChange?: (busy: boolean) => void
}) {
  const { showToast } = useToast()
  const id = useId()
  const [form, setForm] = useState(initial)
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  useEffect(() => { onBusyChange?.(submitting) }, [submitting, onBusyChange])

  function set<K extends keyof AccountForm>(field: K, value: AccountForm[K]) {
    setForm(prev => ({ ...prev, [field]: value }))
    setFormError(null)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    try {
      await onSubmit(form)
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to save account'
      setFormError(message)
      showToast(message, 'error')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form aria-label={title} onSubmit={handleSubmit} className="space-y-3">
      <FormError message={formError} />
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label htmlFor={`${id}-key`} className={labelClass}>Key (required)</label>
          <input
            id={`${id}-key`}
            required
            readOnly={editing}
            value={form.key}
            onChange={e => set('key', e.target.value)}
            className={`${inputClass} font-mono ${editing ? 'bg-gray-50 text-gray-500 dark:bg-gray-900 dark:text-gray-400' : ''}`}
            placeholder="e.g., aws:123456789012"
          />
        </div>
        <div>
          <label htmlFor={`${id}-name`} className={labelClass}>Name (required)</label>
          <input
            id={`${id}-name`}
            required
            value={form.name}
            onChange={e => set('name', e.target.value)}
            className={inputClass}
            placeholder="e.g., Production AWS"
          />
        </div>
        <div>
          <label htmlFor={`${id}-provider`} className={labelClass}>Provider</label>
          <select id={`${id}-provider`} value={form.provider} onChange={e => set('provider', e.target.value)} className={inputClass}>
            {withCurrent(PROVIDER_OPTIONS, form.provider).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </div>
        <div>
          <label htmlFor={`${id}-tier`} className={labelClass}>Tier</label>
          <select id={`${id}-tier`} value={form.tier} onChange={e => set('tier', e.target.value)} className={inputClass}>
            {withCurrent(TIER_OPTIONS, form.tier).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </div>
        <div>
          <label htmlFor={`${id}-environment`} className={labelClass}>Environment</label>
          <input
            id={`${id}-environment`}
            value={form.environment}
            onChange={e => set('environment', e.target.value)}
            className={inputClass}
            placeholder="e.g., production"
          />
        </div>
        <div>
          <label htmlFor={`${id}-platform`} className={labelClass}>Platform</label>
          <input
            id={`${id}-platform`}
            value={form.platform}
            onChange={e => set('platform', e.target.value)}
            className={inputClass}
            placeholder="e.g., eks"
          />
        </div>
        <div>
          <label htmlFor={`${id}-regions`} className={labelClass}>Regions</label>
          <input
            id={`${id}-regions`}
            value={form.regions}
            onChange={e => set('regions', e.target.value)}
            className={inputClass}
            placeholder="e.g., us-east-1, us-west-2"
          />
        </div>
        <div>
          <label htmlFor={`${id}-external-id`} className={labelClass}>External ID</label>
          <input
            id={`${id}-external-id`}
            value={form.external_id}
            onChange={e => set('external_id', e.target.value)}
            className={`${inputClass} font-mono`}
            placeholder="e.g., 123456789012"
          />
        </div>
        <div className="sm:col-span-2">
          <label htmlFor={`${id}-description`} className={labelClass}>Description</label>
          <textarea
            id={`${id}-description`}
            value={form.description}
            onChange={e => set('description', e.target.value)}
            rows={2}
            className={inputClass}
          />
        </div>
      </div>
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={submitting}
          className="px-4 py-1.5 bg-blue-600 text-white text-sm rounded hover:bg-blue-700 disabled:opacity-50"
        >
          {submitting ? submittingLabel : submitLabel}
        </button>
        <button
          type="button"
          onClick={onCancel}
          disabled={submitting}
          className="px-4 py-1.5 border dark:border-gray-600 text-sm rounded hover:bg-gray-50 dark:hover:bg-gray-700/50 dark:text-gray-300"
        >
          Cancel
        </button>
      </div>
    </form>
  )
}

function EditAccountDialog({
  account,
  returnFocusTo,
  onSubmit,
  onClose,
}: {
  account: Account
  returnFocusTo: HTMLElement | null
  onSubmit: (form: AccountForm) => Promise<void>
  onClose: () => void
}) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    dialogRef.current?.querySelector<HTMLElement>('input:not([readonly])')?.focus()
    return () => {
      if (returnFocusTo?.isConnected) returnFocusTo.focus()
    }
  }, [returnFocusTo])

  useEffect(() => {
    if (saving) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [saving, onClose])

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="edit-account-title"
        className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-2xl mx-4 max-h-[90vh] overflow-auto"
      >
        <div className="flex items-center justify-between px-6 py-4 border-b dark:border-gray-700">
          <h2 id="edit-account-title" className="text-lg font-semibold text-gray-900 dark:text-gray-100">Edit Account</h2>
          <button
            onClick={onClose}
            disabled={saving}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 disabled:opacity-50"
            title="Close"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="px-6 py-4">
          <AccountEditor
            title="Edit Account"
            initial={formFromAccount(account)}
            editing
            submitLabel="Save"
            submittingLabel="Saving..."
            onSubmit={onSubmit}
            onCancel={onClose}
            onBusyChange={setSaving}
          />
        </div>
      </div>
    </div>
  )
}

export default function AccountsPage() {
  const { accounts, loading, error, fetchAccounts, createAccount, updateAccount, deleteAccount } = useAccounts()
  const { showToast } = useToast()
  const [search, setSearch] = useState('')
  const [searchParams, setSearchParams] = useSearchParams()
  const selectedId = Number(searchParams.get('selected')) || null
  const [showCreate, setShowCreate] = useState(false)
  const [editing, setEditing] = useState<Account | null>(null)
  const editTrigger = useRef<HTMLElement | null>(null)

  useEffect(() => { fetchAccounts() }, [fetchAccounts])

  function handleSearchChange(value: string) {
    setSearch(value)
    if (selectedId) setSearchParams({}, { replace: true })
  }

  const filtered = accounts.filter(a => {
    if (selectedId) return a.id === selectedId
    if (!search) return true
    const q = search.toLowerCase()
    return a.name.toLowerCase().includes(q) ||
      a.key.toLowerCase().includes(q) ||
      (a.provider || '').toLowerCase().includes(q)
  })

  async function handleCreate(form: AccountForm) {
    await createAccount({ key: form.key.trim(), ...writableFields(form) })
    showToast('Account created', 'success')
    setShowCreate(false)
  }

  async function handleUpdate(id: number, form: AccountForm) {
    const updated = await updateAccount(id, writableFields(form))
    showToast(`Updated ${updated.name}`, 'success')
    setEditing(current => (current?.id === id ? null : current))
  }

  const closeEditor = useCallback(() => setEditing(null), [])

  async function handleDelete(id: number, name: string) {
    if (!confirm(`Delete account "${name}"?`)) return
    try {
      await deleteAccount(id)
      showToast('Account deleted', 'success')
      if (id === selectedId) setSearchParams({}, { replace: true })
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to delete account', 'error')
    }
  }

  return (
    <div className="flex-1 overflow-auto p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Cloud Accounts</h1>
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
        <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg p-4 mb-4">
          <AccountEditor
            title="New Account"
            initial={NEW_ACCOUNT_FORM}
            editing={false}
            submitLabel="Create Account"
            submittingLabel="Creating..."
            onSubmit={handleCreate}
            onCancel={() => setShowCreate(false)}
          />
        </div>
      )}

      {/* Search */}
      <div className="relative mb-4">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 dark:text-gray-500" />
        <input
          value={search}
          onChange={e => handleSearchChange(e.target.value)}
          placeholder="Search accounts by name, key, or provider..."
          className="w-full pl-9 pr-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
        />
      </div>

      {error && (
        <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm p-3 rounded mb-4">{error}</div>
      )}

      {loading ? (
        <div className="text-center py-12 text-gray-400 dark:text-gray-500">Loading accounts...</div>
      ) : filtered.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-gray-500 dark:text-gray-400">{selectedId ? 'Account not found.' : search ? 'No accounts match your search' : 'No accounts yet.'}</p>
        </div>
      ) : (
        <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg overflow-hidden">
          <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
            <thead className="bg-gray-50 dark:bg-gray-800">
              <tr>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Name</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Key</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Provider</th>
                <th className="hidden md:table-cell px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Tier</th>
                <th className="hidden md:table-cell px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Regions</th>
                <th className="hidden md:table-cell px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Created</th>
                <th className="px-4 py-2 text-right text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100 dark:divide-gray-700">
              {filtered.map(a => (
                <tr key={a.id} className="hover:bg-gray-50 dark:hover:bg-gray-700/50">
                  <td className="px-4 py-2 text-sm font-medium text-gray-900 dark:text-gray-100">{a.name}</td>
                  <td className="px-4 py-2 text-sm font-mono text-gray-600 dark:text-gray-300">{a.key}</td>
                  <td className="px-4 py-2"><StatusBadge label={a.provider || 'other'} variant="provider" /></td>
                  <td className="hidden md:table-cell px-4 py-2">{a.tier && <StatusBadge label={a.tier} variant="tier" />}</td>
                  <td className="hidden md:table-cell px-4 py-2 text-sm text-gray-500 dark:text-gray-400">{(a.regions || []).join(', ') || '-'}</td>
                  <td className="hidden md:table-cell px-4 py-2 text-sm text-gray-400 dark:text-gray-500">{formatTimeAgo(a.created_at)}</td>
                  <td className="px-4 py-2 text-right">
                    <button
                      onClick={e => {
                        editTrigger.current = e.currentTarget
                        setEditing(a)
                      }}
                      className="text-gray-400 dark:text-gray-500 hover:text-blue-600 dark:hover:text-blue-400 p-1"
                      title="Edit"
                    >
                      <Pencil className="w-4 h-4" />
                    </button>
                    <button
                      onClick={() => handleDelete(a.id, a.name)}
                      className="text-red-400 dark:text-red-500 hover:text-red-600 dark:hover:text-red-400 p-1"
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

      {editing && (
        <EditAccountDialog
          key={editing.id}
          account={editing}
          returnFocusTo={editTrigger.current}
          onSubmit={form => handleUpdate(editing.id, form)}
          onClose={closeEditor}
        />
      )}
    </div>
  )
}
