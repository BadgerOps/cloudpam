import type { ApiError } from './types'

const AUTH_STORAGE_KEY = 'cloudpam_api_key'

export class ApiRequestError extends Error {
  constructor(
    public status: number,
    public apiError: ApiError,
  ) {
    super(apiError.error)
    this.name = 'ApiRequestError'
  }
}

function getAuthHeaders(): Record<string, string> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  const token = sessionStorage.getItem(AUTH_STORAGE_KEY)
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }
  return headers
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: 'same-origin',
    headers: getAuthHeaders(),
    ...options,
  })

  // On 401, clear token and dispatch logout event
  if (res.status === 401) {
    sessionStorage.removeItem(AUTH_STORAGE_KEY)
    window.dispatchEvent(new CustomEvent('auth:logout'))
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
