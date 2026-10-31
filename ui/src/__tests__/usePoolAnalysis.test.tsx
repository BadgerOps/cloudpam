import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { usePoolAnalysis } from '../hooks/usePoolAnalysis'
import type { FragmentationAnalysis, GapAnalysis } from '../api/types'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(r => { resolve = r })
  return { promise, resolve }
}

function gaps(poolId: number): GapAnalysis {
  return {
    pool_id: poolId,
    pool_name: `pool-${poolId}`,
    parent_cidr: '10.0.0.0/16',
    allocated_blocks: [],
    available_blocks: [{ cidr: '10.0.0.0/16', address_count: 65536 }],
    total_addresses: 65536,
    used_addresses: 0,
    free_addresses: 65536,
    utilization_percent: 0,
  }
}

function frag(poolId: number, score = 0): FragmentationAnalysis {
  return { pool_id: poolId, pool_name: `pool-${poolId}`, score, issues: [], recommendations: [] }
}

function callsTo(fetchMock: ReturnType<typeof vi.fn>, path: string) {
  return fetchMock.mock.calls.filter(([url]) => url === path)
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('usePoolAnalysis', () => {
  it('starts the gap and fragmentation requests together for the pool', async () => {
    const gapRes = deferred<Response>()
    const fragRes = deferred<Response>()
    const fetchMock = vi.fn((url: string) =>
      url === '/api/v1/analysis/gaps' ? gapRes.promise : fragRes.promise,
    )
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => usePoolAnalysis(7))

    expect(result.current.loading).toBe(true)
    // Both requests are issued before either has resolved.
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const [gapCall] = callsTo(fetchMock, '/api/v1/analysis/gaps')
    const [fragCall] = callsTo(fetchMock, '/api/v1/analysis/fragmentation')
    expect(gapCall[1]).toMatchObject({ method: 'POST', body: JSON.stringify({ pool_id: 7 }) })
    expect(fragCall[1]).toMatchObject({ method: 'POST', body: JSON.stringify({ pool_ids: [7] }) })

    await act(async () => {
      gapRes.resolve(jsonResponse(gaps(7)))
      fragRes.resolve(jsonResponse(frag(7, 12)))
    })

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.gaps?.pool_id).toBe(7)
    expect(result.current.fragmentation?.score).toBe(12)
    expect(result.current.gapsError).toBeNull()
    expect(result.current.fragmentationError).toBeNull()
  })

  it('never surfaces a previous pool response after the selection changes', async () => {
    const pending = new Map<string, ReturnType<typeof deferred<Response>>>()
    const fetchMock = vi.fn((url: string, init: RequestInit) => {
      const d = deferred<Response>()
      pending.set(`${url} ${init.body}`, d)
      return d.promise
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result, rerender } = renderHook(({ id }) => usePoolAnalysis(id), { initialProps: { id: 7 } })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    rerender({ id: 8 })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4))
    expect(result.current.loading).toBe(true)

    // Pool 8 resolves first, then pool 7's slow responses land.
    await act(async () => {
      pending.get('/api/v1/analysis/gaps {"pool_id":8}')!.resolve(jsonResponse(gaps(8)))
      pending.get('/api/v1/analysis/fragmentation {"pool_ids":[8]}')!.resolve(jsonResponse(frag(8, 40)))
    })
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      pending.get('/api/v1/analysis/gaps {"pool_id":7}')!.resolve(jsonResponse(gaps(7)))
      pending.get('/api/v1/analysis/fragmentation {"pool_ids":[7]}')!.resolve(jsonResponse(frag(7, 90)))
    })

    expect(result.current.gaps?.pool_id).toBe(8)
    expect(result.current.fragmentation?.pool_id).toBe(8)
    expect(result.current.fragmentation?.score).toBe(40)
  })

  it('never paints the previous pool results under a new pool', async () => {
    const pending = new Map<string, ReturnType<typeof deferred<Response>>>()
    const fetchMock = vi.fn((url: string, init: RequestInit) => {
      const d = deferred<Response>()
      pending.set(`${url} ${init.body}`, d)
      return d.promise
    })
    vi.stubGlobal('fetch', fetchMock)

    // Record every render, so a stale paint between the rerender and the
    // clearing effect is visible even though act() flushes effects.
    const renders: { id: number; gapsPool: number | null; fragPool: number | null }[] = []
    const { result, rerender } = renderHook(({ id }) => {
      const value = usePoolAnalysis(id)
      renders.push({ id, gapsPool: value.gaps?.pool_id ?? null, fragPool: value.fragmentation?.pool_id ?? null })
      return value
    }, { initialProps: { id: 7 } })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    await act(async () => {
      pending.get('/api/v1/analysis/gaps {"pool_id":7}')!.resolve(jsonResponse(gaps(7)))
      pending.get('/api/v1/analysis/fragmentation {"pool_ids":[7]}')!.resolve(jsonResponse(frag(7, 12)))
    })
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.gaps?.pool_id).toBe(7)

    renders.length = 0
    rerender({ id: 8 })

    expect(result.current.gaps).toBeNull()
    expect(result.current.fragmentation).toBeNull()
    // No render for pool 8 ever carried pool 7's data.
    expect(renders.filter(r => r.id === 8 && (r.gapsPool !== null || r.fragPool !== null))).toEqual([])

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4))
    await act(async () => {
      pending.get('/api/v1/analysis/gaps {"pool_id":8}')!.resolve(jsonResponse(gaps(8)))
      pending.get('/api/v1/analysis/fragmentation {"pool_ids":[8]}')!.resolve(jsonResponse(frag(8, 40)))
    })
    await waitFor(() => expect(result.current.gaps?.pool_id).toBe(8))
    expect(result.current.fragmentation?.pool_id).toBe(8)
  })

  it('reports only the failing half and retry refetches both', async () => {
    let gapsFail = true
    const fetchMock = vi.fn(async (url: string) => {
      if (url === '/api/v1/analysis/gaps') {
        return gapsFail
          ? jsonResponse({ error: 'gap analysis failed', detail: 'boom' }, 500)
          : jsonResponse(gaps(7))
      }
      return jsonResponse(frag(7, 5))
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => usePoolAnalysis(7))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.gapsError).toBe('gap analysis failed')
    expect(result.current.gaps).toBeNull()
    expect(result.current.fragmentationError).toBeNull()
    expect(result.current.fragmentation?.score).toBe(5)

    gapsFail = false
    await act(async () => {
      result.current.retry()
    })

    await waitFor(() => expect(result.current.gaps?.pool_id).toBe(7))
    expect(result.current.gapsError).toBeNull()
    expect(result.current.loading).toBe(false)
    expect(callsTo(fetchMock, '/api/v1/analysis/gaps')).toHaveLength(2)
    expect(callsTo(fetchMock, '/api/v1/analysis/fragmentation')).toHaveLength(2)
  })

  it('does nothing without a pool', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => usePoolAnalysis(null))

    expect(fetchMock).not.toHaveBeenCalled()
    expect(result.current).toMatchObject({
      gaps: null,
      fragmentation: null,
      loading: false,
      gapsError: null,
      fragmentationError: null,
    })
  })
})
