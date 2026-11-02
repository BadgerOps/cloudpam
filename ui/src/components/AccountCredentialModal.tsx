import { useEffect, useId, useRef, useState } from 'react'
import { X } from 'lucide-react'
import { useAccountCredential } from '../hooks/useAccountCredential'
import type { CredentialRequestError } from '../hooks/useAccountCredential'
import { useToast } from '../hooks/useToast'
import StatusBadge from '../components/StatusBadge'
import type { Account, AccountCredential, CredentialTest, CredentialType, PutAccountCredentialRequest } from '../api/types'
import { formatTimeAgo } from '../utils/format'

export type CredentialProvider = 'aws' | 'gcp'

const CREDENTIAL_TYPE_LABELS: Record<CredentialType, string> = {
  aws_assume_role: 'AWS assumed role',
  gcp_service_account: 'GCP service account',
}

export function credentialProvider(account: Pick<Account, 'provider'>): CredentialProvider | null {
  return account.provider === 'aws' || account.provider === 'gcp' ? account.provider : null
}

type BadgeKey = 'none' | 'stored' | 'verified' | 'failing'

const STATE_BADGES: Record<BadgeKey, { label: string; className: string }> = {
  none: { label: 'None stored', className: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300' },
  stored: { label: 'Stored', className: 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300' },
  verified: { label: 'Verified', className: 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300' },
  failing: { label: 'Failing', className: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300' },
}

function badgeKey(credential: AccountCredential | null): BadgeKey {
  if (!credential) return 'none'
  if (!credential.last_test) return 'stored'
  return credential.last_test.status === 'ok' ? 'verified' : 'failing'
}

const KEY_ENV = 'CLOUDPAM_OIDC_ENCRYPTION_KEY'
const KEY_EPHEMERAL_STATUS = 409

const inputClass = 'w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100'
const labelClass = 'block text-xs font-medium text-gray-600 dark:text-gray-300 mb-1'
const codeClass = 'font-mono text-xs bg-gray-100 dark:bg-gray-900 px-1 py-0.5 rounded'

interface CredentialForm {
  role_arn: string
  external_id: string
  key_json: string
}

const EMPTY_FORM: CredentialForm = { role_arn: '', external_id: '', key_json: '' }

function toRequest(provider: CredentialProvider, form: CredentialForm): PutAccountCredentialRequest {
  if (provider === 'aws') {
    const external = form.external_id.trim()
    return { type: 'aws_assume_role', aws_assume_role: { role_arn: form.role_arn.trim(), ...(external ? { external_id: external } : {}) } }
  }
  return { type: 'gcp_service_account', gcp_service_account: { key_json: form.key_json } }
}

function SaveError({ error }: { error: CredentialRequestError }) {
  return (
    <div
      role="alert"
      data-testid="credential-save-error"
      className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm p-3 rounded space-y-1"
    >
      <p>{error.error}</p>
      {error.detail && <p className="text-xs">{error.detail}</p>}
      {error.status === KEY_EPHEMERAL_STATUS && (
        <p className="text-xs">
          Set <code className={codeClass}>{KEY_ENV}</code> to a 32-byte hex key on the server, restart it, then save again.
        </p>
      )}
    </div>
  )
}

function TestResult({ test, identity }: { test: CredentialTest; identity?: string }) {
  const ok = test.status === 'ok'
  return (
    <div
      data-testid="credential-test-result"
      className={`text-sm p-3 rounded border ${ok
        ? 'bg-green-50 dark:bg-green-900/30 border-green-200 dark:border-green-800 text-green-800 dark:text-green-300'
        : 'bg-red-50 dark:bg-red-900/30 border-red-200 dark:border-red-800 text-red-700 dark:text-red-300'}`}
    >
      <div className="flex items-center gap-2">
        <StatusBadge label={test.status} />
        <span className="text-xs text-gray-500 dark:text-gray-400">tested {formatTimeAgo(test.tested_at)}</span>
      </div>
      {ok && identity && <p className="mt-1 font-mono text-xs break-all">{identity}</p>}
      {!ok && <p className="mt-1">{test.error || 'The credential did not work.'}</p>}
    </div>
  )
}

function Summary({ credential }: { credential: AccountCredential }) {
  const s = credential.summary
  const rows: [string, string][] = []
  if (s.role_arn) rows.push(['Role ARN', s.role_arn])
  if (credential.type === 'aws_assume_role') rows.push(['External ID', s.has_external_id ? 'set' : 'not set'])
  if (s.client_email) rows.push(['Client email', s.client_email])
  if (s.project_id) rows.push(['Project ID', s.project_id])
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
      {rows.map(([label, value]) => (
        <div key={label} className="contents">
          <dt className="text-gray-500 dark:text-gray-400">{label}</dt>
          <dd className="font-mono text-xs break-all text-gray-900 dark:text-gray-100 self-center">{value}</dd>
        </div>
      ))}
      <dt className="text-gray-500 dark:text-gray-400">Updated</dt>
      <dd className="text-xs text-gray-900 dark:text-gray-100">{formatTimeAgo(credential.updated_at)}</dd>
    </dl>
  )
}

export default function AccountCredentialModal({
  account,
  provider,
  canEdit,
  returnFocusTo,
  onClose,
}: {
  account: Account
  provider: CredentialProvider
  canEdit: boolean
  returnFocusTo: HTMLElement | null
  onClose: () => void
}) {
  const { showToast } = useToast()
  const { state, saving, testing, clearing, testResult, save, test, clear } = useAccountCredential(account.id)
  const id = useId()
  const dialogRef = useRef<HTMLDivElement>(null)
  const [form, setForm] = useState<CredentialForm>(EMPTY_FORM)
  const [saveError, setSaveError] = useState<CredentialRequestError | null>(null)
  const busy = saving || testing || clearing
  const credential = state.kind === 'stored' ? state.credential : null
  const badge = STATE_BADGES[badgeKey(credential)]

  useEffect(() => {
    dialogRef.current?.querySelector<HTMLElement>('input, textarea, button')?.focus()
    return () => {
      if (returnFocusTo?.isConnected) returnFocusTo.focus()
    }
  }, [returnFocusTo])

  useEffect(() => {
    if (busy) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [busy, onClose])

  function set<K extends keyof CredentialForm>(field: K, value: CredentialForm[K]) {
    setForm(prev => ({ ...prev, [field]: value }))
    setSaveError(null)
  }

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    const result = await save(toRequest(provider, form))
    if (!result) return
    if (result.ok) {
      setForm(EMPTY_FORM)
      setSaveError(null)
      showToast('Credential saved', 'success')
    } else {
      setSaveError(result)
    }
  }

  async function handleTest() {
    const result = await test()
    if (!result) return
    if (!result.ok) showToast(result.error, 'error')
  }

  async function handleClear() {
    if (!confirm(`Clear the stored credential for "${account.name}"?`)) return
    const result = await clear()
    if (!result) return
    if (result.ok) {
      setSaveError(null)
      showToast('Credential cleared', 'success')
    } else {
      showToast(result.error, 'error')
    }
  }

  const shownTest = testResult ?? credential?.last_test ?? null

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={`${id}-title`}
        data-testid="account-credential-modal"
        className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-2xl mx-4 max-h-[90vh] overflow-auto"
      >
        <div className="flex items-center justify-between px-6 py-4 border-b dark:border-gray-700">
          <div className="flex items-center gap-3">
            <h2 id={`${id}-title`} className="text-lg font-semibold text-gray-900 dark:text-gray-100">Credentials for {account.name}</h2>
            <span data-testid="credential-state" className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${badge.className}`}>
              {badge.label}
            </span>
          </div>
          <button
            onClick={onClose}
            disabled={busy}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 disabled:opacity-50"
            title="Close"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="px-6 py-4 space-y-4">
          <p className="text-sm text-gray-600 dark:text-gray-400">
            The server's own sync uses this credential for <span className="font-mono">{account.key}</span> instead of its host identity.
            A connected agent keeps the agent's identity. The secret is encrypted with <code className={codeClass}>{KEY_ENV}</code> and is never shown again.
          </p>

          {state.kind === 'loading' && <p className="text-sm text-gray-400 dark:text-gray-500">Loading credential...</p>}
          {state.kind === 'error' && (
            <div role="alert" className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm p-3 rounded">
              {state.message}
            </div>
          )}
          {state.kind === 'none' && (
            <p className="text-sm text-gray-600 dark:text-gray-400">
              No credential stored. Sync runs as the server's host identity, or through a connected agent.
            </p>
          )}
          {credential && (
            <div className="bg-gray-50 dark:bg-gray-900 rounded p-3 space-y-2">
              <div className="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">{CREDENTIAL_TYPE_LABELS[credential.type]}</div>
              <Summary credential={credential} />
            </div>
          )}
          {shownTest && <TestResult test={shownTest} identity={testResult?.identity} />}

          {canEdit && credential && (
            <div className="flex gap-2">
              <button
                type="button"
                onClick={handleTest}
                disabled={busy}
                className="px-4 py-1.5 border dark:border-gray-600 text-sm rounded hover:bg-gray-50 dark:hover:bg-gray-700/50 dark:text-gray-300 disabled:opacity-50"
              >
                {testing ? 'Testing...' : 'Test'}
              </button>
              <button
                type="button"
                onClick={handleClear}
                disabled={busy}
                className="px-4 py-1.5 border border-red-200 dark:border-red-800 text-sm rounded text-red-700 dark:text-red-300 hover:bg-red-50 dark:hover:bg-red-900/30 disabled:opacity-50"
              >
                {clearing ? 'Clearing...' : 'Clear'}
              </button>
            </div>
          )}

          {canEdit && state.kind !== 'loading' && (
            <form aria-label={credential ? 'Replace credential' : 'Store credential'} onSubmit={handleSave} className="space-y-3 border-t dark:border-gray-700 pt-4">
              <div className="text-sm font-medium text-gray-900 dark:text-gray-100">
                {credential ? 'Replace the stored credential' : 'Store a credential'}
              </div>
              {saveError && <SaveError error={saveError} />}
              {provider === 'aws' ? (
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div>
                    <label htmlFor={`${id}-role-arn`} className={labelClass}>Role ARN (required)</label>
                    <input
                      id={`${id}-role-arn`}
                      required
                      value={form.role_arn}
                      onChange={e => set('role_arn', e.target.value)}
                      className={`${inputClass} font-mono`}
                      placeholder="arn:aws:iam::123456789012:role/CloudPAMDiscoveryRole"
                      autoComplete="off"
                    />
                  </div>
                  <div>
                    <label htmlFor={`${id}-external-id`} className={labelClass}>External ID</label>
                    <input
                      id={`${id}-external-id`}
                      value={form.external_id}
                      onChange={e => set('external_id', e.target.value)}
                      className={`${inputClass} font-mono`}
                      placeholder="optional, matched by the role's trust policy"
                      autoComplete="off"
                    />
                  </div>
                </div>
              ) : (
                <div>
                  <label htmlFor={`${id}-key-json`} className={labelClass}>Service account key JSON (required)</label>
                  <textarea
                    id={`${id}-key-json`}
                    required
                    value={form.key_json}
                    onChange={e => set('key_json', e.target.value)}
                    rows={8}
                    className={`${inputClass} font-mono`}
                    placeholder='{"type": "service_account", "project_id": "...", "client_email": "...", "private_key": "..."}'
                    spellCheck={false}
                  />
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Paste the whole key file. The service account needs <span className="font-mono">roles/compute.networkViewer</span> on the project.
                  </p>
                </div>
              )}
              <div className="flex gap-2">
                <button
                  type="submit"
                  disabled={busy}
                  className="px-4 py-1.5 bg-blue-600 text-white text-sm rounded hover:bg-blue-700 disabled:opacity-50"
                >
                  {saving ? 'Saving...' : 'Save'}
                </button>
                <button
                  type="button"
                  onClick={onClose}
                  disabled={busy}
                  className="px-4 py-1.5 border dark:border-gray-600 text-sm rounded hover:bg-gray-50 dark:hover:bg-gray-700/50 dark:text-gray-300"
                >
                  Close
                </button>
              </div>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}
