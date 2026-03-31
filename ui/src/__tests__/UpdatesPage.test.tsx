import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import UpdatesPage from '../pages/UpdatesPage'

const mockUseAuth = vi.fn()
const mockUseToast = vi.fn()
const mockUseUpdates = vi.fn()

vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))

vi.mock('../hooks/useToast', () => ({
  useToast: () => mockUseToast(),
}))

vi.mock('../hooks/useUpdates', () => ({
  useUpdates: () => mockUseUpdates(),
}))

describe('UpdatesPage', () => {
  const showToast = vi.fn()
  const triggerUpgrade = vi.fn()
  const refreshSummary = vi.fn()
  const refreshStatus = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()

    mockUseAuth.mockReturnValue({ role: 'admin' })
    mockUseToast.mockReturnValue({ showToast })
    mockUseUpdates.mockReturnValue({
      summary: {
        current_version: '0.8.0',
        latest_version: '0.9.0',
        update_available: true,
        release_notes: 'Release notes for v0.9.0',
        release_url: 'https://example.com/releases/v0.9.0',
        published_at: '2026-03-31T00:00:00Z',
        checked_at: '2026-03-31T12:00:00Z',
      },
      status: { status: 'idle' },
      loadingSummary: false,
      loadingStatus: false,
      actionLoading: false,
      summaryError: null,
      statusError: null,
      actionError: null,
      refreshSummary,
      refreshStatus,
      triggerUpgrade,
    })

    triggerUpgrade.mockResolvedValue({
      status: 'upgrade_requested',
      target_version: '0.9.0',
    })
    refreshSummary.mockResolvedValue(undefined)
    refreshStatus.mockResolvedValue(undefined)
  })

  it('shows updater details for admins', () => {
    render(<UpdatesPage />)

    expect(screen.getByRole('heading', { name: 'Updates' })).toBeTruthy()
    expect(screen.getByText('Release notes for v0.9.0')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Upgrade to v0.9.0' })).toBeTruthy()
    expect(screen.getByText('Latest stable release information from GitHub.')).toBeTruthy()
  })

  it('submits an upgrade request and shows a success toast', async () => {
    render(<UpdatesPage />)

    fireEvent.click(screen.getByRole('button', { name: 'Upgrade to v0.9.0' }))

    await waitFor(() => {
      expect(triggerUpgrade).toHaveBeenCalledTimes(1)
      expect(showToast).toHaveBeenCalledWith('Upgrade request for v0.9.0 submitted', 'success')
    })
  })

  it('blocks non-admin users from the updater controls', () => {
    mockUseAuth.mockReturnValue({ role: 'viewer' })

    render(<UpdatesPage />)

    expect(screen.getByText('Only administrators can view release metadata or trigger an in-app upgrade.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Upgrade to/i })).toBeNull()
  })
})
