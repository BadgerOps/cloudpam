import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useLogDestinations } from '../hooks/useLogDestinations'
import type { LogDestination } from '../api/types'

function destination(overrides: Partial<LogDestination> = {}): LogDestination {
  return {
    type: 'syslog',
    format: 'cef',
    network: 'udp',
    address: 'siem.example.com:514',
    app_name: 'cloudpam',
    state: 'idle',
    delivered: 0,
    failed: 0,
    ...overrides,
  }
}

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const fetchMock = vi.fn()

describe('useLogDestinations', () => {
  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('loads the destinations once when enabled and polling is off', async () => {
    fetchMock.mockResolvedValue(json(200, { destinations: [destination({ state: 'ok', delivered: 3 })] }))
    const { result } = renderHook(() => useLogDestinations(true, 0))

    expect(result.current.loading).toBe(true)
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(String(fetchMock.mock.calls[0][0])).toBe('/api/v1/settings/log-destinations')
    expect(result.current.destinations).toEqual([destination({ state: 'ok', delivered: 3 })])
    expect(result.current.error).toBeNull()
    expect(result.current.refreshedAt).toBeInstanceOf(Date)
  })

  it('never requests when the caller lacks settings:read', async () => {
    const { result } = renderHook(() => useLogDestinations(false, 0))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).not.toHaveBeenCalled()
    expect(result.current.destinations).toBeNull()
  })

  it('reports the API error and keeps the page usable', async () => {
    fetchMock.mockResolvedValue(json(500, { error: 'failed to load settings' }))
    const { result } = renderHook(() => useLogDestinations(true, 0))
    await waitFor(() => expect(result.current.error).toBe('failed to load settings'))
    expect(result.current.loading).toBe(false)
    expect(result.current.destinations).toBeNull()
  })

  it('re-reads on the poll interval and on refresh', async () => {
    fetchMock.mockResolvedValue(json(200, { destinations: [] }))
    const { result } = renderHook(() => useLogDestinations(true, 20))
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(2))

    const before = fetchMock.mock.calls.length
    await act(async () => { await result.current.refresh() })
    expect(fetchMock.mock.calls.length).toBeGreaterThan(before)
    expect(result.current.destinations).toEqual([])
  })
})
