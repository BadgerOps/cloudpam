import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useApplySchema } from '../wizard/hooks/useApplySchema'
import type { SchemaNode } from '../wizard/utils/cidr'
import { post } from '../api/client'

vi.mock('../api/client', () => ({
  post: vi.fn(),
}))

const mockPost = vi.mocked(post)

const schema: SchemaNode = {
  id: 'root',
  name: 'Root',
  cidr: '10.0.0.0/8',
  type: 'supernet',
  children: [],
}

describe('useApplySchema', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('returns true after applying a schema successfully', async () => {
    mockPost.mockResolvedValue({
      created: 1,
      skipped: 0,
      errors: [],
      root_pool_id: 1,
      pool_map: { root: 1 },
    })
    const { result } = renderHook(() => useApplySchema())
    let applied = false

    await act(async () => {
      applied = await result.current.apply(schema)
    })

    expect(applied).toBe(true)
    expect(result.current.error).toBeNull()
    expect(result.current.result?.created).toBe(1)
  })

  it('returns false and keeps result empty when applying a schema fails', async () => {
    mockPost.mockRejectedValue(new Error('validation failed'))
    const { result } = renderHook(() => useApplySchema())
    let applied = true

    await act(async () => {
      applied = await result.current.apply(schema)
    })

    expect(applied).toBe(false)
    await waitFor(() => {
      expect(result.current.error).toBe('validation failed')
    })
    expect(result.current.result).toBeNull()
  })
})
