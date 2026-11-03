import { useEffect, useRef, useState } from 'react'
import { Loader2, PlugZap, Plus, Save, Trash2 } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'
import { useAISettings } from '../hooks/useAISettings'
import { useToast } from '../hooks/useToast'
import type {
  AIFallbackProviderUpdate,
  AIProviderKind,
  AIProviderSettings,
  AIProviderState,
  AISettingSource,
  UpdateAIProviderSettingsRequest,
} from '../api/types'

const STATE_BADGES: Record<AIProviderState, { label: string; className: string }> = {
  not_configured: { label: 'Not configured', className: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300' },
  configured: { label: 'Configured', className: 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300' },
  verified: { label: 'Verified', className: 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300' },
  failing: { label: 'Failing', className: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300' },
}

const PROVIDER_KINDS: { value: AIProviderKind; label: string; placeholder: string }[] = [
  { value: 'openai', label: 'OpenAI-compatible', placeholder: 'https://api.openai.com/v1' },
  { value: 'anthropic', label: 'Anthropic', placeholder: 'https://api.anthropic.com' },
]
const MAX_FALLBACKS = 5

const KEY_PROMPT = 'Enter the API key again for the new endpoint or provider, or clear the stored key'
const STORED_ENDPOINT_HINT = 'A stored endpoint or provider never uses CLOUDPAM_LLM_API_KEY; enter a key here or leave empty for an authless endpoint'
const ENV_KEY_WARNING =
  'Changing the endpoint or provider stops CLOUDPAM_LLM_API_KEY from applying; enter a key here or leave it empty for an authless endpoint.'
const ANTHROPIC_HINT = 'The Anthropic Messages API needs an API key. The temperature is not sent to Anthropic; the model samples with its default.'
const FALLBACKS_HINT = 'Tried in order when the provider above cannot be reached or answers a server error. Max tokens and temperature follow the primary.'
const TEST_HINT = 'Tests the saved settings, not unsaved edits.'
const STORAGE_UNAVAILABLE = 'Set CLOUDPAM_OIDC_ENCRYPTION_KEY to store an API key here; CLOUDPAM_LLM_API_KEY still works.'
const CLEAR_KEY_CONFIRM =
  'Clear the stored API key? This does not disable the AI provider: when the endpoint and provider come from the environment or the default, CLOUDPAM_LLM_API_KEY is used if it is set; a stored endpoint runs without a key.'

interface FallbackRow {
  id?: string
  provider: AIProviderKind
  endpoint: string
  model: string
  // The typed key lives only here until a successful save.
  apiKey: string
  keyConfigured: boolean
}

interface FormFields {
  provider: AIProviderKind
  endpoint: string
  model: string
  maxTokens: string
  temperature: string
  fallbacks: FallbackRow[]
}

function fieldsFrom(s: AIProviderSettings): FormFields {
  return {
    provider: s.provider ?? 'openai',
    endpoint: s.endpoint ?? '',
    model: s.model ?? '',
    maxTokens: String(s.max_tokens ?? ''),
    temperature: String(s.temperature ?? ''),
    fallbacks: (s.fallbacks ?? []).map(fb => ({
      id: fb.id,
      provider: fb.provider,
      endpoint: fb.endpoint ?? '',
      model: fb.model ?? '',
      apiKey: '',
      keyConfigured: fb.api_key_configured,
    })),
  }
}

function sameFallbacks(a: FallbackRow[], b: FallbackRow[]) {
  return a.length === b.length && a.every((row, i) => {
    const other = b[i]
    return row.id === other.id && row.provider === other.provider && row.endpoint.trim() === other.endpoint.trim() &&
      row.model.trim() === other.model.trim() && row.apiKey.trim() === other.apiKey.trim()
  })
}

function sameFields(a: FormFields, b: FormFields) {
  return a.provider === b.provider && a.endpoint === b.endpoint && a.model === b.model && a.maxTokens === b.maxTokens &&
    a.temperature === b.temperature && sameFallbacks(a.fallbacks, b.fallbacks)
}

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

function keyHelpText(s: AIProviderSettings) {
  if (!s.api_key_configured) return 'No key configured'
  return s.sources.api_key === 'database' ? 'Key configured (stored)' : 'Key configured (from environment)'
}

function placeholderFor(kind: AIProviderKind) {
  return PROVIDER_KINDS.find(k => k.value === kind)?.placeholder ?? ''
}

// buildUpdate diffs the form against the loaded settings and returns only the
// changed fields, or an error message when a value is out of range.
function buildUpdate(form: FormFields, base: FormFields): { update: UpdateAIProviderSettingsRequest; error?: string } {
  const update: UpdateAIProviderSettingsRequest = {}
  if (form.provider !== base.provider) update.provider = form.provider
  if (form.endpoint.trim() !== base.endpoint.trim()) update.endpoint = form.endpoint.trim()
  if (form.model.trim() !== base.model.trim()) update.model = form.model.trim()
  if (form.maxTokens.trim() !== base.maxTokens.trim()) {
    const raw = form.maxTokens.trim()
    if (raw === '') {
      update.max_tokens = 0
    } else {
      const n = Number(raw)
      if (!Number.isInteger(n) || n < 1 || n > 1_000_000) {
        return { update, error: 'Max tokens must be a whole number between 1 and 1000000' }
      }
      update.max_tokens = n
    }
  }
  if (form.temperature.trim() !== base.temperature.trim()) {
    const raw = form.temperature.trim()
    if (raw === '') {
      update.clear_temperature = true
    } else {
      const t = Number(raw)
      if (!Number.isFinite(t) || t < 0 || t > 2) {
        return { update, error: 'Temperature must be between 0 and 2' }
      }
      update.temperature = t
    }
  }
  if (!sameFallbacks(form.fallbacks, base.fallbacks)) {
    const fallbacks: AIFallbackProviderUpdate[] = []
    for (const [i, row] of form.fallbacks.entries()) {
      const key = row.apiKey.trim()
      const hasKey = key !== '' || row.keyConfigured
      if (row.provider === 'anthropic' && !hasKey) {
        return { update, error: `Fallback ${i + 1}: Anthropic needs an API key` }
      }
      if (row.provider === 'openai' && !hasKey && row.endpoint.trim() === '') {
        return { update, error: `Fallback ${i + 1}: enter an API key, or an endpoint for an authless server` }
      }
      const entry: AIFallbackProviderUpdate = { provider: row.provider, endpoint: row.endpoint.trim(), model: row.model.trim() }
      if (row.id) entry.id = row.id
      if (key) entry.api_key = key
      fallbacks.push(entry)
    }
    update.fallbacks = fallbacks
  }
  return { update }
}

const inputClass =
  'mt-1 w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 disabled:cursor-not-allowed disabled:bg-gray-100 disabled:text-gray-500 dark:border-gray-600 dark:bg-gray-900 dark:text-white dark:disabled:bg-gray-800 dark:disabled:text-gray-400'
const labelClass = 'block text-sm font-medium text-gray-700 dark:text-gray-300'

function SourceTag({ field, source }: { field: string; source: AISettingSource }) {
  return (
    <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
      Source:{' '}
      <span data-testid={`ai-source-${field}`} className="rounded bg-gray-100 px-1.5 py-0.5 font-mono text-gray-700 dark:bg-gray-700 dark:text-gray-300">
        {source}
      </span>
    </p>
  )
}

function ProviderSelect({ id, label, value, disabled, onChange }: {
  id?: string
  label?: string
  value: AIProviderKind
  disabled: boolean
  onChange: (kind: AIProviderKind) => void
}) {
  return (
    <select
      id={id}
      aria-label={label}
      className={inputClass}
      value={value}
      disabled={disabled}
      onChange={e => onChange(e.target.value as AIProviderKind)}
    >
      {PROVIDER_KINDS.map(k => (
        <option key={k.value} value={k.value}>{k.label}</option>
      ))}
    </select>
  )
}

export default function AIProviderSettingsCard() {
  const { hasPermission } = useAuth()
  const canRead = hasPermission('settings:read')
  const canWrite = hasPermission('settings:write')
  const { settings, loading, error, save, clearStoredKey, test, saving, saveError, testing, testResult, testError } =
    useAISettings(canRead)
  const { showToast } = useToast()

  const [form, setForm] = useState<FormFields | null>(null)
  const [base, setBase] = useState<FormFields | null>(null)
  // The typed key lives only here and is cleared after a successful save.
  const [apiKey, setApiKey] = useState('')
  const [clearKeyOnSave, setClearKeyOnSave] = useState(false)
  const [keyPrompt, setKeyPrompt] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const resync = useRef(false)

  // Load server values into the form unless the user has unsaved edits.
  useEffect(() => {
    if (!settings) return
    const next = fieldsFrom(settings)
    const dirty = form && base && !sameFields(form, base)
    if (!dirty || resync.current) {
      resync.current = false
      setForm(next)
      setBase(next)
    }
    // Only a new server snapshot should resync; form edits must not.
  }, [settings])

  if (!canRead) return null

  const busy = saving || testing
  const formDisabled = !canWrite || busy
  const keyFromDatabase = settings?.sources.api_key === 'database'
  const hostMoves = !!form && !!base && (form.endpoint.trim() !== base.endpoint.trim() || form.provider !== base.provider)
  const envKeyWarning = settings?.sources.api_key === 'environment' && hostMoves

  function setFallback(i: number, patch: Partial<FallbackRow>) {
    if (!form) return
    setForm({ ...form, fallbacks: form.fallbacks.map((row, j) => (j === i ? { ...row, ...patch } : row)) })
  }

  function addFallback() {
    if (!form || form.fallbacks.length >= MAX_FALLBACKS) return
    setForm({ ...form, fallbacks: [...form.fallbacks, { provider: 'openai', endpoint: '', model: '', apiKey: '', keyConfigured: false }] })
  }

  function removeFallback(i: number) {
    if (!form) return
    setForm({ ...form, fallbacks: form.fallbacks.filter((_, j) => j !== i) })
  }

  async function handleSave() {
    if (!settings || !form || !base || busy) return
    setFormError(null)
    const { update, error: validationError } = buildUpdate(form, base)
    if (validationError) {
      setFormError(validationError)
      return
    }
    const key = apiKey.trim()
    if (key) update.api_key = key
    else if (clearKeyOnSave) update.clear_api_key = true
    if ((update.endpoint !== undefined || update.provider !== undefined) && keyFromDatabase && !update.api_key && !update.clear_api_key) {
      setKeyPrompt(true)
      return
    }
    if (Object.keys(update).length === 0) {
      setFormError('No changes to save')
      return
    }
    resync.current = true
    const result = await save(update)
    if (result?.ok) {
      setApiKey('')
      setClearKeyOnSave(false)
      setKeyPrompt(false)
      showToast('AI provider settings saved', 'success')
    } else {
      resync.current = false
    }
  }

  async function handleClearKey() {
    if (busy || !window.confirm(CLEAR_KEY_CONFIRM)) return
    const result = await clearStoredKey()
    if (result?.ok) {
      setKeyPrompt(false)
      setClearKeyOnSave(false)
      showToast('Stored API key cleared', 'success')
    }
  }

  // A rejected endpoint or provider change, or a key the server could not
  // store, belongs under the key field; everything else goes in the general
  // error block.
  const keyFieldError =
    saveError && (saveError.status === 409 || (saveError.status === 400 && /api_key/.test(saveError.detail ?? '') && !/fallback/.test(saveError.detail ?? '')))
      ? saveError.detail || saveError.error
      : null
  const generalSaveError =
    saveError && !keyFieldError ? (saveError.detail ? `${saveError.error}: ${saveError.detail}` : saveError.error) : null

  const badge = settings ? STATE_BADGES[settings.state] ?? STATE_BADGES.not_configured : null
  const showHeaderError = settings?.last_test_error && !(testResult && !testResult.ok)

  return (
    <section className="bg-white dark:bg-gray-800 rounded-xl shadow border dark:border-gray-700 p-6">
      <div className="flex flex-wrap items-start justify-between gap-4 mb-4">
        <div>
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white">AI Provider</h2>
            {badge && (
              <span data-testid="ai-provider-state" className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${badge.className}`}>
                {badge.label}
              </span>
            )}
          </div>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Provider for AI planning: any OpenAI-compatible API or the Anthropic Messages API. Saved values override the CLOUDPAM_LLM_* environment variables.
          </p>
          {settings?.last_tested_at && (
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Last tested {formatDate(settings.last_tested_at)}
              {settings.last_test_model ? ` · ${settings.last_test_model}` : ''}
            </p>
          )}
        </div>
      </div>

      {showHeaderError && (
        <div className="mb-4 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
          {settings?.last_test_error}
        </div>
      )}

      {error && (
        <div className="mb-4 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
          {error}
        </div>
      )}

      {!settings || !form ? (
        loading && <p className="text-sm text-gray-500 dark:text-gray-400">Loading…</p>
      ) : (
        <div className="space-y-4">
          <div className="grid gap-4 md:grid-cols-2">
            <div>
              <label htmlFor="ai-provider" className={labelClass}>Provider</label>
              <ProviderSelect
                id="ai-provider"
                value={form.provider}
                disabled={formDisabled}
                onChange={provider => setForm({ ...form, provider })}
              />
              <SourceTag field="provider" source={settings.sources.provider} />
              {form.provider === 'anthropic' && (
                <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">{ANTHROPIC_HINT}</p>
              )}
            </div>

            <div>
              <label htmlFor="ai-endpoint" className={labelClass}>Endpoint</label>
              <input
                id="ai-endpoint"
                type="url"
                className={inputClass}
                placeholder={placeholderFor(form.provider)}
                value={form.endpoint}
                disabled={formDisabled}
                onChange={e => setForm({ ...form, endpoint: e.target.value })}
              />
              <SourceTag field="endpoint" source={settings.sources.endpoint} />
            </div>

            <div>
              <label htmlFor="ai-model" className={labelClass}>Model</label>
              <input
                id="ai-model"
                type="text"
                className={inputClass}
                value={form.model}
                disabled={formDisabled}
                onChange={e => setForm({ ...form, model: e.target.value })}
              />
              <SourceTag field="model" source={settings.sources.model} />
            </div>

            <div>
              <label htmlFor="ai-api-key" className={labelClass}>API key</label>
              <input
                id="ai-api-key"
                type="password"
                autoComplete="new-password"
                className={inputClass}
                placeholder={settings.api_key_configured ? 'Enter a new key to replace it' : ''}
                value={apiKey}
                disabled={formDisabled || !settings.api_key_storage_available}
                onChange={e => {
                  setApiKey(e.target.value)
                  if (e.target.value) setKeyPrompt(false)
                }}
              />
              <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">{keyHelpText(settings)}</p>
              <SourceTag field="api_key" source={settings.sources.api_key} />
              {!settings.api_key_storage_available && (
                <p className="mt-1 text-xs text-amber-700 dark:text-amber-300">{STORAGE_UNAVAILABLE}</p>
              )}
              {(settings.sources.endpoint === 'database' || settings.sources.provider === 'database') && settings.sources.api_key === 'none' && (
                <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">{STORED_ENDPOINT_HINT}</p>
              )}
              {envKeyWarning && (
                <p className="mt-1 text-xs text-amber-700 dark:text-amber-300">{ENV_KEY_WARNING}</p>
              )}
              {keyPrompt && (
                <p className="mt-1 text-xs text-red-600 dark:text-red-400">{KEY_PROMPT}</p>
              )}
              {canWrite && (keyPrompt || clearKeyOnSave) && (
                <label className="mt-1 flex items-center gap-2 text-xs text-gray-700 dark:text-gray-300">
                  <input
                    type="checkbox"
                    checked={clearKeyOnSave}
                    disabled={formDisabled}
                    onChange={e => {
                      setClearKeyOnSave(e.target.checked)
                      if (e.target.checked) setKeyPrompt(false)
                    }}
                  />
                  Clear the stored key with this save
                </label>
              )}
              {keyFieldError && (
                <p className="mt-1 text-xs text-red-600 dark:text-red-400">{keyFieldError}</p>
              )}
            </div>

            <div>
              <label htmlFor="ai-max-tokens" className={labelClass}>Max tokens</label>
              <input
                id="ai-max-tokens"
                type="number"
                min={1}
                step={1}
                className={inputClass}
                value={form.maxTokens}
                disabled={formDisabled}
                onChange={e => setForm({ ...form, maxTokens: e.target.value })}
              />
              <SourceTag field="max_tokens" source={settings.sources.max_tokens} />
            </div>

            <div>
              <label htmlFor="ai-temperature" className={labelClass}>Temperature</label>
              <input
                id="ai-temperature"
                type="number"
                min={0}
                max={2}
                step={0.1}
                className={inputClass}
                value={form.temperature}
                disabled={formDisabled}
                onChange={e => setForm({ ...form, temperature: e.target.value })}
              />
              <SourceTag field="temperature" source={settings.sources.temperature} />
            </div>
          </div>

          <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <h3 className="text-sm font-semibold text-gray-900 dark:text-white">Fallback providers</h3>
                <p className="text-xs text-gray-500 dark:text-gray-400">{FALLBACKS_HINT}</p>
              </div>
              {canWrite && (
                <button
                  type="button"
                  onClick={addFallback}
                  disabled={formDisabled || form.fallbacks.length >= MAX_FALLBACKS}
                  className="inline-flex items-center gap-1 px-2 py-1 rounded-lg border border-gray-300 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                >
                  <Plus className="w-3.5 h-3.5" />
                  Add fallback
                </button>
              )}
            </div>
            {form.fallbacks.length === 0 ? (
              <p className="mt-2 text-xs text-gray-500 dark:text-gray-400">No fallback providers configured.</p>
            ) : (
              <ol className="mt-3 space-y-3">
                {form.fallbacks.map((row, i) => {
                  const n = i + 1
                  return (
                    <li key={row.id ?? `new-${i}`} data-testid={`ai-fallback-${n}`} className="rounded-lg border border-gray-200 dark:border-gray-700 p-3">
                      <div className="grid gap-3 md:grid-cols-4">
                        <div>
                          <span className={labelClass}>Provider</span>
                          <ProviderSelect
                            label={`Fallback ${n} provider`}
                            value={row.provider}
                            disabled={formDisabled}
                            onChange={provider => setFallback(i, { provider })}
                          />
                        </div>
                        <div>
                          <span className={labelClass}>Endpoint</span>
                          <input
                            type="url"
                            aria-label={`Fallback ${n} endpoint`}
                            className={inputClass}
                            placeholder={placeholderFor(row.provider)}
                            value={row.endpoint}
                            disabled={formDisabled}
                            onChange={e => setFallback(i, { endpoint: e.target.value })}
                          />
                        </div>
                        <div>
                          <span className={labelClass}>Model</span>
                          <input
                            type="text"
                            aria-label={`Fallback ${n} model`}
                            className={inputClass}
                            value={row.model}
                            disabled={formDisabled}
                            onChange={e => setFallback(i, { model: e.target.value })}
                          />
                        </div>
                        <div>
                          <span className={labelClass}>API key</span>
                          <input
                            type="password"
                            autoComplete="new-password"
                            aria-label={`Fallback ${n} API key`}
                            className={inputClass}
                            placeholder={row.keyConfigured ? 'Key stored; enter a new key to replace it' : ''}
                            value={row.apiKey}
                            disabled={formDisabled || !settings.api_key_storage_available}
                            onChange={e => setFallback(i, { apiKey: e.target.value })}
                          />
                        </div>
                      </div>
                      {canWrite && (
                        <button
                          type="button"
                          onClick={() => removeFallback(i)}
                          disabled={formDisabled}
                          className="mt-2 inline-flex items-center gap-1 text-xs text-red-700 hover:underline disabled:opacity-50 dark:text-red-300"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                          Remove fallback {n}
                        </button>
                      )}
                    </li>
                  )
                })}
              </ol>
            )}
          </div>

          {(formError || generalSaveError) && (
            <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
              {formError || generalSaveError}
            </div>
          )}

          {canWrite ? (
            <div className="flex flex-wrap items-center gap-3">
              <button
                type="button"
                onClick={handleSave}
                disabled={busy}
                className="inline-flex items-center gap-2 px-3 py-2 rounded-lg bg-blue-600 text-white text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
              >
                {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                {saving ? 'Saving…' : 'Save'}
              </button>
              <button
                type="button"
                onClick={() => { if (!saving) void test() }}
                disabled={busy}
                aria-describedby="ai-test-hint"
                className="inline-flex items-center gap-2 px-3 py-2 rounded-lg border border-gray-300 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
              >
                {testing ? <Loader2 className="w-4 h-4 animate-spin" /> : <PlugZap className="w-4 h-4" />}
                {testing ? 'Testing…' : 'Test connection'}
              </button>
              <span id="ai-test-hint" className="text-xs text-gray-500 dark:text-gray-400">{TEST_HINT}</span>
              {keyFromDatabase && (
                <button
                  type="button"
                  onClick={handleClearKey}
                  disabled={busy}
                  className="inline-flex items-center gap-2 px-3 py-2 rounded-lg border border-red-300 text-sm font-medium text-red-700 hover:bg-red-50 disabled:opacity-50 dark:border-red-800 dark:text-red-300 dark:hover:bg-red-950/30"
                >
                  <Trash2 className="w-4 h-4" />
                  Clear stored key
                </button>
              )}
            </div>
          ) : (
            <p className="text-sm text-gray-500 dark:text-gray-400">You need settings:write to change the provider.</p>
          )}

          {testResult && (
            testResult.ok ? (
              <div className="rounded-lg bg-green-50 px-3 py-2 text-sm text-green-700 dark:bg-green-950/30 dark:text-green-300">
                {`Reachable · model ${testResult.model} · streaming ${testResult.streaming_supported ? 'yes' : 'no'} · ${testResult.latency_ms} ms`}
              </div>
            ) : (
              <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
                {testResult.error || 'Connection test failed'}
              </div>
            )
          )}
          {testResult?.fallbacks && testResult.fallbacks.length > 0 && (
            <ul className="space-y-1" aria-label="Fallback test results">
              {testResult.fallbacks.map((fb, i) => (
                <li
                  key={fb.id}
                  className={`rounded-lg px-3 py-2 text-sm ${fb.ok
                    ? 'bg-green-50 text-green-700 dark:bg-green-950/30 dark:text-green-300'
                    : 'border border-red-200 bg-red-50 text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300'}`}
                >
                  {fb.ok
                    ? `Fallback ${i + 1} · ${fb.provider} ${fb.model} · reachable · ${fb.latency_ms} ms`
                    : `Fallback ${i + 1} · ${fb.provider} ${fb.model} · ${fb.error || 'failed'}`}
                </li>
              ))}
            </ul>
          )}
          {testError && (
            <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
              {testError}
            </div>
          )}
        </div>
      )}
    </section>
  )
}
