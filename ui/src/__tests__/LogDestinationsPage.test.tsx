import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import LogDestinationsPage from '../pages/LogDestinationsPage'
import type { LogDestination } from '../api/types'

const mockUseAuth = vi.fn()
vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))

function destination(overrides: Partial<LogDestination> = {}): LogDestination {
  return {
    type: 'syslog',
    format: 'cef',
    network: 'tcp',
    address: 'siem.example.com:514',
    app_name: 'cloudpam',
    state: 'idle',
    delivered: 0,
    failed: 0,
    ...overrides,
  }
}

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const fetchMock = vi.fn()

function allow(permissions: string[]) {
  mockUseAuth.mockReturnValue({ hasPermission: (p: string) => permissions.includes(p) })
}

describe('LogDestinationsPage', () => {
  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
    allow(['settings:read'])
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('explains how to turn forwarding on when nothing is configured', async () => {
    fetchMock.mockResolvedValue(json(200, { destinations: [] }))
    render(<LogDestinationsPage />)

    expect(await screen.findByText('Audit forwarding is off')).toBeTruthy()
    expect(screen.getAllByText('CLOUDPAM_AUDIT_SYSLOG_ADDR').length).toBeGreaterThan(0)
    expect(screen.queryByTestId('log-destination-state')).toBeNull()
  })

  it('shows the destination, its state, counters and last error', async () => {
    fetchMock.mockResolvedValue(json(200, {
      destinations: [destination({
        state: 'failing',
        delivered: 12,
        failed: 2,
        last_success_at: '2026-11-02T09:58:00Z',
        last_failure_at: '2026-11-02T10:00:00Z',
        last_error: 'dial tcp 203.0.113.9:514: connection refused',
      })],
    }))
    render(<LogDestinationsPage />)

    expect((await screen.findByTestId('log-destination-state')).textContent).toBe('Failing')
    expect(screen.getByText('tcp://siem.example.com:514')).toBeTruthy()
    expect(screen.getByText('12')).toBeTruthy()
    expect(screen.getByText('2')).toBeTruthy()
    expect(screen.getByText('dial tcp 203.0.113.9:514: connection refused')).toBeTruthy()
    expect(screen.getByText(/Last send failed at/)).toBeTruthy()
  })

  it('reports a delivering destination without an error box', async () => {
    fetchMock.mockResolvedValue(json(200, {
      destinations: [destination({ network: 'udp', state: 'ok', delivered: 5, last_success_at: '2026-11-02T10:00:00Z' })],
    }))
    render(<LogDestinationsPage />)

    expect((await screen.findByTestId('log-destination-state')).textContent).toBe('Sending')
    expect(screen.queryByText(/send failed/)).toBeNull()
  })

  it('says a successful UDP send does not confirm receipt', async () => {
    fetchMock.mockResolvedValue(json(200, {
      destinations: [destination({ network: 'udp', state: 'ok', delivered: 5, last_success_at: '2026-11-02T10:00:00Z' })],
    }))
    render(<LogDestinationsPage />)

    const note = await screen.findByTestId('log-destination-receipt-note')
    expect(note.textContent).toMatch(/receipt is\s+unconfirmed/)
    expect(note.textContent).toMatch(/no collector is listening/)
    expect(screen.queryByText(/Delivering|Delivered/)).toBeNull()
  })

  it('shows why a configured destination was rejected', async () => {
    fetchMock.mockResolvedValue(json(200, {
      destinations: [destination({ network: 'http', state: 'invalid', config_error: 'unsupported syslog network "http"' })],
    }))
    render(<LogDestinationsPage />)

    expect((await screen.findByTestId('log-destination-state')).textContent).toBe('Rejected')
    expect(screen.getByText(/unsupported syslog network "http"/)).toBeTruthy()
    expect(screen.getByText(/Nothing is forwarded/)).toBeTruthy()
  })

  it('re-reads the status when Refresh is clicked', async () => {
    fetchMock
      .mockResolvedValueOnce(json(200, { destinations: [destination({ state: 'idle' })] }))
      .mockResolvedValueOnce(json(200, { destinations: [destination({ state: 'ok', delivered: 1 })] }))
    render(<LogDestinationsPage />)

    expect((await screen.findByTestId('log-destination-state')).textContent).toBe('Waiting for events')
    fireEvent.click(screen.getByRole('button', { name: /refresh/i }))
    await waitFor(() => expect(screen.getByTestId('log-destination-state').textContent).toBe('Sending'))
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('surfaces a failed status read', async () => {
    fetchMock.mockResolvedValue(json(500, { error: 'failed to load settings' }))
    render(<LogDestinationsPage />)

    expect((await screen.findByRole('alert')).textContent).toContain('failed to load settings')
  })

  it('does not request status without settings:read', () => {
    allow([])
    render(<LogDestinationsPage />)

    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.getByText(/settings:read permission/)).toBeTruthy()
  })
})
