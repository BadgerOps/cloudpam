import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DriftPage from '../pages/DriftPage'
import type { DriftItem, DriftListResponse } from '../api/types'

const mockUseDrift = vi.hoisted(() => vi.fn())
const mockUseToast = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useDrift', () => ({
  useDrift: () => mockUseDrift(),
}))

vi.mock('../hooks/useToast', () => ({
  useToast: () => mockUseToast(),
}))

function item(overrides: Partial<DriftItem>): DriftItem {
  return {
    id: 'drift-1',
    account_id: 7,
    resource_id: 'res-1',
    type: 'unmanaged',
    severity: 'warning',
    status: 'open',
    title: 'Unmanaged vpc: prod',
    description: '',
    resource_cidr: '10.1.4.0/22',
    detected_at: '2026-11-03T00:00:00Z',
    updated_at: '2026-11-03T00:00:00Z',
    ...overrides,
  }
}

function list(items: DriftItem[]): DriftListResponse {
  return {
    items,
    total: items.length,
    page: 1,
    page_size: 50,
    summary: { total_drifts: items.length, by_severity: {}, by_type: {}, accounts_scanned: 0, resources_scanned: 0, pools_scanned: 0 },
  }
}

const fetch = vi.fn()
const applySuggestion = vi.fn()
const showToast = vi.fn()

function mockDrift(items: DriftItem[]) {
  mockUseDrift.mockReturnValue({
    data: list(items),
    loading: false,
    error: null,
    fetch,
    detect: vi.fn(),
    resolve: vi.fn(),
    ignore: vi.fn(),
    applySuggestion,
  })
}

describe('DriftPage suggestions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseToast.mockReturnValue({ showToast })
  })

  it('shows the suggestion and applies it in one click', async () => {
    const create = item({
      suggested_action: 'create_pool',
      suggested_pool_id: 2,
      suggestion_reason: 'create a pool under containing pool 2',
    })
    mockDrift([create])
    applySuggestion.mockResolvedValue({ ...create, status: 'resolved' })

    render(<DriftPage />)
    expect(screen.getByText(/Suggested: Create pool under #2/)).toBeTruthy()

    fetch.mockClear()
    fireEvent.click(screen.getByRole('button', { name: 'Apply suggestion: Create pool under #2' }))

    await waitFor(() => expect(applySuggestion).toHaveBeenCalledWith(create))
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('Create pool under #2: applied and resolved', 'success'))
    expect(fetch).toHaveBeenCalled()
  })

  it('offers no apply button for a review suggestion', () => {
    mockDrift([item({ suggested_action: 'review', suggestion_reason: 'the CIDR overlaps managed pools [4]' })])

    render(<DriftPage />)

    expect(screen.getByText(/Suggested: Review by hand/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Apply suggestion/ })).toBeNull()
  })

  it('reports a failed apply as an error', async () => {
    mockDrift([item({ suggested_action: 'link_pool', suggested_pool_id: 3, suggestion_reason: 'pool 3 has exactly this CIDR' })])
    applySuggestion.mockRejectedValue(new Error('pool not found'))

    render(<DriftPage />)
    fireEvent.click(screen.getByRole('button', { name: 'Apply suggestion: Link to pool #3' }))

    await waitFor(() => expect(showToast).toHaveBeenCalledWith('pool not found', 'error'))
  })
})
