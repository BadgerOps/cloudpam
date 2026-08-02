import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useDrift } from '../hooks/useDrift'
import { get } from '../api/client'
import type { DriftListResponse } from '../api/types'

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

function response(total: number): DriftListResponse {
  return {
    items: [],
    total,
    page: 1,
    page_size: 50,
    summary: {
      total_drifts: total,
      by_severity: {},
      by_type: {},
      accounts_scanned: 0,
      resources_scanned: 0,
      pools_scanned: 0,
    },
  }
}

describe('useDrift', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('keeps the newest filter results when an older request resolves last', async () => {
    const first = deferred<DriftListResponse>()
    const second = deferred<DriftListResponse>()
    mockGet
      .mockReturnValueOnce(first.promise as Promise<never>)
      .mockReturnValueOnce(second.promise as Promise<never>)

    const { result } = renderHook(() => useDrift())

    act(() => {
      void result.current.fetch({ severity: 'low' })
      void result.current.fetch({ severity: 'critical' })
    })

    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))

    await act(async () => {
      second.resolve(response(2))
      await second.promise
    })
    await act(async () => {
      first.resolve(response(99))
      await first.promise
    })

    expect(result.current.data?.total).toBe(2)
  })
})
