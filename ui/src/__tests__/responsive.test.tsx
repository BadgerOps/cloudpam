import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ImportExportModal from '../components/ImportExportModal'
import PoolsPage from '../pages/PoolsPage'
import type { Pool } from '../api/types'

const mockUsePools = vi.hoisted(() => vi.fn())
const mockUseAccounts = vi.hoisted(() => vi.fn())
const mockUseToast = vi.hoisted(() => vi.fn())

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
  get: vi.fn(),
  post: vi.fn(),
  postRaw: vi.fn(),
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

function headerFor(label: string): HTMLElement {
  const th = Array.from(document.querySelectorAll('th')).find(
    el => el.textContent?.trim() === label,
  )
  if (!th) throw new Error(`no column header named ${label}`)
  return th as HTMLElement
}

describe('responsive tables', () => {
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
    mockUseAccounts.mockReturnValue({ accounts: [], fetchAccounts: vi.fn() })
    mockUseToast.mockReturnValue({ showToast: vi.fn() })
  })

  it('hides secondary pool columns below md but keeps identity and actions', () => {
    render(<PoolsPage />)

    for (const label of ['Type', 'IPs', 'Created']) {
      const th = headerFor(label)
      expect(th.classList.contains('hidden')).toBe(true)
      expect(th.classList.contains('md:table-cell')).toBe(true)
    }

    for (const label of ['Name', 'CIDR', 'Status', 'Actions']) {
      expect(headerFor(label).classList.contains('hidden')).toBe(false)
    }
  })

  it('hides the matching body cells so rows stay aligned', () => {
    render(<PoolsPage />)

    const row = document.querySelector('tbody tr') as HTMLElement
    const cells = Array.from(row.querySelectorAll('td'))
    const hidden = cells.map(td => td.classList.contains('hidden'))

    // Name, CIDR, Type, Status, IPs, Created, Actions
    expect(hidden).toEqual([false, false, true, false, true, true, false])
  })
})

describe('responsive modals', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseToast.mockReturnValue({ showToast: vi.fn() })
  })

  it('renders the import/export panel full-screen below md and as a card at md+', () => {
    render(<ImportExportModal open onClose={() => {}} />)

    const panel = screen.getByTestId('import-export-panel')
    expect(panel.classList.contains('h-full')).toBe(true)
    expect(panel.classList.contains('max-h-full')).toBe(true)
    expect(panel.classList.contains('md:h-auto')).toBe(true)
    expect(panel.classList.contains('md:max-w-4xl')).toBe(true)
    expect(panel.classList.contains('md:max-h-[85vh]')).toBe(true)
    expect(panel.classList.contains('md:rounded-xl')).toBe(true)
    // No unconditional rounding/width constraint that would letterbox mobile.
    expect(panel.classList.contains('rounded-xl')).toBe(false)
    expect(panel.classList.contains('max-w-4xl')).toBe(false)
  })
})
