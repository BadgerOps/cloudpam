import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useBlocks } from '../hooks/useBlocks'
import { get } from '../api/client'
import type { BlocksListResponse } from '../api/types'

vi.mock('../api/client', () => ({
  get: vi.fn(),
}))

const mockGet = vi.mocked(get)

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => {
    resolve = res
  })
  return { promise, resolve }
}

function response(page: number): BlocksListResponse {
  return {
    items: [
      {
        id: page,
        name: `block-page-${page}`,
        cidr: `10.0.${page}.0/24`,
        type: 'subnet',
        status: 'active',
        created_at: '2026-01-01T00:00:00Z',
      },
    ],
    total: 120,
    page,
    page_size: 50,
  }
}

describe('useBlocks', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('keeps the newest page when an older request resolves last', async () => {
    const first = deferred<BlocksListResponse>()
    const second = deferred<BlocksListResponse>()
    mockGet
      .mockReturnValueOnce(first.promise as Promise<never>)
      .mockReturnValueOnce(second.promise as Promise<never>)

    const { result } = renderHook(() => useBlocks())

    act(() => {
      void result.current.fetchBlocks({ page: 2 })
      void result.current.fetchBlocks({ page: 3 })
    })
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))

    await act(async () => {
      second.resolve(response(3))
      await second.promise
    })
    await act(async () => {
      first.resolve(response(2))
      await first.promise
    })

    expect(result.current.blocks[0].name).toBe('block-page-3')
  })
})
