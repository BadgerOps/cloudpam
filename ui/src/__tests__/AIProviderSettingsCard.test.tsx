import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AIProviderSettingsCard from '../components/AIProviderSettingsCard'
import type { AIProviderSettings, AIProviderTestResult } from '../api/types'

const mockUseAuth = vi.fn()
vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))

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
const calls: Call[] = []
const fetchMock = vi.fn()

type Handler = (call: Call) => Response | Promise<Response>

function serve(current: AIProviderSettings, handlers: { patch?: Handler; post?: Handler } = {}) {
  fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = {
      url: String(input),
      method: init?.method ?? 'GET',
      body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined,
    }
    calls.push(call)
    if (call.method === 'PATCH' && handlers.patch) return handlers.patch(call)
    if (call.method === 'POST' && handlers.post) return handlers.post(call)
    return json(200, current)
  })
}

function asUser(perms: string[]) {
  mockUseAuth.mockReturnValue({ hasPermission: (p: string) => perms.includes(p) })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => { resolve = res })
  return { promise, resolve }
}

async function renderCard() {
  const view = render(<AIProviderSettingsCard />)
  await screen.findByLabelText('Endpoint')
  await waitFor(() => expect((screen.getByLabelText('Model') as HTMLInputElement).value).not.toBe(''))
  return view
}

function input(label: string) {
  return screen.getByLabelText(label) as HTMLInputElement
}

const patches = () => calls.filter(c => c.method === 'PATCH')

const KEY_PROMPT_TEXT = 'Enter the API key again for the new endpoint, or clear the stored key'

describe('AIProviderSettingsCard', () => {
  beforeEach(() => {
    calls.length = 0
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
    asUser(['settings:read', 'settings:write'])
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it.each([
    ['not_configured', 'Not configured'],
    ['configured', 'Configured'],
    ['verified', 'Verified'],
    ['failing', 'Failing'],
  ] as const)('shows the %s lifecycle badge', async (state, label) => {
    serve(settings({ state }))
    await renderCard()
    expect(screen.getByTestId('ai-provider-state').textContent).toBe(label)
  })

  it('shows the last test time, model and error', async () => {
    serve(settings({
      state: 'failing',
      last_tested_at: '2026-10-30T12:00:00Z',
      last_test_model: 'gpt-4o-mini',
      last_test_error: 'provider rejected the credentials',
    }))
    await renderCard()
    expect(screen.getByText(/Last tested .* · gpt-4o-mini/)).toBeTruthy()
    expect(screen.getByText('provider rejected the credentials')).toBeTruthy()
  })

  it('never prefills the key and describes where the key comes from', async () => {
    serve(settings({ api_key_configured: true, sources: { ...settings().sources, endpoint: 'database', api_key: 'database' } }))
    const view = await renderCard()
    expect(input('API key').type).toBe('password')
    expect(input('API key').value).toBe('')
    expect(screen.getByText('Key configured (stored)')).toBeTruthy()
    view.unmount()

    serve(settings({ api_key_configured: true, sources: { ...settings().sources, api_key: 'environment' } }))
    const view2 = await renderCard()
    expect(input('API key').value).toBe('')
    expect(screen.getByText('Key configured (from environment)')).toBeTruthy()
    view2.unmount()

    serve(settings())
    await renderCard()
    expect(screen.getByText('No key configured')).toBeTruthy()
  })

  it('shows the source of each field', async () => {
    serve(settings({ sources: { endpoint: 'database', model: 'environment', api_key: 'none', max_tokens: 'default', temperature: 'database' } }))
    await renderCard()
    expect(screen.getByTestId('ai-source-endpoint').textContent).toBe('database')
    expect(screen.getByTestId('ai-source-model').textContent).toBe('environment')
    expect(screen.getByTestId('ai-source-max_tokens').textContent).toBe('default')
    expect(screen.getByTestId('ai-source-temperature').textContent).toBe('database')
  })

  it('offers Clear stored key only for a database key and sends clear_api_key', async () => {
    serve(settings({ api_key_configured: true, sources: { ...settings().sources, api_key: 'environment' } }))
    const view = await renderCard()
    expect(screen.queryByRole('button', { name: 'Clear stored key' })).toBeNull()
    view.unmount()

    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    serve(settings({ api_key_configured: true, sources: { ...settings().sources, endpoint: 'database', api_key: 'database' } }))
    await renderCard()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Clear stored key' }))
    })
    expect(confirm.mock.calls[0][0]).toMatch(/CLOUDPAM_LLM_API_KEY/)
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0].body).toEqual({ clear_api_key: true })
  })

  it('disables the key input when key storage is unavailable', async () => {
    serve(settings({ api_key_storage_available: false }))
    await renderCard()
    expect(input('API key').disabled).toBe(true)
    expect(screen.getByText('Set CLOUDPAM_OIDC_ENCRYPTION_KEY to store an API key here; CLOUDPAM_LLM_API_KEY still works.')).toBeTruthy()
  })

  it('shows the server message when saving a key returns 409', async () => {
    const message = 'cannot store an api key: CLOUDPAM_OIDC_ENCRYPTION_KEY is not set'
    serve(settings(), { patch: () => json(409, { error: message }) })
    await renderCard()
    fireEvent.change(input('API key'), { target: { value: 'sk-secret' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    })
    expect(await screen.findByText(message)).toBeTruthy()
  })

  it('gives read-only users disabled inputs and no mutation buttons', async () => {
    asUser(['settings:read'])
    serve(settings({ api_key_configured: true, sources: { ...settings().sources, endpoint: 'database', api_key: 'database' } }))
    await renderCard()
    for (const label of ['Endpoint', 'Model', 'API key', 'Max tokens', 'Temperature']) {
      expect(input(label).disabled).toBe(true)
    }
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Clear stored key' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Test connection' })).toBeNull()
    expect(screen.getByText('You need settings:write to change the provider.')).toBeTruthy()
  })

  it('renders nothing and fetches nothing without settings:read', async () => {
    asUser([])
    const { container } = render(<AIProviderSettingsCard />)
    expect(container.textContent).toBe('')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('shows the reachable summary after a successful test', async () => {
    const result: AIProviderTestResult = {
      ok: true, state: 'verified', endpoint: 'https://api.openai.com/v1', model: 'gpt-4o',
      latency_ms: 120, streaming_supported: true, tested_at: '2026-10-30T12:00:00Z',
    }
    serve(settings(), { post: () => json(200, result) })
    await renderCard()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Test connection' }))
    })
    expect(await screen.findByText('Reachable · model gpt-4o · streaming yes · 120 ms')).toBeTruthy()
    expect(calls.filter(c => c.method === 'POST').map(c => c.url)).toEqual(['/api/v1/settings/ai/test'])
  })

  it('shows the sanitized error after a failed test', async () => {
    const result: AIProviderTestResult = {
      ok: false, state: 'failing', endpoint: 'https://api.openai.com/v1', model: 'gpt-4o',
      latency_ms: 30, streaming_supported: false, error: 'provider rejected the credentials', tested_at: '2026-10-30T12:00:00Z',
    }
    serve(settings(), { post: () => json(200, result) })
    await renderCard()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Test connection' }))
    })
    expect(await screen.findByText('provider rejected the credentials')).toBeTruthy()
    expect(screen.queryByText(/^Reachable/)).toBeNull()
  })

  it('saves only the changed fields, sends a typed key and clears the key input', async () => {
    serve(settings(), { patch: () => json(200, settings({ model: 'llama3' })) })
    await renderCard()
    fireEvent.change(input('Model'), { target: { value: 'llama3' } })
    fireEvent.change(input('API key'), { target: { value: 'sk-secret' } })
    fireEvent.change(input('Temperature'), { target: { value: '' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    })
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0].body).toEqual({ model: 'llama3', api_key: 'sk-secret', clear_temperature: true })
    await waitFor(() => expect(input('API key').value).toBe(''))
    // Refreshed from GET after the save.
    expect(calls.filter(c => c.method === 'GET').length).toBeGreaterThanOrEqual(2)
  })

  it('sends an emptied endpoint as an empty string', async () => {
    serve(settings({ endpoint: 'http://ollama:11434/v1', sources: { ...settings().sources, endpoint: 'database' } }),
      { patch: () => json(200, settings()) })
    await renderCard()
    fireEvent.change(input('Endpoint'), { target: { value: '' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    })
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0].body).toEqual({ endpoint: '' })
  })

  it('disables Save while a save is pending', async () => {
    const pending = deferred<Response>()
    serve(settings(), { patch: () => pending.promise })
    await renderCard()
    fireEvent.change(input('Model'), { target: { value: 'llama3' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    })
    const saveButton = screen.getByRole('button', { name: /Saving/ }) as HTMLButtonElement
    expect(saveButton.disabled).toBe(true)
    expect(input('Model').disabled).toBe(true)
    fireEvent.click(saveButton)
    expect(patches()).toHaveLength(1)
    await act(async () => {
      pending.resolve(json(200, settings({ model: 'llama3' })))
    })
    await waitFor(() => expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(false))
  })

  it('requires the key or an explicit clear when changing the endpoint with a stored key', async () => {
    const stored = settings({
      endpoint: 'https://old.example/v1', api_key_configured: true,
      sources: { ...settings().sources, endpoint: 'database', api_key: 'database' },
    })
    serve(stored, { patch: () => json(200, stored) })
    await renderCard()
    fireEvent.change(input('Endpoint'), { target: { value: 'https://new.example/v1' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    })
    expect(screen.getByText('Enter the API key again for the new endpoint, or clear the stored key')).toBeTruthy()
    expect(patches()).toHaveLength(0)

    fireEvent.click(screen.getByLabelText('Clear the stored key with this save'))
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    })
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0].body).toEqual({ endpoint: 'https://new.example/v1', clear_api_key: true })
  })

  it('shows the server detail under the key field when the endpoint change is rejected', async () => {
    const detail = 're-enter api_key or set clear_api_key when changing the endpoint while a stored key exists'
    // The UI thinks the key comes from the environment, but the server has a stored key.
    serve(settings({ api_key_configured: true, sources: { ...settings().sources, api_key: 'environment' } }),
      { patch: () => json(400, { error: 'invalid endpoint', detail }) })
    await renderCard()
    fireEvent.change(input('Endpoint'), { target: { value: 'https://new.example/v1' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    })
    expect(await screen.findByText(detail)).toBeTruthy()
  })

  it('drops a stale test result after a successful save', async () => {
    const result: AIProviderTestResult = {
      ok: true, state: 'verified', endpoint: 'https://api.openai.com/v1', model: 'gpt-4o',
      latency_ms: 120, streaming_supported: true, tested_at: '2026-10-30T12:00:00Z',
    }
    serve(settings(), { post: () => json(200, result), patch: () => json(200, settings({ model: 'llama3' })) })
    await renderCard()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Test connection' }))
    })
    expect(await screen.findByText('Reachable · model gpt-4o · streaming yes · 120 ms')).toBeTruthy()

    fireEvent.change(input('Model'), { target: { value: 'llama3' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    })
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0].body).toEqual({ model: 'llama3' })
    await waitFor(() => expect(screen.queryByText(/^Reachable/)).toBeNull())
  })

  it('drops a stale test error after a successful save', async () => {
    serve(settings(), {
      post: () => json(500, { error: 'test failed', detail: 'boom' }),
      patch: () => json(200, settings({ model: 'llama3' })),
    })
    await renderCard()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Test connection' }))
    })
    expect(await screen.findByText('test failed: boom')).toBeTruthy()

    fireEvent.change(input('Model'), { target: { value: 'llama3' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    })
    await waitFor(() => expect(patches()).toHaveLength(1))
    await waitFor(() => expect(screen.queryByText('test failed: boom')).toBeNull())
  })

  describe('after Clear stored key succeeds', () => {
    const stored = settings({
      endpoint: 'https://old.example/v1', api_key_configured: true,
      sources: { ...settings().sources, endpoint: 'database', api_key: 'database' },
    })

    async function promptForKey() {
      serve(stored, { patch: () => json(200, stored) })
      vi.spyOn(window, 'confirm').mockReturnValue(true)
      await renderCard()
      fireEvent.change(input('Endpoint'), { target: { value: 'https://new.example/v1' } })
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Save' }))
      })
      expect(screen.getByText(KEY_PROMPT_TEXT)).toBeTruthy()
    }

    async function clearKey() {
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Clear stored key' }))
      })
      await waitFor(() => expect(patches()).toHaveLength(1))
      expect(patches()[0].body).toEqual({ clear_api_key: true })
    }

    it('hides the re-enter prompt', async () => {
      await promptForKey()
      await clearKey()
      await waitFor(() => expect(screen.queryByText(KEY_PROMPT_TEXT)).toBeNull())
      expect(screen.queryByLabelText('Clear the stored key with this save')).toBeNull()
    })

    it('resets the clear-on-save checkbox', async () => {
      await promptForKey()
      fireEvent.click(screen.getByLabelText('Clear the stored key with this save'))
      expect((screen.getByLabelText('Clear the stored key with this save') as HTMLInputElement).checked).toBe(true)
      await clearKey()
      await waitFor(() => expect(screen.queryByLabelText('Clear the stored key with this save')).toBeNull())
    })
  })

  it('says that Test connection checks the saved settings', async () => {
    serve(settings())
    await renderCard()
    expect(screen.getByText('Tests the saved settings, not unsaved edits.')).toBeTruthy()
  })

  it('disables Save and Test while a test is pending', async () => {
    const pending = deferred<Response>()
    serve(settings(), { post: () => pending.promise })
    await renderCard()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Test connection' }))
    })
    expect((screen.getByRole('button', { name: /Testing/ }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
    await act(async () => {
      pending.resolve(json(200, {
        ok: true, state: 'verified', endpoint: 'https://api.openai.com/v1', model: 'gpt-4o',
        latency_ms: 1, streaming_supported: true, tested_at: '2026-10-30T12:00:00Z',
      }))
    })
    await waitFor(() => expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(false))
    expect((screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('disables Test while a save is pending', async () => {
    const pending = deferred<Response>()
    serve(settings(), { patch: () => pending.promise })
    await renderCard()
    fireEvent.change(input('Model'), { target: { value: 'llama3' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    })
    expect((screen.getByRole('button', { name: /Saving/ }) as HTMLButtonElement).disabled).toBe(true)
    const testButton = screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement
    expect(testButton.disabled).toBe(true)
    fireEvent.click(testButton)
    expect(calls.filter(c => c.method === 'POST')).toHaveLength(0)
    await act(async () => {
      pending.resolve(json(200, settings({ model: 'llama3' })))
    })
    await waitFor(() => expect((screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement).disabled).toBe(false))
  })

  describe('environment key warning', () => {
    const ENV_WARNING =
      'Changing the endpoint stops CLOUDPAM_LLM_API_KEY from applying; enter a key here or leave it empty for an authless endpoint.'

    it('appears when the key comes from the environment and the endpoint changes', async () => {
      serve(settings({ api_key_configured: true, sources: { ...settings().sources, api_key: 'environment' } }))
      await renderCard()
      expect(screen.queryByText(ENV_WARNING)).toBeNull()
      fireEvent.change(input('Endpoint'), { target: { value: 'https://new.example/v1' } })
      expect(screen.getByText(ENV_WARNING)).toBeTruthy()
      // A warning, not a block: Save stays enabled.
      expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(false)
      fireEvent.change(input('Endpoint'), { target: { value: 'https://api.openai.com/v1' } })
      expect(screen.queryByText(ENV_WARNING)).toBeNull()
    })

    it.each(['database', 'none'] as const)('does not appear for a %s key', async source => {
      serve(settings({
        api_key_configured: source === 'database',
        sources: { ...settings().sources, api_key: source },
      }))
      await renderCard()
      fireEvent.change(input('Endpoint'), { target: { value: 'https://new.example/v1' } })
      expect(screen.queryByText(ENV_WARNING)).toBeNull()
    })
  })

  it('explains that a stored endpoint never uses the environment key', async () => {
    serve(settings({ endpoint: 'http://ollama:11434/v1', sources: { ...settings().sources, endpoint: 'database', api_key: 'none' } }))
    await renderCard()
    expect(screen.getByText('A stored endpoint never uses CLOUDPAM_LLM_API_KEY; enter a key here or leave empty for an authless endpoint')).toBeTruthy()
  })
})
