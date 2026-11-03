import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import BlocksPage from '../pages/BlocksPage'
import type { Block } from '../api/types'

const mockGet = vi.hoisted(() => vi.fn())
const mockDel = vi.hoisted(() => vi.fn())
vi.mock('../api/client', () => ({
  get: (path: string) => mockGet(path),
  patch: vi.fn(),
  del: (path: string) => mockDel(path),
}))

vi.mock('../hooks/useAccounts', () => ({
  useAccounts: () => ({
    accounts: [{ id: 7, key: 'prod', name: 'Prod Account', created_at: '2026-01-01T00:00:00Z' }],
    fetchAccounts: vi.fn(),
  }),
}))

vi.mock('../hooks/useToast', () => ({
  useToast: () => ({ showToast: vi.fn() }),
}))

function block(id: number): Block {
  return {
    id,
    name: `block-${id}`,
    cidr: `10.0.${id}.0/24`,
    type: 'subnet',
    status: 'active',
    created_at: '2026-01-01T00:00:00Z',
  }
}

function blocksRequests(): URL[] {
  return mockGet.mock.calls
    .map(call => new URL(call[0] as string, 'http://localhost'))
    .filter(u => u.pathname === '/api/v1/blocks')
}

function lastBlocksRequest(): URL {
  const urls = blocksRequests()
  return urls[urls.length - 1]
}

let total: number

describe('BlocksPage pagination', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    total = 120
    mockGet.mockImplementation((path: string) => {
      const page = Number(new URL(path, 'http://localhost').searchParams.get('page') ?? '1')
      const start = (page - 1) * 50
      const count = Math.max(0, Math.min(50, total - start))
      const items = Array.from({ length: count }, (_, i) => block(start + i + 1))
      return Promise.resolve({ items, total, page, page_size: 50 })
    })
  })

  function renderPage() {
    return render(
      <MemoryRouter>
        <BlocksPage />
      </MemoryRouter>,
    )
  }

  it('pages forward with the account filter and returns to page 1 when it changes', async () => {
    renderPage()
    await screen.findByText('Showing 1-50 of 120 blocks')

    fireEvent.change(screen.getByDisplayValue('All Accounts'), { target: { value: '7' } })
    await waitFor(() => expect(lastBlocksRequest().searchParams.get('accounts')).toBe('7'))

    fireEvent.click(screen.getByText('Next'))
    await screen.findByText('Showing 51-100 of 120 blocks')
    expect(lastBlocksRequest().searchParams.get('page')).toBe('2')
    expect(lastBlocksRequest().searchParams.get('accounts')).toBe('7')
    expect(screen.getByText('block-51')).toBeTruthy()

    fireEvent.change(screen.getByDisplayValue('Prod Account'), { target: { value: '' } })
    await screen.findByText('Showing 1-50 of 120 blocks')
    expect(lastBlocksRequest().searchParams.get('page')).toBe('1')
    expect(lastBlocksRequest().searchParams.get('accounts')).toBeNull()
  })

  it('refetches the current page after a delete and steps back when the page empties', async () => {
    total = 51
    mockDel.mockResolvedValue({})
    renderPage()
    await screen.findByText('Showing 1-50 of 51 blocks')

    fireEvent.click(screen.getByText('Next'))
    await screen.findByText('Showing 51-51 of 51 blocks')

    total = 50
    fireEvent.click(screen.getByTitle('Delete block'))
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(mockDel).toHaveBeenCalledWith('/api/v1/pools/51'))

    await screen.findByText('Showing 1-50 of 50 blocks')
    expect(lastBlocksRequest().searchParams.get('page')).toBe('1')
  })
})
