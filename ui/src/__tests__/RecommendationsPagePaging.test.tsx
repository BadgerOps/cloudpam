import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RecommendationsPage from '../pages/RecommendationsPage'

const mockGet = vi.hoisted(() => vi.fn())
vi.mock('../api/client', () => ({
  get: (path: string) => mockGet(path),
  post: vi.fn(),
}))

vi.mock('../hooks/usePools', () => ({
  usePools: () => ({ pools: [], fetchPools: vi.fn() }),
}))

vi.mock('../hooks/useToast', () => ({
  useToast: () => ({ showToast: vi.fn() }),
}))

function recommendations(page: number, total: number) {
  return { items: [], total, page, page_size: 50 }
}

function lastRequestTo(prefix: string): URL {
  const urls = mockGet.mock.calls
    .map(call => new URL(call[0] as string, 'http://localhost'))
    .filter(u => u.pathname === prefix)
  return urls[urls.length - 1]
}

describe('RecommendationsPage pagination', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGet.mockImplementation((path: string) => {
      const page = Number(new URL(path, 'http://localhost').searchParams.get('page') ?? '1')
      return Promise.resolve(recommendations(page, 120))
    })
  })

  it('pages forward with the active filters and returns to page 1 when a filter changes', async () => {
    render(<RecommendationsPage />)
    await screen.findByText('Showing 1-50 of 120 recommendations')

    fireEvent.change(screen.getByDisplayValue('All Statuses'), { target: { value: 'pending' } })
    await waitFor(() => {
      const last = lastRequestTo('/api/v1/recommendations')
      expect(last.searchParams.get('status')).toBe('pending')
    })

    fireEvent.click(screen.getByText('Next'))
    await screen.findByText('Showing 51-100 of 120 recommendations')
    const pageTwo = lastRequestTo('/api/v1/recommendations')
    expect(pageTwo.searchParams.get('page')).toBe('2')
    expect(pageTwo.searchParams.get('status')).toBe('pending')

    fireEvent.change(screen.getByDisplayValue('All Types'), { target: { value: 'allocation' } })
    await screen.findByText('Showing 1-50 of 120 recommendations')
    const filtered = lastRequestTo('/api/v1/recommendations')
    expect(filtered.searchParams.get('page')).toBe('1')
    expect(filtered.searchParams.get('type')).toBe('allocation')
    expect(filtered.searchParams.get('status')).toBe('pending')
  })
})
