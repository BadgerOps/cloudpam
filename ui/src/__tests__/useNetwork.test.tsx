import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useNetworkView } from '../hooks/useNetwork'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useNetworkView', () => {
  it('passes schema policy and alternate exact pool filters to conflict queries', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [], total: 0 }))
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useNetworkView())

    await act(async () => {
      await result.current.fetchConflicts({
        account_id: 7,
        conflict_type: 'alternate_exact_pool',
        schema_policy: 'region_level',
        q: 'prod',
      })
    })

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/network/conflicts?account_id=7&conflict_type=alternate_exact_pool&schema_policy=region_level&q=prod',
      expect.objectContaining({ credentials: 'same-origin' }),
    )
  })

  it('loads managed network objects with API-backed filters', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [], total: 0 }))
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useNetworkView())

    await act(async () => {
      await result.current.fetchObjects({
        account_id: 7,
        object_type: 'vpc',
        status: 'placeholder',
        q: 'missing',
      })
    })

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/network/objects?account_id=7&object_type=vpc&state=placeholder&q=missing',
      expect.objectContaining({ credentials: 'same-origin' }),
    )
    await waitFor(() => expect(result.current.objects?.total).toBe(0))
  })

  it('resolves relationships through the body-form endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({
      id: 'contains/discovered/id/with/slashes',
      type: 'contains',
      source_kind: 'network_object',
      source_id: '1',
      target_kind: 'discovered',
      target_id: 'id/with/slashes',
      confidence: 1,
      resolution_state: 'accepted',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }))
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useNetworkView())

    await act(async () => {
      await result.current.resolveNetworkRelationship({
        id: 'contains/discovered/id/with/slashes',
        resolution_state: 'accepted',
        reason: 'reviewed',
      })
    })

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/network/relationships/resolve',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          id: 'contains/discovered/id/with/slashes',
          resolution_state: 'accepted',
          reason: 'reviewed',
        }),
      }),
    )
  })
})
