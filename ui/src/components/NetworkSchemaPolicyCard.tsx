import { useState } from 'react'
import { Loader2, Save } from 'lucide-react'
import { ApiRequestError } from '../api/client'
import { useNetworkSchemaPolicy } from '../hooks/useSettings'
import { useToast } from '../hooks/useToast'
import {
  NETWORK_SCHEMA_POLICIES,
  manualDuplicateScope,
  networkSchemaPolicyName,
  networkSchemaPolicyRequest,
  type ManualDuplicateScope,
  type NetworkSchemaPolicyName,
} from '../utils/networkPolicy'

interface Draft {
  name: NetworkSchemaPolicyName
  duplicateScope: ManualDuplicateScope
}

const selectClass =
  'mt-1 w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 disabled:cursor-not-allowed disabled:bg-gray-100 disabled:text-gray-500 dark:border-gray-600 dark:bg-gray-900 dark:text-white dark:disabled:bg-gray-800 dark:disabled:text-gray-400'
const labelClass = 'block text-sm font-medium text-gray-700 dark:text-gray-300'

function saveErrorMessage(err: unknown): string {
  if (err instanceof ApiRequestError) {
    const detail = err.apiError?.detail
    return detail ? `${err.apiError.error}: ${detail}` : err.apiError?.error || err.message
  }
  return err instanceof Error ? err.message : 'Failed to save network schema policy'
}

export default function NetworkSchemaPolicyCard() {
  const { policy, loading, error, canRead, canEdit, updatePolicy } = useNetworkSchemaPolicy()
  const { showToast } = useToast()
  // null means the form shows the stored policy; a draft is an unsaved edit.
  const [draft, setDraft] = useState<Draft | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  if (!canRead) return null

  const stored: Draft | null = policy
    ? { name: networkSchemaPolicyName(policy.name), duplicateScope: manualDuplicateScope(policy) }
    : null
  const form = draft ?? stored
  const dirty = !!draft && !!stored && (draft.name !== stored.name || (draft.name === 'manual' && draft.duplicateScope !== stored.duplicateScope))
  const disabled = !canEdit || saving
  const selected = form ? NETWORK_SCHEMA_POLICIES.find((option) => option.name === form.name) : undefined

  async function handleSave() {
    if (!form || !dirty || saving) return
    setSaving(true)
    setSaveError(null)
    try {
      await updatePolicy(networkSchemaPolicyRequest(form.name, form.duplicateScope))
      setDraft(null)
      showToast('Network schema policy saved', 'success')
    } catch (err) {
      setSaveError(saveErrorMessage(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="bg-white dark:bg-gray-800 rounded-xl shadow border dark:border-gray-700 p-6">
      <div className="mb-4">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Network Schema Policy</h2>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
          How Discovery decides which account, region, or relationship owns a CIDR, and when a repeated CIDR is a conflict.
          The Discovery network views start from this policy; a view can pick another one for a single request.
        </p>
      </div>

      {error && (
        <div className="mb-4 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
          {error}
        </div>
      )}

      {!form ? (
        loading && <p className="text-sm text-gray-500 dark:text-gray-400">Loading…</p>
      ) : (
        <div className="space-y-4">
          <div className="grid gap-4 md:grid-cols-2">
            <div>
              <label htmlFor="network-schema-policy" className={labelClass}>Policy</label>
              <select
                id="network-schema-policy"
                className={selectClass}
                value={form.name}
                disabled={disabled}
                onChange={(e) => setDraft({ ...form, name: e.target.value as NetworkSchemaPolicyName })}
              >
                {NETWORK_SCHEMA_POLICIES.map((option) => (
                  <option key={option.name} value={option.name}>{option.label}</option>
                ))}
              </select>
              {selected && <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">{selected.description}</p>}
            </div>

            {form.name === 'manual' && (
              <div>
                <label htmlFor="network-schema-duplicate-scope" className={labelClass}>Duplicate CIDRs</label>
                <select
                  id="network-schema-duplicate-scope"
                  className={selectClass}
                  value={form.duplicateScope}
                  disabled={disabled}
                  onChange={(e) => setDraft({ ...form, duplicateScope: e.target.value as ManualDuplicateScope })}
                >
                  <option value="manual">Flag only where relationships overlap</option>
                  <option value="global">Flag anywhere in the network</option>
                </select>
              </div>
            )}
          </div>

          {saveError && (
            <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
              {saveError}
            </div>
          )}

          {canEdit ? (
            <button
              type="button"
              onClick={handleSave}
              disabled={!dirty || saving}
              className="inline-flex items-center gap-2 px-3 py-2 rounded-lg bg-blue-600 text-white text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
            >
              {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
              {saving ? 'Saving…' : 'Save'}
            </button>
          ) : (
            <p className="text-sm text-gray-500 dark:text-gray-400">You need settings:write to change the policy.</p>
          )}
        </div>
      )}
    </section>
  )
}
