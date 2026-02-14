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
