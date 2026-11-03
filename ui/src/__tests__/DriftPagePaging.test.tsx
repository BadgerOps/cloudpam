import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DriftPage from '../pages/DriftPage'

const mockGet = vi.hoisted(() => vi.fn())
vi.mock('../api/client', () => ({
  get: (path: string) => mockGet(path),
  post: vi.fn(),
}))

vi.mock('../hooks/useToast', () => ({
  useToast: () => ({ showToast: vi.fn() }),
}))

function driftItems(page: number, total: number) {
  return {
    items: [],
    total,
    page,
    page_size: 50,
    summary: { total_drifts: total, by_type: {}, by_severity: {}, by_status: {} },
  }
}

function lastUrl(): URL {
  const calls = mockGet.mock.calls
  return new URL(calls[calls.length - 1][0] as string, 'http://localhost')
}

describe('DriftPage pagination', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGet.mockImplementation((path: string) => {
      const page = Number(new URL(path, 'http://localhost').searchParams.get('page') ?? '1')
      return Promise.resolve(driftItems(page, 120))
    })
  })

  it('pages forward with the active filters and returns to page 1 when a filter changes', async () => {
    render(<DriftPage />)
    await screen.findByText('Showing 1-50 of 120 drift items')
    expect(lastUrl().searchParams.get('status')).toBe('open')

    fireEvent.change(screen.getByDisplayValue('All Severities'), { target: { value: 'critical' } })
    await waitFor(() => expect(lastUrl().searchParams.get('severity')).toBe('critical'))

    fireEvent.click(screen.getByText('Next'))
    await screen.findByText('Showing 51-100 of 120 drift items')
    expect(lastUrl().searchParams.get('page')).toBe('2')
    expect(lastUrl().searchParams.get('severity')).toBe('critical')
    expect(lastUrl().searchParams.get('status')).toBe('open')

    fireEvent.change(screen.getByDisplayValue('Open'), { target: { value: 'resolved' } })
    await screen.findByText('Showing 1-50 of 120 drift items')
    expect(lastUrl().searchParams.get('page')).toBe('1')
    expect(lastUrl().searchParams.get('status')).toBe('resolved')
    expect(lastUrl().searchParams.get('severity')).toBe('critical')
  })

  it('keeps the loaded page when the next page fails and retries it from Next', async () => {
    render(<DriftPage />)
    await screen.findByText('Showing 1-50 of 120 drift items')

    mockGet.mockRejectedValueOnce(new Error('drift backend down'))
    fireEvent.click(screen.getByText('Next'))
    await screen.findByText('drift backend down')
    expect(screen.getByText('Showing 1-50 of 120 drift items')).toBeTruthy()
    expect(lastUrl().searchParams.get('page')).toBe('2')

    fireEvent.click(screen.getByText('Next'))
    await screen.findByText('Showing 51-100 of 120 drift items')
    expect(lastUrl().searchParams.get('page')).toBe('2')
  })
})
