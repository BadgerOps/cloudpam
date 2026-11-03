import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PoolRunway from '../components/PoolRunway'
import type { GrowthProjection, PoolStats } from '../api/types'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const baseStats: PoolStats = {
  total_ips: 65536,
  used_ips: 7680,
  available_ips: 57856,
  utilization: 11.7,
  child_count: 3,
  direct_children: 3,
}

function projection(overrides: Partial<GrowthProjection>): GrowthProjection {
  return {
    status: 'ok',
    window_days: 90,
    window_start: '2026-08-04T12:00:00Z',
    sample_count: 4,
    growth_ips_per_day: 256,
    runway_days: 226,
    exhaustion_date: '2027-06-16T12:00:00Z',
    forecast: [
      { months: 3, used_ips: 31056, utilization: 47.4 },
      { months: 6, used_ips: 54432, utilization: 83.1 },
      { months: 12, used_ips: 65536, utilization: 100 },
    ],
    ...overrides,
  }
}

function stubStats(respond: () => Response) {
  const fetchMock = vi.fn(async (url: string) => {
    if (url === '/api/v1/pools/7/stats') return respond()
    throw new Error(`unexpected fetch ${url}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('PoolRunway', () => {
  it('shows the runway, the rate, and the forecast when growth is fitted', async () => {
    stubStats(() => jsonResponse({ ...baseStats, projection: projection({}) }))
    render(<PoolRunway poolId={7} />)

    await waitFor(() => {
      expect(screen.getByTestId('runway').textContent).toBe('7 months')
    })
    expect(screen.getByText(/256 IPs\/day over the last 90 days/).textContent).toContain('full by')
    const forecast = screen.getByTestId('growth-forecast')
    expect(forecast.textContent).toContain('3 mo')
    expect(forecast.textContent).toContain('47%')
    expect(forecast.textContent).toContain('100%')
  })

  it('says the runway is unbounded when usage is flat', async () => {
    stubStats(() => jsonResponse({
      ...baseStats,
      projection: projection({ status: 'no_growth', growth_ips_per_day: 0, runway_days: undefined, exhaustion_date: undefined }),
    }))
    render(<PoolRunway poolId={7} />)

    await waitFor(() => {
      expect(screen.getByText('No growth over the last 90 days')).toBeTruthy()
    })
    expect(screen.queryByTestId('runway')).toBeNull()
    expect(screen.getByTestId('growth-forecast')).toBeTruthy()
  })

  it('explains when the pool has too little history', async () => {
    stubStats(() => jsonResponse({
      ...baseStats,
      projection: projection({ status: 'insufficient_history', sample_count: 1, growth_ips_per_day: 0, runway_days: undefined, exhaustion_date: undefined, forecast: [] }),
    }))
    render(<PoolRunway poolId={7} />)

    await waitFor(() => {
      expect(screen.getByText(/Not enough history to project growth/)).toBeTruthy()
    })
    expect(screen.queryByTestId('growth-forecast')).toBeNull()
  })

  it('shows an error with a retry that refetches the stats', async () => {
    let calls = 0
    const fetchMock = stubStats(() => {
      calls += 1
      return calls === 1
        ? jsonResponse({ error: 'stats unavailable' }, 500)
        : jsonResponse({ ...baseStats, projection: projection({}) })
    })
    render(<PoolRunway poolId={7} />)

    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toContain('stats unavailable')
    })
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => {
      expect(screen.getByTestId('runway')).toBeTruthy()
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
