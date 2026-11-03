import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { usePools } from '../hooks/usePools'
import { del, get } from '../api/client'
import type { Pool } from '../api/types'

vi.mock('../api/client', () => ({
  get: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
  del: vi.fn(),
}))

const mockGet = vi.mocked(get)
const mockDel = vi.mocked(del)

function pool(id: number, parent_id: number | null): Pool {
  return {
    id, parent_id, name: `p${id}`, cidr: `10.${id}.0.0/16`,
    type: 'vpc', status: 'active', source: 'manual',
    created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  }
}

describe('usePools.deletePool', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGet.mockResolvedValue([pool(1, null), pool(2, 1), pool(3, 2), pool(4, null)])
    mockDel.mockResolvedValue(undefined)
  })

  it('drops the pool and its descendants from the list after a forced delete', async () => {
    const { result } = renderHook(() => usePools())
    await act(async () => { await result.current.fetchPools() })
    await waitFor(() => expect(result.current.pools).toHaveLength(4))

    await act(async () => { await result.current.deletePool(1, true) })

    expect(mockDel).toHaveBeenCalledWith('/api/v1/pools/1?force=true')
    expect(result.current.pools.map(p => p.id)).toEqual([4])
  })

  it('drops only the leaf after a plain delete', async () => {
    const { result } = renderHook(() => usePools())
    await act(async () => { await result.current.fetchPools() })
    await waitFor(() => expect(result.current.pools).toHaveLength(4))

    await act(async () => { await result.current.deletePool(3) })

    expect(mockDel).toHaveBeenCalledWith('/api/v1/pools/3')
    expect(result.current.pools.map(p => p.id)).toEqual([1, 2, 4])
  })
})
