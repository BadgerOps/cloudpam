import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAISettings } from '../hooks/useAISettings'
import type { AIProviderSettings, AIProviderTestResult } from '../api/types'

function settings(overrides: Partial<AIProviderSettings> = {}): AIProviderSettings {
  return {
    endpoint: 'https://api.openai.com/v1',
    model: 'gpt-4o',
    max_tokens: 4096,
    temperature: 0.7,
    api_key_configured: false,
    api_key_storage_available: true,
    sources: { endpoint: 'default', model: 'default', api_key: 'none', max_tokens: 'default', temperature: 'default' },
    state: 'not_configured',
    ...overrides,
  }
}

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

interface Call { url: string; method: string; body: unknown }

const fetchMock = vi.fn()
const calls: Call[] = []

function record(input: RequestInfo | URL, init?: RequestInit): Call {
  const call = {
    url: String(input),
    method: init?.method ?? 'GET',
    body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined,
  }
  calls.push(call)
  return call
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => { resolve = res })
  return { promise, resolve }
}

describe('useAISettings', () => {
  beforeEach(() => {
    calls.length = 0
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('loads the settings on mount when enabled', async () => {
    fetchMock.mockImplementation(async (input, init) => {
      record(input, init)
      return json(200, settings({ state: 'configured' }))
    })
    const { result } = renderHook(() => useAISettings(true))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(calls).toEqual([{ url: '/api/v1/settings/ai', method: 'GET', body: undefined }])
    expect(result.current.settings?.state).toBe('configured')
  })

  it('does not fetch when disabled', async () => {
    const { result } = renderHook(() => useAISettings(false))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).not.toHaveBeenCalled()
    expect(result.current.settings).toBeNull()
  })

  it('save PATCHes exactly the given fields and refreshes from GET', async () => {
    let gets = 0
    fetchMock.mockImplementation(async (input, init) => {
      const call = record(input, init)
      if (call.method === 'PATCH') return json(200, settings({ model: 'llama3' }))
      gets++
      return json(200, settings({ model: gets > 1 ? 'llama3' : 'gpt-4o', state: gets > 1 ? 'configured' : 'not_configured' }))
    })
    const { result } = renderHook(() => useAISettings(true))
    await waitFor(() => expect(result.current.loading).toBe(false))

    let outcome
    await act(async () => {
      outcome = await result.current.save({ model: 'llama3' })
    })
    expect(outcome).toEqual({ ok: true })
    expect(calls.map(c => `${c.method} ${c.url}`)).toEqual([
      'GET /api/v1/settings/ai',
      'PATCH /api/v1/settings/ai',
      'GET /api/v1/settings/ai',
    ])
    expect(calls[1].body).toEqual({ model: 'llama3' })
    await waitFor(() => expect(result.current.settings?.state).toBe('configured'))
    expect(result.current.settings?.model).toBe('llama3')
  })

  it('clearStoredKey sends clear_api_key: true', async () => {
    fetchMock.mockImplementation(async (input, init) => {
      record(input, init)
      return json(200, settings())
    })
    const { result } = renderHook(() => useAISettings(true))
    await waitFor(() => expect(result.current.loading).toBe(false))
    await act(async () => {
      await result.current.clearStoredKey()
    })
    const patchCall = calls.find(c => c.method === 'PATCH')
    expect(patchCall?.body).toEqual({ clear_api_key: true })
  })

  it('test POSTs and stores the result, then refreshes', async () => {
    const testResult: AIProviderTestResult = {
      ok: true,
      state: 'verified',
      endpoint: 'https://api.openai.com/v1',
      model: 'gpt-4o',
      latency_ms: 120,
      streaming_supported: true,
      tested_at: '2026-10-30T12:00:00Z',
    }
    fetchMock.mockImplementation(async (input, init) => {
      const call = record(input, init)
      if (call.method === 'POST') return json(200, testResult)
      return json(200, settings())
    })
    const { result } = renderHook(() => useAISettings(true))
    await waitFor(() => expect(result.current.loading).toBe(false))
    await act(async () => {
      await result.current.test()
    })
    expect(calls.map(c => `${c.method} ${c.url}`)).toEqual([
      'GET /api/v1/settings/ai',
      'POST /api/v1/settings/ai/test',
      'GET /api/v1/settings/ai',
    ])
    expect(calls[1].body).toEqual({})
    expect(result.current.testResult).toEqual(testResult)
    expect(result.current.testing).toBe(false)
  })

  it('ignores a second save while the first is pending', async () => {
    const pending = deferred<Response>()
    fetchMock.mockImplementation(async (input, init) => {
      const call = record(input, init)
      if (call.method === 'PATCH') return pending.promise
      return json(200, settings())
    })
    const { result } = renderHook(() => useAISettings(true))
    await waitFor(() => expect(result.current.loading).toBe(false))

    let first: Promise<unknown> = Promise.resolve()
    let second: unknown
    await act(async () => {
      first = result.current.save({ model: 'a' })
      second = await result.current.save({ model: 'b' })
    })
    expect(second).toBeNull()
    expect(result.current.saving).toBe(true)
    await act(async () => {
      pending.resolve(json(200, settings()))
      await first
    })
    expect(calls.filter(c => c.method === 'PATCH')).toHaveLength(1)
    expect(calls.find(c => c.method === 'PATCH')?.body).toEqual({ model: 'a' })
    expect(result.current.saving).toBe(false)
  })

  it('surfaces a 409 from save with its status and server message', async () => {
    const message = 'cannot store an api key: CLOUDPAM_OIDC_ENCRYPTION_KEY is not set'
    fetchMock.mockImplementation(async (input, init) => {
      const call = record(input, init)
      if (call.method === 'PATCH') return json(409, { error: message })
      return json(200, settings())
    })
    const { result } = renderHook(() => useAISettings(true))
    await waitFor(() => expect(result.current.loading).toBe(false))
    let outcome
    await act(async () => {
      outcome = await result.current.save({ api_key: 'sk-test' })
    })
    expect(outcome).toEqual({ ok: false, status: 409, error: message, detail: undefined })
    expect(result.current.saveError).toEqual({ status: 409, error: message, detail: undefined })
    // No refresh after a failed save.
    expect(calls.filter(c => c.method === 'GET')).toHaveLength(1)
  })
})
