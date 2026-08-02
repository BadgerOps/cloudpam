import { act, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import ConfigurationPage from '../pages/ConfigurationPage'

const mocks = vi.hoisted(() => ({
  getSystemInfo: vi.fn(),
  checkForUpdates: vi.fn(),
  triggerUpgrade: vi.fn(),
  getUpgradeStatus: vi.fn(),
}))

vi.mock('../api/client', () => ({
  getSystemInfo: () => mocks.getSystemInfo(),
  checkForUpdates: (force?: boolean) => mocks.checkForUpdates(force),
  triggerUpgrade: () => mocks.triggerUpgrade(),
  getUpgradeStatus: () => mocks.getUpgradeStatus(),
}))

vi.mock('../utils/upgradeReload', () => ({
  scheduleFrontendResetAfterUpgrade: () => 0,
}))

function renderPage() {
  return render(
    <MemoryRouter>
      <ConfigurationPage />
    </MemoryRouter>,
  )
}

async function startUpgrade() {
  const button = await screen.findByRole('button', { name: 'Upgrade Now' })
  await act(async () => {
    button.click()
  })
}

describe('ConfigurationPage upgrade polling', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers({ shouldAdvanceTime: true })

    mocks.getSystemInfo.mockResolvedValue({ version: 'v1.0.0' })
    mocks.checkForUpdates.mockResolvedValue({
      update_available: true,
      upgrade_supported: true,
      latest_version: 'v1.1.0',
      checked_at: '2026-08-02T00:00:00Z',
      published_at: '2026-08-01T00:00:00Z',
    })
    mocks.triggerUpgrade.mockResolvedValue({ status: 'started', target_version: 'v1.1.0', message: '' })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  // Regression: the poll callback was an async function whose rejection
  // escaped as an unhandled rejection, so a status endpoint that went away
  // left the button disabled and the message stuck on "in progress" forever.
  it('recovers the UI when status polling keeps failing', async () => {
    mocks.getUpgradeStatus.mockRejectedValue(new Error('connection refused'))

    renderPage()
    await startUpgrade()

    // First two failures are treated as the server restarting.
    for (let i = 0; i < 2; i++) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000)
      })
    }
    expect(screen.getByRole('button', { name: 'Upgrading…' })).toHaveProperty('disabled', true)
    expect(screen.getByText('Waiting for the server to come back...')).toBeTruthy()

    // The third consecutive failure stops the poll and frees the UI.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Upgrade Now' })).toHaveProperty('disabled', false)
    })
    expect(screen.getByText(/Lost contact with the server while upgrading/)).toBeTruthy()

    // Polling has stopped; no further status calls are made.
    const callsAfterStop = mocks.getUpgradeStatus.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15000)
    })
    expect(mocks.getUpgradeStatus.mock.calls.length).toBe(callsAfterStop)
  })

  it('treats a transient failure as the server restarting and keeps polling', async () => {
    mocks.getUpgradeStatus
      .mockRejectedValueOnce(new Error('connection refused'))
      .mockResolvedValue({ status: 'running', message: 'Installing...' })

    renderPage()
    await startUpgrade()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(screen.getByText('Waiting for the server to come back...')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    await waitFor(() => expect(screen.getByText('Installing...')).toBeTruthy())
    expect(screen.getByRole('button', { name: 'Upgrading…' })).toHaveProperty('disabled', true)
  })

  it('stops polling and reports the message when the upgrade fails', async () => {
    mocks.getUpgradeStatus.mockResolvedValue({ status: 'failed', message: 'checksum mismatch' })

    renderPage()
    await startUpgrade()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Upgrade Now' })).toHaveProperty('disabled', false)
    })
    expect(screen.getByText('checksum mismatch')).toBeTruthy()
  })
})
