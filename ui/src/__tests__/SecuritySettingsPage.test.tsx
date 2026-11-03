import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SecuritySettingsPage from '../pages/SecuritySettingsPage'
import type { SecuritySettings } from '../hooks/useSettings'

const mockUseSecuritySettings = vi.hoisted(() => vi.fn())
const mockUseAuth = vi.hoisted(() => vi.fn())
const showToast = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useSettings', () => ({
  useSecuritySettings: () => mockUseSecuritySettings(),
}))
vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))
vi.mock('../hooks/useToast', () => ({
  useToast: () => ({ showToast }),
}))

const settings: SecuritySettings = {
  session_duration_hours: 24,
  max_sessions_per_user: 5,
  password_min_length: 12,
  password_max_length: 72,
  login_rate_limit_per_minute: 5,
  account_lockout_attempts: 10,
  account_lockout_cooldown_minutes: 15,
  audit_retention_days: 0,
  trusted_proxies: ['10.0.0.0/8'],
  local_auth_enabled: true,
  api_key_default_expiry_days: 90,
  api_key_max_lifetime_days: 365,
  api_key_rotation_reminder_days: 30,
  api_key_allowed_scopes_by_role: { admin: ['*'] },
}

const updateSettings = vi.fn()

function renderPage(canEdit = true) {
  mockUseAuth.mockReturnValue({ hasPermission: (p: string) => canEdit && p === 'settings:write' })
  mockUseSecuritySettings.mockReturnValue({ settings, loading: false, error: null, updateSettings })
  return render(
    <MemoryRouter>
      <SecuritySettingsPage />
    </MemoryRouter>,
  )
}

function input(label: string): HTMLInputElement {
  return screen.getByLabelText(label) as HTMLInputElement
}

beforeEach(() => {
  vi.clearAllMocks()
  updateSettings.mockResolvedValue(settings)
})

describe('SecuritySettingsPage', () => {
  it('labels every control so it can be reached by name', () => {
    renderPage()

    expect(input('Session Duration (hours)').value).toBe('24')
    expect(input('Max Sessions per User').value).toBe('5')
    expect(input('Minimum Length').value).toBe('12')
    expect(input('Maximum Length').value).toBe('72')
    expect(input('Default Expiry (days)').value).toBe('90')
    expect(input('Max Lifetime (days)').value).toBe('365')
    expect(input('Rotation Reminder (days)').value).toBe('30')
    expect(input('Rate Limit (attempts/minute)').value).toBe('5')
    expect(input('Account Lockout Threshold').value).toBe('10')
    expect(input('Lockout Cooldown (minutes)').value).toBe('15')
    expect((screen.getByLabelText('Trusted Proxy CIDRs') as HTMLTextAreaElement).value).toBe('10.0.0.0/8')
  })

  it('saves edited numbers and one proxy per line', async () => {
    renderPage()

    fireEvent.change(input('Session Duration (hours)'), { target: { value: '48' } })
    fireEvent.change(input('Maximum Length'), { target: { value: '200' } })
    fireEvent.change(screen.getByLabelText('Trusted Proxy CIDRs'), {
      target: { value: '10.0.0.0/8\n\n 192.168.0.0/16 \n' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save Settings' }))

    await waitFor(() => expect(updateSettings).toHaveBeenCalledTimes(1))
    expect(updateSettings).toHaveBeenCalledWith({
      ...settings,
      session_duration_hours: 48,
      password_max_length: 72,
      trusted_proxies: ['10.0.0.0/8', '192.168.0.0/16'],
    })
    expect(showToast).toHaveBeenCalledWith('Security settings saved', 'success')
  })

  it('hides Save without settings:write', () => {
    renderPage(false)

    expect(screen.queryByRole('button', { name: 'Save Settings' })).toBeNull()
    expect(screen.getByText(/settings:write required/)).toBeTruthy()
  })
})
