import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
  // The detail panel requests gap and fragmentation analysis; leave those
  // pending so these tests only see the pool data they control.
  post: () => new Promise(() => {}),
}))

function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location">{location.pathname}{location.search}</div>
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(r => { resolve = r })
  return { promise, resolve }
}

function withStats(p: Pool) {
  return {
    ...p,
    stats: { total_ips: 65536, used_ips: 0, available_ips: 65536, utilization: 0, child_count: 0, direct_children: 0 },
    children: [],
  }
}

function panel() {
  const heading = screen.queryByRole('heading', { level: 3 })
  return heading ? within(heading.parentElement!.parentElement!) : null
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

  it('never shows the previous pool once ?selected names a different id', async () => {
    const staging: Pool = { ...pool, id: 2, name: 'staging', cidr: '10.1.0.0/16' }
    mockUsePools.mockReturnValue({
      pools: [pool, staging], hierarchy: [], loading: false, error: null,
      fetchPools, fetchHierarchy, createPool, updatePool, deletePool,
    })
    const second = deferred<ReturnType<typeof withStats>>()
    mockGet.mockImplementation((url: string) => url === '/api/v1/pools/1' ? Promise.resolve(withStats(pool)) : second.promise)
    renderPage('/pools?selected=1')

    await waitFor(() => {
      expect(panel()!.getByText('10.0.0.0/16')).toBeTruthy()
    })

    fireEvent.click(screen.getAllByTitle('View details')[1])

    expect(screen.getByTestId('location').textContent).toBe('/pools?selected=2')
    expect(panel()).toBeNull()

    second.resolve(withStats(staging))

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 3, name: 'staging' })).toBeTruthy()
    })
    expect(panel()!.getByText('10.1.0.0/16')).toBeTruthy()
    expect(panel()!.queryByText('10.0.0.0/16')).toBeNull()
  })

  it('closes the panel and drops ?selected when the pool cannot be loaded', async () => {
    mockGet.mockRejectedValue(new Error('pool not found'))
    renderPage('/pools?selected=99')

    await waitFor(() => {
      expect(showToast).toHaveBeenCalledWith('pool not found', 'error')
      expect(screen.getByTestId('location').textContent).toBe('/pools')
    })
    expect(panel()).toBeNull()
  })

  it('reloads the open panel after the pool is saved', async () => {
    mockGet.mockResolvedValueOnce(withStats(pool))
    renderPage('/pools?selected=1')
    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 3, name: 'prod' })).toBeTruthy()
    })

    const renamed: Pool = { ...pool, name: 'prod-renamed' }
    updatePool.mockResolvedValue(renamed)
    mockGet.mockResolvedValueOnce(withStats(renamed))
    fireEvent.click(screen.getByTitle('Edit pool'))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'prod-renamed' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 3, name: 'prod-renamed' })).toBeTruthy()
    })
    expect(screen.getByTestId('location').textContent).toBe('/pools?selected=1')
  })

  it('does not reopen a panel closed while a save was still refreshing', async () => {
    mockGet.mockResolvedValue(withStats(pool))
    const refresh = deferred<void>()
    fetchPools.mockReturnValue(refresh.promise)
    renderPage('/pools?selected=1')
    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 3, name: 'prod' })).toBeTruthy()
    })

    updatePool.mockResolvedValue({ ...pool, name: 'prod-renamed' })
    fireEvent.click(screen.getByTitle('Edit pool'))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'prod-renamed' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(fetchPools).toHaveBeenCalled()
    })

    const heading = screen.getByRole('heading', { level: 3, name: 'prod' })
    fireEvent.click(heading.parentElement!.querySelector('button')!)
    expect(screen.getByTestId('location').textContent).toBe('/pools')

    refresh.resolve()
    await waitFor(() => {
      expect(fetchHierarchy).toHaveBeenCalled()
    })
    expect(panel()).toBeNull()
    expect(screen.getByTestId('location').textContent).toBe('/pools')
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
