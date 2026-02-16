import type { ApiError } from './types'

export class ApiRequestError extends Error {
  constructor(
    public status: number,
    public apiError: ApiError,
  ) {
    super(apiError.error)
    this.name = 'ApiRequestError'
  }
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })

  // On 401, dispatch logout event so the auth context can clear state
  if (res.status === 401) {
    window.dispatchEvent(new CustomEvent('auth:logout'))
  }

  const contentType = res.headers.get('content-type') || ''
  if (!contentType.includes('application/json')) {
    throw new ApiRequestError(res.status, {
      error: `Unexpected response (${res.status}): server returned ${contentType || 'non-JSON'}`,
    })
  }

  const body = await res.json()

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

export function get<T>(path: string): Promise<T> {
  return request<T>(path)
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

export interface SSECallbacks {
  onDelta: (text: string) => void
  onDone: () => void
  onError: (error: Error) => void
}

export async function streamPost(path: string, data: unknown, callbacks: SSECallbacks): Promise<void> {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
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

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      for (const line of lines) {
        const trimmed = line.trim()
        if (trimmed === '') continue

        if (trimmed === 'event: done') {
          // Read the next data line then finish
          callbacks.onDone()
          return
        }

        if (trimmed.startsWith('data: ')) {
          const jsonStr = trimmed.slice(6)
          if (jsonStr === '{}') {
            callbacks.onDone()
            return
          }
          try {
            const parsed = JSON.parse(jsonStr)
            if (parsed.delta !== undefined) {
              callbacks.onDelta(parsed.delta)
            }
          } catch {
            // skip malformed JSON
          }
        }
      }
    }
    callbacks.onDone()
  } catch (err) {
    callbacks.onError(err instanceof Error ? err : new Error(String(err)))
  }
}
