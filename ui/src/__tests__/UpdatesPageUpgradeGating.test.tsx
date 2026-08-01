import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import UpdatesPage from '../pages/UpdatesPage'
import type { UpdateStatusResponse } from '../api/types'

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

vi.mock('../utils/upgradeReload', () => ({
  scheduleFrontendResetAfterUpgrade: vi.fn(),
}))

describe('UpdatesPage upgrade gating', () => {
  const showToast = vi.fn()
  const triggerUpgrade = vi.fn()

  function setup(status: UpdateStatusResponse, summaryOverrides: Record<string, unknown> = {}) {
    mockUseAuth.mockReturnValue({ role: 'admin' })
    mockUseToast.mockReturnValue({ showToast })
    mockUseUpdates.mockReturnValue({
      summary: {
        current_version: '0.8.0',
        latest_version: '0.9.0',
        update_available: true,
        ...summaryOverrides,
      },
      status,
      loadingSummary: false,
      loadingStatus: false,
      actionLoading: false,
      summaryError: null,
      statusError: null,
      actionError: null,
      refreshSummary: vi.fn().mockResolvedValue(undefined),
      refreshStatus: vi.fn().mockResolvedValue(undefined),
      triggerUpgrade,
      acknowledgeUpgradeStatus: vi.fn().mockResolvedValue({}),
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('submits only one upgrade request when the button is clicked twice', async () => {
    setup({ status: 'idle' })
    let resolveTrigger: (value: { status: string; target_version: string }) => void = () => {}
    triggerUpgrade.mockReturnValue(
      new Promise(res => {
        resolveTrigger = res
      }),
    )

    render(<UpdatesPage />)

    const button = screen.getByRole('button', { name: 'Upgrade to v0.9.0' })
    fireEvent.click(button)
    fireEvent.click(button)

    expect(triggerUpgrade).toHaveBeenCalledTimes(1)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Upgrade to v0.9.0' })).toHaveProperty('disabled', true)
    })

    resolveTrigger({ status: 'upgrade_requested', target_version: '0.9.0' })

    // Still gated afterwards, because an upgrade is now pending
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Upgrade to v0.9.0' })).toHaveProperty('disabled', true)
    })
    expect(triggerUpgrade).toHaveBeenCalledTimes(1)
  })

  it('disables the upgrade button while a host upgrade is already running', () => {
    setup({ status: 'running', target_version: '0.9.0' })

    render(<UpdatesPage />)

    expect(screen.getByRole('button', { name: 'Upgrade to v0.9.0' })).toHaveProperty('disabled', true)
  })

  it('disables the upgrade button when the server reports upgrades are unsupported', () => {
    setup({ status: 'idle' }, { upgrade_supported: false })

    render(<UpdatesPage />)

    const button = screen.getByRole('button', { name: 'Upgrade to v0.9.0' })
    expect(button).toHaveProperty('disabled', true)
    expect(button.getAttribute('title')).toBe('In-app upgrades are not supported by this deployment')

    fireEvent.click(button)
    expect(triggerUpgrade).not.toHaveBeenCalled()
  })

  it('enables the upgrade button when the server reports upgrades are supported', () => {
    setup({ status: 'idle' }, { upgrade_supported: true })

    render(<UpdatesPage />)

    const button = screen.getByRole('button', { name: 'Upgrade to v0.9.0' })
    expect(button).toHaveProperty('disabled', false)
    expect(button.getAttribute('title')).toBeNull()
  })
})
