import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RecommendationsPage from '../pages/RecommendationsPage'
import type { Recommendation } from '../api/types'

const mockUseRecommendations = vi.hoisted(() => vi.fn())
const mockUsePools = vi.hoisted(() => vi.fn())
const mockUseToast = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useRecommendations', () => ({
  useRecommendations: () => mockUseRecommendations(),
}))
vi.mock('../hooks/usePools', () => ({
  usePools: () => mockUsePools(),
}))
vi.mock('../hooks/useToast', () => ({
  useToast: () => mockUseToast(),
}))

function rec(overrides: Partial<Recommendation>): Recommendation {
  return {
    id: 'r',
    pool_id: 1,
    type: 'allocation',
    status: 'pending',
    priority: 'high',
    title: '',
    description: '',
    score: 80,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

const consolidation = rec({
  id: 'c1',
  type: 'consolidation',
  title: 'Merge 10.0.1.0/25 and 10.0.1.128/25 into 10.0.1.0/24',
  suggested_cidr: '10.0.1.0/24',
  metadata: { source_cidrs: '10.0.1.0/25,10.0.1.128/25', source_pool_ids: '2,3' },
})
const resize = rec({
  id: 'r1',
  type: 'resize',
  title: 'Shrink 10.0.0.0/16 to 10.0.0.0/23',
  suggested_cidr: '10.0.0.0/23',
  metadata: { current_cidr: '10.0.0.0/16' },
})
const reclaim = rec({
  id: 'k1',
  type: 'reclaim',
  title: 'Reclaim 10.0.5.0/24',
  suggested_cidr: '10.0.5.0/24',
  metadata: { idle_days: '45' },
})

// The row actions carry title="Apply" too; the modal's button renders last.
function modalApplyButton() {
  const buttons = screen.getAllByRole('button', { name: 'Apply' })
  return buttons[buttons.length - 1]
}

describe('RecommendationsPage new types', () => {
  const apply = vi.fn().mockResolvedValue(undefined)
  const fetch = vi.fn()

  beforeEach(() => {
    apply.mockClear()
    fetch.mockClear()
    mockUseRecommendations.mockReturnValue({
      data: { items: [consolidation, resize, reclaim], total: 3, page: 1, page_size: 20 },
      loading: false,
      error: null,
      fetch,
      generate: vi.fn(),
      apply,
      dismiss: vi.fn(),
    })
    mockUsePools.mockReturnValue({
      pools: [{ id: 1, name: 'Corp', cidr: '10.0.0.0/16', type: 'supernet', status: 'active', source: 'manual', created_at: '', updated_at: '' }],
      fetchPools: vi.fn(),
    })
    mockUseToast.mockReturnValue({ showToast: vi.fn() })
  })

  it('offers the three new types as filters', () => {
    render(<RecommendationsPage />)
    const options = screen.getAllByRole('option').map((o) => o.textContent)
    expect(options).toEqual(expect.arrayContaining(['Consolidation', 'Resize', 'Reclaim']))

    const typeSelect = screen.getByDisplayValue('All Types')
    fireEvent.change(typeSelect, { target: { value: 'reclaim' } })
    expect(fetch).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'reclaim' }))
  })

  it('applies a consolidation keeping the first pool name unless one is typed', async () => {
    render(<RecommendationsPage />)
    fireEvent.click(screen.getAllByTitle('Apply')[0])

    expect(screen.getByText('Merges:').parentElement?.textContent).toContain('10.0.1.0/25 and 10.0.1.128/25')
    const name = screen.getByLabelText('Pool Name') as HTMLInputElement
    expect(name.value).toBe('')
    expect(name.placeholder).toBe('Keep the name of the pool at 10.0.1.0/25')

    fireEvent.click(modalApplyButton())
    await waitFor(() => expect(apply).toHaveBeenCalledWith('c1', undefined, undefined))

    fireEvent.click(screen.getAllByTitle('Apply')[0])
    fireEvent.change(screen.getByLabelText('Pool Name'), { target: { value: 'web' } })
    fireEvent.click(modalApplyButton())
    await waitFor(() => expect(apply).toHaveBeenLastCalledWith('c1', 'web', undefined))
  })

  it('explains a resize and a reclaim before applying them', async () => {
    render(<RecommendationsPage />)

    fireEvent.click(screen.getAllByTitle('Apply')[1])
    expect(screen.getByText(/Shrinks the pool from 10\.0\.0\.0\/16 to 10\.0\.0\.0\/23/)).toBeTruthy()
    expect(screen.queryByDisplayValue(/Consolidated|Allocation/)).toBeNull()
    fireEvent.click(modalApplyButton())
    await waitFor(() => expect(apply).toHaveBeenCalledWith('r1', undefined, undefined))

    fireEvent.click(screen.getAllByTitle('Apply')[2])
    expect(screen.getByText(/Deletes the pool 10\.0\.5\.0\/24/)).toBeTruthy()
  })
})
