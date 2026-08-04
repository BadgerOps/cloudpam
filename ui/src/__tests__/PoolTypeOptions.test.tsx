import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PoolsPage from '../pages/PoolsPage'
import BlocksPage from '../pages/BlocksPage'
import type { Block, Pool } from '../api/types'

// POOL_TYPES is stubbed with a sixth type that does not exist in the real
// constant. A <select> that hard-codes its own <option> list cannot render it;
// one that maps over POOL_TYPES must. That is the point of these tests — they
// fail against a hard-coded list and pass against a derived one, so they pin
// the wiring instead of restating today's five types.
vi.mock('../utils/poolTypes', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../utils/poolTypes')>()
  return {
    ...actual,
    POOL_TYPES: [
      ...actual.POOL_TYPES,
      { id: 'testonly' as never, label: 'Test Only Type', dot: 'bg-pink-500' },
    ],
  }
})

const mockUsePools = vi.hoisted(() => vi.fn())
const mockUseBlocks = vi.hoisted(() => vi.fn())
const mockUseAccounts = vi.hoisted(() => vi.fn())
const mockUseToast = vi.hoisted(() => vi.fn())
const mockGet = vi.hoisted(() => vi.fn())

vi.mock('../hooks/usePools', () => ({ usePools: () => mockUsePools() }))
vi.mock('../hooks/useBlocks', () => ({ useBlocks: () => mockUseBlocks() }))
vi.mock('../hooks/useAccounts', () => ({ useAccounts: () => mockUseAccounts() }))
vi.mock('../hooks/useToast', () => ({ useToast: () => mockUseToast() }))
vi.mock('../api/client', () => ({
  get: (...args: unknown[]) => mockGet(...args),
  patch: vi.fn(),
  del: vi.fn(),
}))

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

const block: Block = {
  id: 42,
  name: 'prod-subnet',
  cidr: '10.0.1.0/24',
  type: 'subnet',
  status: 'active',
  created_at: '2026-01-01T00:00:00Z',
}

function expectedIds(): string[] {
  // Read through the same stubbed module the pages see.
  return ['supernet', 'region', 'environment', 'vpc', 'subnet', 'testonly']
}

function optionValuesOf(id: string): string[] {
  const select = document.getElementById(id) as HTMLSelectElement | null
  expect(select).toBeTruthy()
  return Array.from(select!.querySelectorAll('option')).map((o) => o.value)
}

describe('pool-type <select> options come from POOL_TYPES', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUsePools.mockReturnValue({
      pools: [pool],
      hierarchy: [],
      loading: false,
      error: null,
      fetchPools: vi.fn(),
      fetchHierarchy: vi.fn(),
      createPool: vi.fn(),
      updatePool: vi.fn(),
      deletePool: vi.fn(),
    })
    mockUseBlocks.mockReturnValue({
      blocks: [block],
      total: 1,
      loading: false,
      error: null,
      fetchBlocks: vi.fn(),
    })
    mockUseAccounts.mockReturnValue({ accounts: [], fetchAccounts: vi.fn() })
    mockUseToast.mockReturnValue({ showToast: vi.fn() })
    mockGet.mockResolvedValue([])
  })

  it('lists every pool type in the create-pool select', () => {
    render(<PoolsPage />)
    fireEvent.click(screen.getByRole('button', { name: /New Pool/i }))

    expect(optionValuesOf('create-pool-type')).toEqual(expectedIds())
    expect(screen.getByRole('option', { name: 'Test Only Type' })).toBeTruthy()
  })

  it('lists every pool type in the edit-pool select', () => {
    render(<PoolsPage />)
    fireEvent.click(screen.getByTitle('Edit pool'))

    expect(optionValuesOf('edit-pool-type')).toEqual(expectedIds())
  })

  it('lists every pool type in the edit-block select', () => {
    render(
      <MemoryRouter>
        <BlocksPage />
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByTitle('Edit block'))

    expect(optionValuesOf('edit-block-type')).toEqual(expectedIds())
  })
})
