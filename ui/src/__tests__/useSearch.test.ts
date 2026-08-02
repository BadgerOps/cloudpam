import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useSearch } from '../hooks/useSearch'
import { get } from '../api/client'
import type { SearchResponse } from '../api/types'

vi.mock('../api/client', () => ({
  get: vi.fn(),
}))

const mockGet = vi.mocked(get)

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function response(name: string): SearchResponse {
  return {
    items: [{ type: 'pool', id: 1, name }],
    total: 1,
    page: 1,
    page_size: 20,
  }
}

describe('useSearch', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('keeps the newest results when an older request resolves last', async () => {
    const first = deferred<SearchResponse>()
    const second = deferred<SearchResponse>()
    mockGet
      .mockReturnValueOnce(first.promise as Promise<never>)
      .mockReturnValueOnce(second.promise as Promise<never>)

    const { result } = renderHook(() => useSearch())

    act(() => {
      result.current.setQuery('alpha')
    })
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(1))

    act(() => {
      result.current.setQuery('beta')
    })
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))

    // The newer request answers first, then the stale one lands
    await act(async () => {
      second.resolve(response('beta'))
      await second.promise
    })
    await act(async () => {
      first.resolve(response('alpha'))
      await first.promise
    })

    expect(result.current.results?.items[0].name).toBe('beta')
  })

  it('ignores an error from a superseded request', async () => {
    const first = deferred<SearchResponse>()
    const second = deferred<SearchResponse>()
    mockGet
      .mockReturnValueOnce(first.promise as Promise<never>)
      .mockReturnValueOnce(second.promise as Promise<never>)

    const { result } = renderHook(() => useSearch())

    act(() => {
      result.current.setQuery('alpha')
    })
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(1))

    act(() => {
      result.current.setQuery('beta')
    })
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))

    await act(async () => {
      second.resolve(response('beta'))
      await second.promise
    })
    await act(async () => {
      first.reject(new Error('stale failure'))
      await first.promise.catch(() => undefined)
    })

    expect(result.current.error).toBeNull()
    expect(result.current.results?.items[0].name).toBe('beta')
  })
})
