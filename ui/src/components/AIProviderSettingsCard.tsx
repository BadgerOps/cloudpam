import { useEffect, useRef, useState } from 'react'
import { Loader2, PlugZap, Save, Trash2 } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'
import { useAISettings } from '../hooks/useAISettings'
import { useToast } from '../hooks/useToast'
import type { AIProviderSettings, AIProviderState, AISettingSource, UpdateAIProviderSettingsRequest } from '../api/types'

const STATE_BADGES: Record<AIProviderState, { label: string; className: string }> = {
  not_configured: { label: 'Not configured', className: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300' },
  configured: { label: 'Configured', className: 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300' },
  verified: { label: 'Verified', className: 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300' },
  failing: { label: 'Failing', className: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300' },
}

const KEY_PROMPT = 'Enter the API key again for the new endpoint, or clear the stored key'
const STORED_ENDPOINT_HINT = 'A stored endpoint never uses CLOUDPAM_LLM_API_KEY; enter a key here or leave empty for an authless endpoint'
const STORAGE_UNAVAILABLE = 'Set CLOUDPAM_OIDC_ENCRYPTION_KEY to store an API key here; CLOUDPAM_LLM_API_KEY still works.'
const CLEAR_KEY_CONFIRM =
  'Clear the stored API key? This does not disable the AI provider: when the endpoint comes from CLOUDPAM_LLM_ENDPOINT or the default, CLOUDPAM_LLM_API_KEY is used if it is set; a stored endpoint runs without a key.'

interface FormFields {
  endpoint: string
  model: string
  maxTokens: string
  temperature: string
}

function fieldsFrom(s: AIProviderSettings): FormFields {
  return {
    endpoint: s.endpoint ?? '',
    model: s.model ?? '',
    maxTokens: String(s.max_tokens ?? ''),
    temperature: String(s.temperature ?? ''),
  }
}

function sameFields(a: FormFields, b: FormFields) {
  return a.endpoint === b.endpoint && a.model === b.model && a.maxTokens === b.maxTokens && a.temperature === b.temperature
}

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

function keyHelpText(s: AIProviderSettings) {
  if (!s.api_key_configured) return 'No key configured'
  return s.sources.api_key === 'database' ? 'Key configured (stored)' : 'Key configured (from environment)'
}

// buildUpdate diffs the form against the loaded settings and returns only the
// changed fields, or an error message when a value is out of range.
function buildUpdate(form: FormFields, base: FormFields): { update: UpdateAIProviderSettingsRequest; error?: string } {
  const update: UpdateAIProviderSettingsRequest = {}
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

  async function handleSave() {
    if (!settings || !form || !base || saving) return
    setFormError(null)
    const { update, error: validationError } = buildUpdate(form, base)
    if (validationError) {
      setFormError(validationError)
      return
    }
    const key = apiKey.trim()
    if (key) update.api_key = key
    else if (clearKeyOnSave) update.clear_api_key = true
    if (update.endpoint !== undefined && keyFromDatabase && !update.api_key && !update.clear_api_key) {
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
    if (saving || !window.confirm(CLEAR_KEY_CONFIRM)) return
    const result = await clearStoredKey()
    if (result?.ok) showToast('Stored API key cleared', 'success')
  }

  // A rejected endpoint change, or a key the server could not store, belongs
  // under the key field; everything else goes in the general error block.
  const keyFieldError =
    saveError && (saveError.status === 409 || (saveError.status === 400 && /api_key/.test(saveError.detail ?? '')))
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
            OpenAI-compatible endpoint for AI planning. Saved values override the CLOUDPAM_LLM_* environment variables.
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
            <div className="md:col-span-2">
              <label htmlFor="ai-endpoint" className={labelClass}>Endpoint</label>
              <input
                id="ai-endpoint"
                type="url"
                className={inputClass}
                placeholder="https://api.openai.com/v1"
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
              {settings.sources.endpoint === 'database' && settings.sources.api_key === 'none' && (
                <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">{STORED_ENDPOINT_HINT}</p>
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
                disabled={saving}
                className="inline-flex items-center gap-2 px-3 py-2 rounded-lg bg-blue-600 text-white text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
              >
                {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                {saving ? 'Saving…' : 'Save'}
              </button>
              <button
                type="button"
                onClick={() => { void test() }}
                disabled={testing}
                className="inline-flex items-center gap-2 px-3 py-2 rounded-lg border border-gray-300 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
              >
                {testing ? <Loader2 className="w-4 h-4 animate-spin" /> : <PlugZap className="w-4 h-4" />}
                {testing ? 'Testing…' : 'Test connection'}
              </button>
              {keyFromDatabase && (
                <button
                  type="button"
                  onClick={handleClearKey}
                  disabled={saving}
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
