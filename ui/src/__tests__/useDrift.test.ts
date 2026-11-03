import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useDrift } from '../hooks/useDrift'
import { get, patch, post } from '../api/client'
import type { DiscoveryImportApplyResponse, DriftItem, DriftListResponse } from '../api/types'

vi.mock('../api/client', () => ({
  get: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
}))

const mockGet = vi.mocked(get)
const mockPost = vi.mocked(post)
const mockPatch = vi.mocked(patch)

function driftItem(overrides: Partial<DriftItem>): DriftItem {
  return {
    id: 'drift-1',
    account_id: 7,
    resource_id: 'res-1',
    type: 'unmanaged',
    severity: 'warning',
    status: 'open',
    title: 'Unmanaged vpc',
    description: '',
    detected_at: '2026-11-03T00:00:00Z',
    updated_at: '2026-11-03T00:00:00Z',
    ...overrides,
  }
}

function importResponse(created: number, errors: string[] = []): DiscoveryImportApplyResponse {
  return {
    preview: {
      items: [],
      importable: 0,
      blocked: 0,
      linked_only: 0,
      already_linked: 0,
      conflict_count: 0,
    },
    summary: {} as DiscoveryImportApplyResponse['summary'],
    pools_created: created,
    resources_linked: 0,
    skipped: created ? 0 : 1,
    errors,
  }
}

function serve(item: DriftItem): DriftItem {
  mockGet.mockResolvedValue(item as never)
  return item
}

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

  it('applies a link suggestion through the link endpoint, then resolves', async () => {
    mockPost.mockResolvedValue({} as never)
    const { result } = renderHook(() => useDrift())

    await result.current.applySuggestion(serve(driftItem({ suggested_action: 'link_pool', suggested_pool_id: 3 })))

    expect(mockPost.mock.calls).toEqual([
      ['/api/v1/discovery/resources/res-1/link', { pool_id: 3 }],
      ['/api/v1/drift/drift-1/resolve', {}],
    ])
  })

  it('applies a create suggestion through import apply, then resolves', async () => {
    mockPost.mockResolvedValueOnce(importResponse(1) as never).mockResolvedValueOnce({} as never)
    const { result } = renderHook(() => useDrift())

    await result.current.applySuggestion(serve(driftItem({ suggested_action: 'create_pool', suggested_pool_id: 2 })))

    expect(mockPost.mock.calls).toEqual([
      ['/api/v1/discovery/import/apply', { account_id: 7, resource_ids: ['res-1'] }],
      ['/api/v1/drift/drift-1/resolve', {}],
    ])
  })

  it('leaves the item open when import apply skips the resource', async () => {
    mockPost.mockResolvedValueOnce(importResponse(0, ['vpc-1: create pool: overlaps']) as never)
    const { result } = renderHook(() => useDrift())

    await expect(
      result.current.applySuggestion(serve(driftItem({ suggested_action: 'create_pool' }))),
    ).rejects.toThrow('overlaps')
    expect(mockPost).toHaveBeenCalledTimes(1)
  })

  it('deprecates an orphaned pool, then resolves', async () => {
    mockPatch.mockResolvedValue({} as never)
    mockPost.mockResolvedValue({} as never)
    const { result } = renderHook(() => useDrift())

    await result.current.applySuggestion(
      serve(driftItem({ type: 'orphaned_pool', resource_id: undefined, pool_id: 9, suggested_action: 'deprecate_pool', suggested_pool_id: 9 })),
    )

    expect(mockGet).toHaveBeenCalledWith('/api/v1/drift/drift-1')

    expect(mockPatch).toHaveBeenCalledWith('/api/v1/pools/9', { status: 'deprecated' })
    expect(mockPost).toHaveBeenCalledWith('/api/v1/drift/drift-1/resolve', {})
  })

  it('does not deprecate a pool whose suggestion changed since the page loaded', async () => {
    const shown = driftItem({ type: 'orphaned_pool', resource_id: undefined, pool_id: 9, suggested_action: 'deprecate_pool', suggested_pool_id: 9 })
    mockGet.mockResolvedValue({ ...shown, suggested_action: 'review', suggested_pool_id: undefined, suggestion_reason: 'an active resource links to this pool again' } as never)
    const { result } = renderHook(() => useDrift())

    await expect(result.current.applySuggestion(shown)).rejects.toThrow('links to this pool again')
    expect(mockPatch).not.toHaveBeenCalled()
    expect(mockPost).not.toHaveBeenCalled()
  })

  it('refuses a review suggestion without changing anything', async () => {
    const { result } = renderHook(() => useDrift())

    await expect(
      result.current.applySuggestion(serve(driftItem({ suggested_action: 'review' }))),
    ).rejects.toThrow('no suggestion to apply')
    expect(mockPost).not.toHaveBeenCalled()
    expect(mockPatch).not.toHaveBeenCalled()
  })

  it('stays loading until the newest request settles', async () => {
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
      first.resolve(response(99))
      await first.promise
    })
    expect(result.current.loading).toBe(true)

    await act(async () => {
      second.resolve(response(2))
      await second.promise
    })
    expect(result.current.loading).toBe(false)
  })
})
