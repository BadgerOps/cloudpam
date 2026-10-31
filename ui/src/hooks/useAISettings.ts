import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiRequestError, get, patch, post } from '../api/client'
import type { AIProviderSettings, AIProviderTestResult, UpdateAIProviderSettingsRequest } from '../api/types'

const SETTINGS_PATH = '/api/v1/settings/ai'

export interface AISettingsError {
  status: number // 0 when the request never got an HTTP response
  error: string
  detail?: string
}

export type AISaveResult = { ok: true } | ({ ok: false } & AISettingsError)

function toError(err: unknown, fallback: string): AISettingsError {
  if (err instanceof ApiRequestError) {
    return { status: err.status, error: err.apiError?.error || err.message || fallback, detail: err.apiError?.detail }
  }
  return { status: 0, error: err instanceof Error ? err.message : fallback }
}

// useAISettings loads and edits the admin-managed AI provider settings.
// Pass hasPermission('settings:read') as enabled so the hook never issues a
// request the caller is not entitled to make. save and test keep separate
// pending flags, and a second call while one is pending is ignored (returns null).
export function useAISettings(enabled: boolean) {
  const [settings, setSettings] = useState<AIProviderSettings | null>(null)
  const [loading, setLoading] = useState(enabled)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<AISettingsError | null>(null)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<AIProviderTestResult | null>(null)
  const [testError, setTestError] = useState<string | null>(null)
  const savingRef = useRef(false)
  const testingRef = useRef(false)
  const loadSeq = useRef(0)

  const refresh = useCallback(async () => {
    const seq = ++loadSeq.current
    if (!enabled) {
      setSettings(null)
      setError(null)
      setLoading(false)
      return
    }
    try {
      setLoading(true)
      const data = await get<AIProviderSettings>(SETTINGS_PATH)
      if (seq !== loadSeq.current) return
      setSettings(data)
      setError(null)
    } catch (err) {
      if (seq !== loadSeq.current) return
      setError(toError(err, 'Failed to load AI provider settings').error)
    } finally {
      if (seq === loadSeq.current) setLoading(false)
    }
  }, [enabled])

  useEffect(() => { void refresh() }, [refresh])

  const save = useCallback(async (update: UpdateAIProviderSettingsRequest): Promise<AISaveResult | null> => {
    if (savingRef.current) return null
    savingRef.current = true
    setSaving(true)
    setSaveError(null)
    try {
      const data = await patch<AIProviderSettings>(SETTINGS_PATH, update)
      if (data) setSettings(data)
      // A test result describes the settings before this save.
      setTestResult(null)
      setTestError(null)
      await refresh()
      return { ok: true }
    } catch (err) {
      const e = toError(err, 'Failed to save AI provider settings')
      setSaveError(e)
      return { ok: false, ...e }
    } finally {
      savingRef.current = false
      setSaving(false)
    }
  }, [refresh])

  const clearStoredKey = useCallback(() => save({ clear_api_key: true }), [save])

  const test = useCallback(async (): Promise<AIProviderTestResult | null> => {
    if (testingRef.current) return null
    testingRef.current = true
    setTesting(true)
    setTestError(null)
    setTestResult(null)
    try {
      const result = await post<AIProviderTestResult>(`${SETTINGS_PATH}/test`, {})
      setTestResult(result)
      await refresh()
      return result
    } catch (err) {
      const e = toError(err, 'Failed to test the AI provider')
      setTestError(e.detail ? `${e.error}: ${e.detail}` : e.error)
      return null
    } finally {
      testingRef.current = false
      setTesting(false)
    }
  }, [refresh])

  return {
    settings,
    loading,
    error,
    refresh,
    save,
    clearStoredKey,
    test,
    saving,
    saveError,
    testing,
    testResult,
    testError,
  }
}
