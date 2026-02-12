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
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })

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
