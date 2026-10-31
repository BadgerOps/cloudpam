import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PoolDetailPanel from '../components/PoolDetailPanel'
import type { FragmentationAnalysis, GapAnalysis, PoolWithStats } from '../api/types'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const pool: PoolWithStats = {
  id: 7,
  name: 'prod',
  cidr: '10.0.0.0/16',
  type: 'region',
  status: 'active',
  source: 'manual',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  stats: { total_ips: 65536, used_ips: 2048, available_ips: 63488, utilization: 3.1, child_count: 2, direct_children: 2 },
}

function gapsWithBlocks(count: number): GapAnalysis {
  return {
    pool_id: 7,
    pool_name: 'prod',
    parent_cidr: '10.0.0.0/16',
    allocated_blocks: [{ pool_id: 8, name: 'web', cidr: '10.0.0.0/22', utilization_percent: 10 }],
    available_blocks: Array.from({ length: count }, (_, i) => ({ cidr: `10.0.${(i + 1) * 4}.0/22`, address_count: 1024 })),
    total_addresses: 65536,
    used_addresses: 1024,
    free_addresses: 64512,
    utilization_percent: 1.6,
  }
}

function fragWith(score: number, overrides: Partial<FragmentationAnalysis> = {}): FragmentationAnalysis {
  return { pool_id: 7, pool_name: 'prod', score, issues: [], recommendations: [], ...overrides }
}

function stubAnalysis(gaps: () => Response, frag: () => Response) {
  const fetchMock = vi.fn(async (url: string) => {
    if (url === '/api/v1/analysis/gaps') return gaps()
    if (url === '/api/v1/analysis/fragmentation') return frag()
    throw new Error(`unexpected fetch ${url}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function gapCalls(fetchMock: ReturnType<typeof vi.fn>) {
  return fetchMock.mock.calls.filter(([url]) => url === '/api/v1/analysis/gaps')
}

function renderPanel() {
  return render(
    <MemoryRouter>
      <PoolDetailPanel pool={pool} onClose={() => {}} />
    </MemoryRouter>,
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('PoolDetailPanel address analysis', () => {
  it('shows free space and at most five available ranges', async () => {
    stubAnalysis(() => jsonResponse(gapsWithBlocks(7)), () => jsonResponse(fragWith(10)))
    renderPanel()

    expect(screen.getByRole('button', { name: /Address Analysis/ })).toBeTruthy()
    await waitFor(() => expect(screen.getByText('64.5K')).toBeTruthy())
    expect(screen.getByTestId('available-block-count').textContent).toBe('7')
    const ranges = screen.getAllByTestId('available-range')
    expect(ranges.map(r => r.textContent)).toEqual([
      '10.0.4.0/22', '10.0.8.0/22', '10.0.12.0/22', '10.0.16.0/22', '10.0.20.0/22',
    ])
    expect(screen.getByText('+2 more')).toBeTruthy()
  })

  it('lists exactly five available ranges without a more line', async () => {
    stubAnalysis(() => jsonResponse(gapsWithBlocks(5)), () => jsonResponse(fragWith(10)))
    renderPanel()

    await waitFor(() => expect(screen.getAllByTestId('available-range')).toHaveLength(5))
    expect(screen.getByText('10 / 100')).toBeTruthy()
    expect(screen.queryByText(/more$/)).toBeNull()
  })

  it.each([
    [10, 'Healthy'],
    [35, 'Attention'],
    [80, 'Severe'],
  ])('labels fragmentation score %i as %s', async (score, label) => {
    stubAnalysis(() => jsonResponse(gapsWithBlocks(1)), () => jsonResponse(fragWith(score)))
    renderPanel()

    await waitFor(() => expect(screen.getByText(`${score} / 100`)).toBeTruthy())
    expect(screen.getByTestId('fragmentation-badge').textContent).toBe(label)
  })

  it('bounds issues and recommendations to three each', async () => {
    const issues = Array.from({ length: 4 }, (_, i) => ({
      type: 'scattered' as const, severity: 'warning' as const, cidr: `10.0.${i}.0/24`, pool_id: 7, description: `issue ${i}`,
    }))
    const recommendations = ['rec 0', 'rec 1', 'rec 2', 'rec 3', 'rec 4']
    stubAnalysis(() => jsonResponse(gapsWithBlocks(1)), () => jsonResponse(fragWith(60, { issues, recommendations })))
    renderPanel()

    await waitFor(() => expect(screen.getByText('issue 0')).toBeTruthy())
    expect(screen.getByText('issue 2')).toBeTruthy()
    expect(screen.queryByText('issue 3')).toBeNull()
    expect(screen.getByText('+1 more')).toBeTruthy()
    expect(screen.getByText('rec 2')).toBeTruthy()
    expect(screen.queryByText('rec 3')).toBeNull()
    expect(screen.getByText('+2 more')).toBeTruthy()
  })

  it('lists exactly three issues and three recommendations without a more line', async () => {
    const issues = Array.from({ length: 3 }, (_, i) => ({
      type: 'scattered' as const, severity: 'warning' as const, cidr: `10.0.${i}.0/24`, pool_id: 7, description: `issue ${i}`,
    }))
    const recommendations = ['rec 0', 'rec 1', 'rec 2']
    stubAnalysis(() => jsonResponse(gapsWithBlocks(1)), () => jsonResponse(fragWith(60, { issues, recommendations })))
    renderPanel()

    await waitFor(() => expect(screen.getByText('issue 2')).toBeTruthy())
    expect(screen.getByText('rec 2')).toBeTruthy()
    expect(screen.queryByText(/more$/)).toBeNull()
  })

  it('shows a calm healthy state for a pool without children', async () => {
    const empty: GapAnalysis = {
      ...gapsWithBlocks(0),
      allocated_blocks: null,
      available_blocks: [{ cidr: '10.0.0.0/16', address_count: 65536 }],
      used_addresses: 0,
      free_addresses: 65536,
      utilization_percent: 0,
    }
    stubAnalysis(() => jsonResponse(empty), () => jsonResponse(fragWith(0, { issues: null, recommendations: null })))
    renderPanel()

    await waitFor(() => expect(screen.getByText('No child allocations yet; the whole range is available')).toBeTruthy())
    expect(screen.queryAllByTestId('available-range')).toHaveLength(0)
    expect(screen.queryByTestId('fragmentation-badge')).toBeNull()
  })

  it('shows a gaps error and retries the request', async () => {
    let gapsFail = true
    const fetchMock = stubAnalysis(
      () => gapsFail ? jsonResponse({ error: 'gap analysis failed' }, 500) : jsonResponse(gapsWithBlocks(2)),
      () => jsonResponse(fragWith(10)),
    )
    renderPanel()

    await waitFor(() => expect(screen.getByText('gap analysis failed')).toBeTruthy())
    // The fragmentation half still renders.
    expect(screen.getByText('10 / 100')).toBeTruthy()

    gapsFail = false
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => expect(screen.getAllByTestId('available-range')).toHaveLength(2))
    expect(screen.queryByText('gap analysis failed')).toBeNull()
    expect(gapCalls(fetchMock)).toHaveLength(2)
  })

  it('collapses and expands without refetching', async () => {
    const fetchMock = stubAnalysis(() => jsonResponse(gapsWithBlocks(2)), () => jsonResponse(fragWith(10)))
    renderPanel()

    await waitFor(() => expect(screen.getAllByTestId('available-range')).toHaveLength(2))
    const toggle = screen.getByRole('button', { name: /Address Analysis/ })
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(toggle.getAttribute('aria-controls')).toBe('pool-analysis-7')
    expect(document.getElementById('pool-analysis-7')).not.toBeNull()

    fireEvent.click(toggle)
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryAllByTestId('available-range')).toHaveLength(0)
    expect(screen.queryByText('10 / 100')).toBeNull()

    fireEvent.click(toggle)
    expect(screen.getAllByTestId('available-range')).toHaveLength(2)
    expect(screen.getByText('10 / 100')).toBeTruthy()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
