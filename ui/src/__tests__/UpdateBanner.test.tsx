import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UpdateBanner from '../components/UpdateBanner'

const mockUseAuth = vi.hoisted(() => vi.fn())
const mockCheckForUpdates = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))

vi.mock('../api/client', () => ({
  checkForUpdates: (force?: boolean) => mockCheckForUpdates(force),
  triggerUpgrade: vi.fn(),
  getUpgradeStatus: vi.fn(),
  acknowledgeUpgradeStatus: vi.fn(),
}))

vi.mock('../utils/upgradeReload', () => ({
  scheduleFrontendResetAfterUpgrade: vi.fn(),
}))

const originalLocalStorage = Object.getOwnPropertyDescriptor(window, 'localStorage')

function installThrowingLocalStorage() {
  const throwing = {
    getItem: () => {
      throw new DOMException('The operation is insecure.', 'SecurityError')
    },
    setItem: () => {
      throw new DOMException('The operation is insecure.', 'SecurityError')
    },
    removeItem: () => {
      throw new DOMException('The operation is insecure.', 'SecurityError')
    },
    clear: () => {},
    key: () => null,
    length: 0,
  }
  Object.defineProperty(window, 'localStorage', { value: throwing, configurable: true })
}

describe('UpdateBanner storage resilience', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseAuth.mockReturnValue({ role: 'admin' })
    mockCheckForUpdates.mockResolvedValue({
      current_version: '0.8.0',
      latest_version: '0.9.0',
      update_available: true,
      release_notes: '',
      upgrade_supported: false,
      checked_at: '2026-01-01T00:00:00Z',
    })
  })

  afterEach(() => {
    if (originalLocalStorage) {
      Object.defineProperty(window, 'localStorage', originalLocalStorage)
    }
    window.localStorage.clear()
  })

  it('renders the banner when reading the dismissed version throws', async () => {
    installThrowingLocalStorage()

    render(<UpdateBanner />)

    expect(await screen.findByText('CloudPAM v0.9.0 is available')).toBeTruthy()
  })

  it('dismisses without crashing when writing to storage throws', async () => {
    installThrowingLocalStorage()

    render(<UpdateBanner />)
    const dismiss = await screen.findByRole('button', { name: 'Dismiss' })

    expect(() => fireEvent.click(dismiss)).not.toThrow()

    await waitFor(() => {
      expect(screen.queryByText('CloudPAM v0.9.0 is available')).toBeNull()
    })
  })

  it('persists the dismissed version when storage works', async () => {
    render(<UpdateBanner />)
    fireEvent.click(await screen.findByRole('button', { name: 'Dismiss' }))

    await waitFor(() => {
      expect(window.localStorage.getItem('cloudpam_dismissed_update_version')).toBe('0.9.0')
    })
  })

  it('hides the banner for an already dismissed version', async () => {
    window.localStorage.setItem('cloudpam_dismissed_update_version', '0.9.0')

    render(<UpdateBanner />)

    await waitFor(() => {
      expect(mockCheckForUpdates).toHaveBeenCalled()
    })
    expect(screen.queryByText('CloudPAM v0.9.0 is available')).toBeNull()
  })
})
