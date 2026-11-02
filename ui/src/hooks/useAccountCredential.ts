import { useCallback, useEffect, useRef, useState } from 'react'
import {
  ApiRequestError,
  deleteAccountCredential,
  getAccountCredential,
  putAccountCredential,
  testAccountCredential,
} from '../api/client'
import type { AccountCredential, CredentialTestResult, PutAccountCredentialRequest } from '../api/types'

export type CredentialState =
  | { kind: 'loading' }
  | { kind: 'none' }
  | { kind: 'stored'; credential: AccountCredential }
  | { kind: 'error'; message: string }

export interface CredentialRequestError {
  status: number // 0 when the request never got an HTTP response
  error: string
  detail?: string
}

export type CredentialActionResult<T = undefined> = { ok: true; value: T } | ({ ok: false } & CredentialRequestError)

function toError(err: unknown, fallback: string): CredentialRequestError {
  if (err instanceof ApiRequestError) {
    return { status: err.status, error: err.apiError?.error || err.message || fallback, detail: err.apiError?.detail }
  }
  return { status: 0, error: err instanceof Error ? err.message : fallback }
}

// useAccountCredential loads and edits the stored discovery credential of one account.
// A 404 from GET is the `none` state, not an error. save, test and clear each ignore
// a second call while one is pending (they return null).
export function useAccountCredential(accountId: number) {
  const [state, setState] = useState<CredentialState>({ kind: 'loading' })
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [clearing, setClearing] = useState(false)
  const [testResult, setTestResult] = useState<CredentialTestResult | null>(null)
  const busy = useRef(false)
  const loadSeq = useRef(0)

  const refresh = useCallback(async () => {
    const seq = ++loadSeq.current
    try {
      const credential = await getAccountCredential(accountId)
      if (seq !== loadSeq.current) return
      setState({ kind: 'stored', credential })
    } catch (err) {
      if (seq !== loadSeq.current) return
      const e = toError(err, 'Failed to load the credential')
      setState(e.status === 404 ? { kind: 'none' } : { kind: 'error', message: e.error })
    }
  }, [accountId])

  useEffect(() => {
    setState({ kind: 'loading' })
    setTestResult(null)
    void refresh()
  }, [refresh])

  const save = useCallback(async (req: PutAccountCredentialRequest): Promise<CredentialActionResult | null> => {
    if (busy.current) return null
    busy.current = true
    setSaving(true)
    try {
      const credential = await putAccountCredential(accountId, req)
      // A test result describes the credential before this save.
      setTestResult(null)
      setState({ kind: 'stored', credential })
      return { ok: true, value: undefined }
    } catch (err) {
      return { ok: false, ...toError(err, 'Failed to save the credential') }
    } finally {
      busy.current = false
      setSaving(false)
    }
  }, [accountId])

  const test = useCallback(async (): Promise<CredentialActionResult<CredentialTestResult> | null> => {
    if (busy.current) return null
    busy.current = true
    setTesting(true)
    setTestResult(null)
    try {
      const result = await testAccountCredential(accountId)
      setTestResult(result)
      await refresh()
      return { ok: true, value: result }
    } catch (err) {
      return { ok: false, ...toError(err, 'Failed to test the credential') }
    } finally {
      busy.current = false
      setTesting(false)
    }
  }, [accountId, refresh])

  const clear = useCallback(async (): Promise<CredentialActionResult | null> => {
    if (busy.current) return null
    busy.current = true
    setClearing(true)
    try {
      await deleteAccountCredential(accountId)
      setTestResult(null)
      setState({ kind: 'none' })
      return { ok: true, value: undefined }
    } catch (err) {
      const e = toError(err, 'Failed to clear the credential')
      if (e.status === 404) {
        setState({ kind: 'none' })
        return { ok: true, value: undefined }
      }
      return { ok: false, ...e }
    } finally {
      busy.current = false
      setClearing(false)
    }
  }, [accountId])

  return { state, saving, testing, clearing, testResult, refresh, save, test, clear }
}
