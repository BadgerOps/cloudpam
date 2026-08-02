import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useRecommendations } from '../hooks/useRecommendations'
import { get } from '../api/client'
import type { RecommendationsListResponse } from '../api/types'

vi.mock('../api/client', () => ({
  get: vi.fn(),
  post: vi.fn(),
}))

const mockGet = vi.mocked(get)

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => {
    resolve = res
  })
  return { promise, resolve }
}

function response(total: number): RecommendationsListResponse {
  return { items: [], total, page: 1, page_size: 50 }
}

describe('useRecommendations', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('keeps the newest filter results when an older request resolves last', async () => {
    const first = deferred<RecommendationsListResponse>()
    const second = deferred<RecommendationsListResponse>()
    mockGet
      .mockReturnValueOnce(first.promise as Promise<never>)
      .mockReturnValueOnce(second.promise as Promise<never>)

    const { result } = renderHook(() => useRecommendations())

    act(() => {
      void result.current.fetch({ status: 'pending' })
      void result.current.fetch({ status: 'applied' })
    })

    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))

    await act(async () => {
      second.resolve(response(3))
      await second.promise
    })
    await act(async () => {
      first.resolve(response(77))
      await first.promise
    })

    expect(result.current.data?.total).toBe(3)
  })
})
