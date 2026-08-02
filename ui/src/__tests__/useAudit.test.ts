import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAudit } from '../hooks/useAudit'

const mockGet = vi.hoisted(() => vi.fn())
vi.mock('../api/client', () => ({
  get: (path: string) => mockGet(path),
}))

function auditResponse(total: number) {
  return { events: [{ id: 'e1', action: 'create', resource_type: 'pool' }], total }
}

describe('useAudit pagination', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGet.mockResolvedValue(auditResponse(100))
  })

  // Regression: nextPage/prevPage called fetchEvents with only an offset, so
  // paging through a filtered log silently reverted to unfiltered results.
  it('keeps the active filters when paging forward', async () => {
    const { result } = renderHook(() => useAudit())

    await act(async () => {
      await result.current.fetchEvents(0, 25, 'create', 'pool')
    })
    await waitFor(() => expect(result.current.total).toBe(100))

    act(() => result.current.nextPage())
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))

    const url = mockGet.mock.calls[1][0] as string
    expect(url).toContain('offset=25')
    expect(url).toContain('action=create')
    expect(url).toContain('resource_type=pool')
  })

  it('keeps the active filters when paging back', async () => {
    const { result } = renderHook(() => useAudit())

    await act(async () => {
      await result.current.fetchEvents(50, 25, 'delete', 'account')
    })
    await waitFor(() => expect(result.current.offset).toBe(50))

    act(() => result.current.prevPage())
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))

    const url = mockGet.mock.calls[1][0] as string
    expect(url).toContain('offset=25')
    expect(url).toContain('action=delete')
    expect(url).toContain('resource_type=account')
  })

  // A non-default page size must survive paging too, or the offset arithmetic
  // and the request disagree.
  it('reuses the last page size when paging', async () => {
    const { result } = renderHook(() => useAudit())

    await act(async () => {
      await result.current.fetchEvents(0, 10, 'create')
    })
    act(() => result.current.nextPage())
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))

    const url = mockGet.mock.calls[1][0] as string
    expect(url).toContain('limit=10')
    expect(url).toContain('offset=10')
  })

  it('does not page past the end or before the start', async () => {
    const { result } = renderHook(() => useAudit())
    mockGet.mockResolvedValue(auditResponse(20))

    await act(async () => {
      await result.current.fetchEvents(0, 25)
    })
    await waitFor(() => expect(result.current.total).toBe(20))

    act(() => result.current.nextPage())
    act(() => result.current.prevPage())
    expect(mockGet).toHaveBeenCalledTimes(1)
  })
})
