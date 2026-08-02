import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiRequestError, del, get, post, postRaw, streamPost } from '../api/client'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function noContentResponse() {
  return new Response(null, { status: 204 })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('request empty-body handling', () => {
  it('resolves DELETE requests that return 204 No Content', async () => {
    const fetchMock = vi.fn().mockResolvedValue(noContentResponse())
    vi.stubGlobal('fetch', fetchMock)

    await expect(del('/api/v1/accounts/1')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/accounts/1',
      expect.objectContaining({ method: 'DELETE', credentials: 'same-origin' }),
    )
  })

  it('resolves successful responses with an empty body and no content type', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(del('/api/v1/auth/keys/abc')).resolves.toBeUndefined()
  })

  it('resolves successful responses declaring content-length: 0', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('', { status: 200, headers: { 'Content-Length': '0' } }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(del('/api/v1/discovery/resources/r1/link')).resolves.toBeUndefined()
  })

  it('still throws for non-JSON error responses such as an HTML 500 page', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('<html><body>boom</body></html>', {
        status: 500,
        headers: { 'Content-Type': 'text/html' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(del('/api/v1/accounts/1')).rejects.toMatchObject({
      name: 'ApiRequestError',
      status: 500,
    })
  })

  it('still throws the API error payload for JSON error responses', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ error: 'pool has children' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    await expect(del('/api/v1/pools/3')).rejects.toBeInstanceOf(ApiRequestError)
  })

  it('still returns parsed JSON bodies for successful responses', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ keys: [] }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(get('/api/v1/auth/keys')).resolves.toEqual({ keys: [] })
  })
})

describe('postRaw', () => {
  it('sends the body verbatim with a text/csv content type', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ created: 2, skipped: 0, errors: [] }))
    vi.stubGlobal('fetch', fetchMock)

    const csv = 'key,name\nacct-a,Account A\nacct-b,Account B\n'
    await expect(postRaw('/api/v1/import/accounts', csv)).resolves.toEqual({
      created: 2,
      skipped: 0,
      errors: [],
    })

    const [, init] = fetchMock.mock.calls[0]
    expect(init.method).toBe('POST')
    expect(init.credentials).toBe('same-origin')
    expect(init.headers['Content-Type']).toBe('text/csv')
    expect(init.body).toBe(csv)
  })

  it('includes the CSRF token header like other state-changing requests', async () => {
    document.cookie = 'csrf_token=raw-token-123'
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ created: 0, skipped: 0, errors: [] }))
    vi.stubGlobal('fetch', fetchMock)

    await postRaw('/api/v1/import/pools', 'name,cidr\n')

    const [, init] = fetchMock.mock.calls[0]
    expect(init.headers['X-CSRF-Token']).toBe('raw-token-123')
    document.cookie = 'csrf_token=; expires=Thu, 01 Jan 1970 00:00:00 GMT'
  })

  it('differs from post(), which JSON-encodes the body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}))
    vi.stubGlobal('fetch', fetchMock)

    const csv = 'key,name\nacct-a,Account A\n'
    await post('/api/v1/import/accounts', csv)

    const [, init] = fetchMock.mock.calls[0]
    expect(init.headers['Content-Type']).toBe('application/json')
    expect(init.body).toBe(JSON.stringify(csv))
  })
})

function sseResponse(chunks: string[]) {
  const encoder = new TextEncoder()
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk))
      }
      controller.close()
    },
  })
  return new Response(stream, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  })
}

function collect() {
  const deltas: string[] = []
  let done = false
  let error: Error | null = null
  return {
    deltas,
    get done() { return done },
    get error() { return error },
    callbacks: {
      onDelta: (d: string) => { deltas.push(d) },
      onDone: () => { done = true },
      onError: (e: Error) => { error = e },
    },
  }
}

describe('streamPost EOF handling', () => {
  it('parses a final data line that arrives without a trailing newline', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sseResponse([
      'data: {"delta":"hello"}\n',
      'data: {"delta":" world"}',
    ])))

    const sink = collect()
    await streamPost('/api/v1/ai/chat', {}, sink.callbacks)

    expect(sink.deltas).toEqual(['hello', ' world'])
    expect(sink.done).toBe(true)
    expect(sink.error).toBeNull()
  })

  it('honours a terminating done event with no trailing newline', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sseResponse([
      'data: {"delta":"a"}\n',
      'event: done',
    ])))

    const sink = collect()
    await streamPost('/api/v1/ai/chat', {}, sink.callbacks)

    expect(sink.deltas).toEqual(['a'])
    expect(sink.done).toBe(true)
  })

  it('reassembles a data line split across chunk boundaries', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sseResponse([
      'data: {"del',
      'ta":"split"}\n',
    ])))

    const sink = collect()
    await streamPost('/api/v1/ai/chat', {}, sink.callbacks)

    expect(sink.deltas).toEqual(['split'])
    expect(sink.done).toBe(true)
  })

  it('emits a trailing multi-byte character flushed at EOF', async () => {
    const encoder = new TextEncoder()
    const full = encoder.encode('data: {"delta":"café"}')
    // Split mid multi-byte sequence so the decoder must be flushed at EOF.
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(full.slice(0, full.length - 1))
        controller.enqueue(full.slice(full.length - 1))
        controller.close()
      },
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(stream, {
      status: 200,
      headers: { 'Content-Type': 'text/event-stream' },
    })))

    const sink = collect()
    await streamPost('/api/v1/ai/chat', {}, sink.callbacks)

    expect(sink.deltas).toEqual(['café'])
    expect(sink.done).toBe(true)
  })

  it('signals completion exactly once for a stream ending in a blank line', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sseResponse([
      'data: {"delta":"x"}\n\n',
    ])))

    const sink = collect()
    await streamPost('/api/v1/ai/chat', {}, sink.callbacks)

    expect(sink.deltas).toEqual(['x'])
    expect(sink.done).toBe(true)
  })
})
