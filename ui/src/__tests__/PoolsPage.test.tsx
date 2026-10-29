import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PoolsPage from '../pages/PoolsPage'
import type { Pool } from '../api/types'

const mockUsePools = vi.hoisted(() => vi.fn())
const mockUseAccounts = vi.hoisted(() => vi.fn())
const mockUseToast = vi.hoisted(() => vi.fn())
const mockGet = vi.hoisted(() => vi.fn())

vi.mock('../hooks/usePools', () => ({
  usePools: () => mockUsePools(),
}))

vi.mock('../hooks/useAccounts', () => ({
  useAccounts: () => mockUseAccounts(),
}))

vi.mock('../hooks/useToast', () => ({
  useToast: () => mockUseToast(),
}))

vi.mock('../api/client', () => ({
  get: (...args: unknown[]) => mockGet(...args),
}))

function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location">{location.pathname}{location.search}</div>
}

function renderPage(path = '/pools') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <PoolsPage />
      <LocationProbe />
    </MemoryRouter>,
  )
}

describe('PoolsPage validation', () => {
  const showToast = vi.fn()
  const fetchPools = vi.fn()
  const fetchHierarchy = vi.fn()
  const createPool = vi.fn()
  const updatePool = vi.fn()
  const deletePool = vi.fn()
  const fetchAccounts = vi.fn()

  const pool: Pool = {
    id: 1,
    name: 'prod',
    cidr: '10.0.0.0/16',
    type: 'vpc',
    status: 'active',
    source: 'manual',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }

  beforeEach(() => {
    vi.clearAllMocks()
    mockUsePools.mockReturnValue({
      pools: [pool],
      hierarchy: [],
      loading: false,
      error: null,
      fetchPools,
      fetchHierarchy,
      createPool,
      updatePool,
      deletePool,
    })
    mockUseAccounts.mockReturnValue({
      accounts: [{ id: 10, key: 'aws:111111111111', name: 'Prod account' }],
      fetchAccounts,
    })
    mockUseToast.mockReturnValue({ showToast })
  })

  it('shows inline create validation errors without submitting', () => {
    renderPage()

    fireEvent.click(screen.getByRole('button', { name: /New Pool/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Create Pool' }))

    expect(screen.getByText('Name is required')).toBeTruthy()
    expect(screen.getByText('CIDR is required')).toBeTruthy()
    expect(createPool).not.toHaveBeenCalled()
  })

  it('renders create server errors inline and as a toast', async () => {
    createPool.mockRejectedValue(new Error('CIDR overlaps an existing pool'))
    renderPage()

    fireEvent.click(screen.getByRole('button', { name: /New Pool/i }))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: ' prod child ' } })
    fireEvent.change(screen.getByLabelText('CIDR'), { target: { value: ' 10.0.1.0/24 ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create Pool' }))

    await waitFor(() => {
      expect(createPool).toHaveBeenCalledWith(expect.objectContaining({ name: 'prod child', cidr: '10.0.1.0/24' }))
      expect(screen.getByRole('alert').textContent).toContain('CIDR overlaps an existing pool')
      expect(showToast).toHaveBeenCalledWith('CIDR overlaps an existing pool', 'error')
    })
  })

  it('shows inline edit validation errors without submitting', () => {
    renderPage()

    fireEvent.click(screen.getByTitle('Edit pool'))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(screen.getByText('Name is required')).toBeTruthy()
    expect(updatePool).not.toHaveBeenCalled()
  })

  it('renders edit server errors inline and as a toast', async () => {
    updatePool.mockRejectedValue(new Error('pool update rejected'))
    renderPage()

    fireEvent.click(screen.getByTitle('Edit pool'))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'prod-renamed' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updatePool).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'prod-renamed' }))
      expect(screen.getByRole('alert').textContent).toContain('pool update rejected')
      expect(showToast).toHaveBeenCalledWith('pool update rejected', 'error')
    })
  })

  it('opens the detail panel for the pool named in ?selected', async () => {
    mockGet.mockResolvedValue({
      ...pool,
      stats: { total_ips: 65536, used_ips: 0, available_ips: 65536, utilization: 0, child_count: 0, direct_children: 0 },
      children: [],
    })
    renderPage('/pools?selected=1')

    await waitFor(() => {
      expect(mockGet).toHaveBeenCalledWith('/api/v1/pools/1')
      expect(screen.getByRole('heading', { level: 3, name: 'prod' })).toBeTruthy()
    })
  })

  it('writes ?selected when a row is opened, so the URL and the panel agree', async () => {
    mockGet.mockResolvedValue({
      ...pool,
      stats: { total_ips: 65536, used_ips: 0, available_ips: 65536, utilization: 0, child_count: 0, direct_children: 0 },
      children: [],
    })
    renderPage()

    fireEvent.click(screen.getByTitle('View details'))

    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/pools?selected=1')
      expect(screen.getByRole('heading', { level: 3, name: 'prod' })).toBeTruthy()
    })
  })
})
