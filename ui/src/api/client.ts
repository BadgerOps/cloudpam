import type { ApiError } from './types'
import type { SystemInfoResponse, UpdateCheckResponse, UpgradeStatusAckResponse, UpgradeStatusResponse } from './types'

export class ApiRequestError extends Error {
  constructor(
    public status: number,
    public apiError: ApiError,
  ) {
    super(apiError.error)
    this.name = 'ApiRequestError'
  }
}

function getCSRFToken(): string | null {
  const match = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]+)/)
  return match ? match[1] : null
}

interface RequestOptions extends Omit<RequestInit, 'headers'> {
  headers?: Record<string, string>
}

// Successful responses are allowed to carry no payload (e.g. 204 No Content from
// DELETE endpoints), and those statuses never have a readable body.
function isNoContent(res: Response): boolean {
  return res.status === 204 || res.status === 205 || res.headers.get('content-length') === '0'
}

async function readBody(res: Response): Promise<string> {
  try {
    return await res.text()
  } catch {
    return ''
  }
}

async function request<T>(path: string, options?: RequestOptions): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json', ...options?.headers }

  // Add CSRF token for state-changing requests
  if (options?.method && options.method !== 'GET' && options.method !== 'HEAD') {
    const csrfToken = getCSRFToken()
    if (csrfToken) {
      headers['X-CSRF-Token'] = csrfToken
    }
  }

  const res = await fetch(path, {
    credentials: 'same-origin',
    ...options,
    headers,
  })

  // On 401, dispatch logout event so the auth context can clear state
  if (res.status === 401) {
    window.dispatchEvent(new CustomEvent('auth:logout'))
  }

  // A successful response with no content is not an error; callers of these
  // endpoints (e.g. del()) expect void.
  if (res.ok && isNoContent(res)) {
    return undefined as T
  }

  const contentType = res.headers.get('content-type') || ''
  const text = await readBody(res)

  if (res.ok && text.trim() === '') {
    return undefined as T
  }

  if (!contentType.includes('application/json')) {
    throw new ApiRequestError(res.status, {
      error: `Unexpected response (${res.status}): server returned ${contentType || 'non-JSON'}`,
    })
  }

  let body: unknown
  try {
    body = JSON.parse(text)
  } catch {
    throw new ApiRequestError(res.status, {
      error: `Unexpected response (${res.status}): server returned malformed JSON`,
    })
  }

  if (!res.ok) {
    throw new ApiRequestError(res.status, body as ApiError)
  }

  return body as T
}

export function post<T>(path: string, data: unknown): Promise<T> {
  return request<T>(path, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

// postRaw sends the body verbatim (no JSON encoding) for endpoints that parse the
// raw request body, such as the CSV import handlers.
export function postRaw<T>(path: string, body: string, contentType = 'text/csv'): Promise<T> {
  return request<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': contentType },
    body,
  })
}

export function get<T>(path: string): Promise<T> {
  return request<T>(path)
}

export async function getText(path: string): Promise<string> {
  const res = await fetch(path, {
    credentials: 'same-origin',
  })

  if (res.status === 401) {
    window.dispatchEvent(new CustomEvent('auth:logout'))
  }

  if (!res.ok) {
    const contentType = res.headers.get('content-type') || ''
    if (contentType.includes('application/json')) {
      const body = await res.json()
      throw new ApiRequestError(res.status, body as ApiError)
    }
    throw new ApiRequestError(res.status, { error: `Unexpected response (${res.status})` })
  }

  return await res.text()
}

export function patch<T>(path: string, data: unknown): Promise<T> {
  return request<T>(path, {
    method: 'PATCH',
    body: JSON.stringify(data),
  })
}

export function del<T = void>(path: string): Promise<T> {
  return request<T>(path, { method: 'DELETE' })
}

export function getSystemInfo(): Promise<SystemInfoResponse> {
  return get<SystemInfoResponse>('/api/v1/system/info')
}

export function getChangelogMarkdown(): Promise<string> {
  return getText('/api/v1/system/changelog')
}

export function checkForUpdates(force = false): Promise<UpdateCheckResponse> {
  const suffix = force ? '?force=true' : ''
  return get<UpdateCheckResponse>(`/api/v1/updates${suffix}`)
}

export function triggerUpgrade(): Promise<{ status: string; upgrade_id?: string; target_version: string; message: string }> {
  return post<{ status: string; upgrade_id?: string; target_version: string; message: string }>('/api/v1/updates/upgrade', {})
}

export function getUpgradeStatus(): Promise<UpgradeStatusResponse> {
  return get<UpgradeStatusResponse>('/api/v1/updates/status')
}

export function acknowledgeUpgradeStatus(): Promise<UpgradeStatusAckResponse> {
  return post<UpgradeStatusAckResponse>('/api/v1/updates/status/ack', {})
}

export interface SSECallbacks {
  onDelta: (text: string) => void
  onDone: () => void
  onError: (error: Error) => void
}

export async function streamPost(path: string, data: unknown, callbacks: SSECallbacks): Promise<void> {
  const streamHeaders: Record<string, string> = { 'Content-Type': 'application/json' }
  const csrfToken = getCSRFToken()
  if (csrfToken) {
    streamHeaders['X-CSRF-Token'] = csrfToken
  }

  const res = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: streamHeaders,
    body: JSON.stringify(data),
  })

  if (res.status === 401) {
    window.dispatchEvent(new CustomEvent('auth:logout'))
  }

  if (!res.ok) {
    const contentType = res.headers.get('content-type') || ''
    if (contentType.includes('application/json')) {
      const body = await res.json()
      callbacks.onError(new Error(body.error || `HTTP ${res.status}`))
    } else {
      callbacks.onError(new Error(`HTTP ${res.status}`))
    }
    return
  }

  const reader = res.body?.getReader()
  if (!reader) {
    callbacks.onError(new Error('No response body'))
    return
  }

  const decoder = new TextDecoder()
  let buffer = ''

  // Returns true when the line signals end of stream.
  function handleLine(line: string): boolean {
    const trimmed = line.trim()
    if (trimmed === '') return false

    if (trimmed === 'event: done') return true

    if (trimmed.startsWith('data: ')) {
      const jsonStr = trimmed.slice(6)
      if (jsonStr === '{}') return true
      try {
        const parsed = JSON.parse(jsonStr)
        if (parsed.delta !== undefined) {
          callbacks.onDelta(parsed.delta)
        }
      } catch {
        // skip malformed JSON
      }
    }
    return false
  }

  try {
    for (;;) {
      const { done, value } = await reader.read()

      // Flush the decoder at EOF so a trailing multi-byte character is emitted.
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })

      const lines = buffer.split('\n')
      // Mid-stream the trailing fragment may be an incomplete line, so hold it
      // back. At EOF nothing more is coming, so parse it as a final event.
      buffer = done ? '' : lines.pop() ?? ''

      for (const line of lines) {
        if (handleLine(line)) {
          callbacks.onDone()
          return
        }
      }

      if (done) break
    }
    callbacks.onDone()
  } catch (err) {
    callbacks.onError(err instanceof Error ? err : new Error(String(err)))
  }
}
