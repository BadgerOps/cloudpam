import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useDiscoveryResources } from '../hooks/useDiscovery'
import { get } from '../api/client'
import type { DiscoveryResourcesResponse } from '../api/types'

vi.mock('../api/client', () => ({
  get: vi.fn(),
  post: vi.fn(),
  del: vi.fn(),
}))

const mockGet = vi.mocked(get)

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => {
    resolve = res
  })
  return { promise, resolve }
}

function response(total: number): DiscoveryResourcesResponse {
  return { items: [], total, page: 1, page_size: 25 }
}

describe('useDiscoveryResources', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('does not let a previous account load overwrite the current one', async () => {
    const first = deferred<DiscoveryResourcesResponse>()
    const second = deferred<DiscoveryResourcesResponse>()
    mockGet
      .mockReturnValueOnce(first.promise as Promise<never>)
      .mockReturnValueOnce(second.promise as Promise<never>)

    const { result } = renderHook(() => useDiscoveryResources())

    act(() => {
      void result.current.fetch(1)
      void result.current.fetch(2)
    })

    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))

    await act(async () => {
      second.resolve(response(5))
      await second.promise
    })
    await act(async () => {
      first.resolve(response(999))
      await first.promise
    })

    expect(result.current.data?.total).toBe(5)
  })
})
