import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useConflictChecker } from '../wizard/hooks/useConflictChecker'
import type { SchemaNode } from '../wizard/utils/cidr'
import type { SchemaCheckResponse } from '../api/types'
import { post } from '../api/client'

vi.mock('../api/client', () => ({
  post: vi.fn(),
}))

const mockPost = vi.mocked(post)

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => {
    resolve = res
  })
  return { promise, resolve }
}

function schemaNode(cidr: string): SchemaNode {
  return { id: 'root', name: 'Root', cidr, type: 'supernet', children: [] }
}

function response(cidr: string): SchemaCheckResponse {
  return {
    conflicts: [
      {
        planned_cidr: cidr,
        planned_name: 'Root',
        existing_pool_id: 1,
        existing_pool_name: 'Existing',
        existing_cidr: cidr,
        overlap_type: 'overlap',
      },
    ],
    total_pools: 1,
    conflict_count: 1,
  }
}

describe('useConflictChecker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('keeps conflicts for the latest schema when an older check resolves last', async () => {
    const first = deferred<SchemaCheckResponse>()
    const second = deferred<SchemaCheckResponse>()
    mockPost
      .mockReturnValueOnce(first.promise as Promise<never>)
      .mockReturnValueOnce(second.promise as Promise<never>)

    const { result, rerender } = renderHook(
      ({ schema }: { schema: SchemaNode }) => useConflictChecker(schema, true),
      { initialProps: { schema: schemaNode('10.0.0.0/8') } },
    )

    await waitFor(() => expect(mockPost).toHaveBeenCalledTimes(1))

    rerender({ schema: schemaNode('192.168.0.0/16') })
    await waitFor(() => expect(mockPost).toHaveBeenCalledTimes(2))

    // The newer check answers first, then the stale one lands
    await act(async () => {
      second.resolve(response('192.168.0.0/16'))
      await second.promise
    })
    await act(async () => {
      first.resolve(response('10.0.0.0/8'))
      await first.promise
    })

    expect(result.current.conflicts).toHaveLength(1)
    expect(result.current.conflicts[0].planned_cidr).toBe('192.168.0.0/16')
  })
})
