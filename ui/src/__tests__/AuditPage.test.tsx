import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AuditPage from '../pages/AuditPage'

const mockGet = vi.hoisted(() => vi.fn())
vi.mock('../api/client', () => ({
  get: (path: string) => mockGet(path),
}))

function auditResponse(total: number) {
  return {
    events: [
      {
        id: 'e1',
        timestamp: '2026-01-01T00:00:00Z',
        action: 'create',
        resource_type: 'pool',
        resource_id: '1',
      },
    ],
    total,
    limit: 25,
    offset: 0,
  }
}

function lastUrl(): URL {
  const path = mockGet.mock.calls[mockGet.mock.calls.length - 1][0] as string
  return new URL(path, 'http://localhost')
}

describe('AuditPage pagination', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGet.mockResolvedValue(auditResponse(100))
  })

  it('keeps the active filters when paging forward and back', async () => {
    render(<AuditPage />)
    await screen.findByText('Showing 1-25 of 100 events')

    fireEvent.change(screen.getByDisplayValue('All Actions'), { target: { value: 'create' } })
    await waitFor(() => expect(lastUrl().searchParams.get('action')).toBe('create'))

    fireEvent.click(screen.getByText('Next'))
    await screen.findByText('Showing 26-50 of 100 events')
    expect(lastUrl().searchParams.get('offset')).toBe('25')
    expect(lastUrl().searchParams.get('action')).toBe('create')

    fireEvent.click(screen.getByText('Previous'))
    await screen.findByText('Showing 1-25 of 100 events')
    expect(lastUrl().searchParams.get('offset')).toBe('0')
    expect(lastUrl().searchParams.get('action')).toBe('create')
  })

  it('returns to the first page when a filter changes', async () => {
    render(<AuditPage />)
    await screen.findByText('Showing 1-25 of 100 events')

    fireEvent.click(screen.getByText('Next'))
    await screen.findByText('Showing 26-50 of 100 events')

    fireEvent.change(screen.getByDisplayValue('All Resources'), { target: { value: 'pool' } })
    await screen.findByText('Showing 1-25 of 100 events')
    expect(lastUrl().searchParams.get('offset')).toBe('0')
    expect(lastUrl().searchParams.get('resource_type')).toBe('pool')
  })

  it('does not page past the end', async () => {
    mockGet.mockResolvedValue(auditResponse(20))
    render(<AuditPage />)
    await screen.findByText('Showing 1-20 of 20 events')

    expect((screen.getByText('Next') as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByText('Previous') as HTMLButtonElement).disabled).toBe(true)
  })
})
