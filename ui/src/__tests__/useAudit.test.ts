import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAudit } from '../hooks/useAudit'
import { get } from '../api/client'
import type { AuditListResponse } from '../api/types'

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

function response(offset: number): AuditListResponse {
  return {
    events: [
      {
        id: `event-at-${offset}`,
        timestamp: '2026-01-01T00:00:00Z',
        actor: 'admin',
        actor_type: 'user',
        action: 'create',
        resource_type: 'pool',
        resource_id: '1',
        status_code: 200,
      },
    ],
    total: 100,
    limit: 25,
    offset,
  }
}

describe('useAudit', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('keeps the newest page when an older request resolves last', async () => {
    const first = deferred<AuditListResponse>()
    const second = deferred<AuditListResponse>()
    mockGet
      .mockReturnValueOnce(first.promise as Promise<never>)
      .mockReturnValueOnce(second.promise as Promise<never>)

    const { result } = renderHook(() => useAudit())

    act(() => {
      void result.current.fetchEvents(25)
      void result.current.fetchEvents(50)
    })
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))

    await act(async () => {
      second.resolve(response(50))
      await second.promise
    })
    await act(async () => {
      first.resolve(response(25))
      await first.promise
    })

    expect(result.current.events[0].id).toBe('event-at-50')
  })
  it('stays loading until the newest request settles', async () => {
    const first = deferred<AuditListResponse>()
    const second = deferred<AuditListResponse>()
    mockGet
      .mockReturnValueOnce(first.promise as Promise<never>)
      .mockReturnValueOnce(second.promise as Promise<never>)

    const { result } = renderHook(() => useAudit())

    act(() => {
      void result.current.fetchEvents(0, 25, 'create')
      void result.current.fetchEvents(0, 25, 'delete')
    })
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))

    await act(async () => {
      first.resolve(response(0))
      await first.promise
    })
    expect(result.current.loading).toBe(true)

    await act(async () => {
      second.resolve(response(0))
      await second.promise
    })
    expect(result.current.loading).toBe(false)
  })
})
