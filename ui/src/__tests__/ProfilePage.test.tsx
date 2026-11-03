import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AuthContext } from '../hooks/useAuth'
import type { AuthContextValue } from '../hooks/useAuth'
import { ToastContext } from '../hooks/useToast'
import type { UserInfo } from '../api/types'
import ProfilePage from '../pages/ProfilePage'

const BASE_USER: UserInfo = {
  id: 'u1',
  username: 'jane',
  role: 'viewer',
  is_active: true,
  created_at: '2024-01-01T00:00:00Z',
  failed_login_attempts: 0,
}

interface Spies {
  logout?: () => void
  showToast?: (message: string, type?: 'info' | 'error' | 'success') => void
}

function renderProfile(currentUser: UserInfo, spies: Spies = {}) {
  const value: AuthContextValue = {
    keyName: currentUser.username,
    role: currentUser.role,
    authType: 'session',
    currentUser,
    permissions: [],
    hasPermission: () => false,
    isAuthenticated: true,
    authEnabled: true,
    localAuthEnabled: true,
    needsSetup: false,
    authChecked: true,
    loginWithPassword: async () => {},
    logout: spies.logout ?? (() => {}),
  }
  return render(
    <AuthContext.Provider value={value}>
      <ToastContext.Provider value={{ toasts: [], showToast: spies.showToast ?? (() => {}) }}>
        <ProfilePage />
      </ToastContext.Provider>
    </AuthContext.Provider>
  )
}

function stubRevokeFetch(status: number, body: unknown) {
  const fetchMock = vi.fn(async () =>
    new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('ProfilePage password section', () => {
  it('shows the Change Password form to a local user', () => {
    renderProfile({ ...BASE_USER, auth_provider: 'local' })

    expect(screen.getByRole('heading', { name: 'Change Password' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Change Password' })).toBeTruthy()
    expect(document.querySelectorAll('input[type="password"]').length).toBe(3)
  })

  it('shows the form to a user created before auth_provider existed', () => {
    renderProfile(BASE_USER)

    expect(screen.getByRole('heading', { name: 'Change Password' })).toBeTruthy()
  })

  it('hides the form from an OIDC-provisioned user and says where the password lives', () => {
    renderProfile({
      ...BASE_USER,
      auth_provider: 'oidc',
      oidc_issuer: 'https://idp.example.test/realms/corp',
    })

    expect(screen.queryByRole('heading', { name: 'Change Password' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Change Password' })).toBeNull()
    expect(document.querySelectorAll('input[type="password"]').length).toBe(0)
    expect(screen.getByText(/managed by your identity provider/i)).toBeTruthy()
    expect(screen.getByText('https://idp.example.test/realms/corp')).toBeTruthy()
  })

  it('still says where the password lives when the issuer is unknown', () => {
    renderProfile({ ...BASE_USER, auth_provider: 'oidc' })

    expect(screen.queryByRole('heading', { name: 'Change Password' })).toBeNull()
    expect(screen.getByText(/managed by your identity provider/i)).toBeTruthy()
  })
})

describe('ProfilePage sign out everywhere', () => {
  it('revokes every session, reports it, then signs this browser out', async () => {
    const fetchMock = stubRevokeFetch(200, { status: 'sessions revoked' })
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const calls: string[] = []
    renderProfile(BASE_USER, {
      logout: () => { calls.push('logout') },
      showToast: (message, type) => { calls.push(`toast:${type}:${message}`) },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Sign out everywhere' }))

    await waitFor(() => expect(calls).toContain('logout'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/v1/auth/users/u1/revoke-sessions')
    expect(init.method).toBe('POST')
    expect(calls).toEqual(['toast:success:Signed out everywhere. Sign in again to continue.', 'logout'])
  })

  it('does nothing when the confirmation is declined', () => {
    const fetchMock = stubRevokeFetch(200, { status: 'sessions revoked' })
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    const logout = vi.fn()
    renderProfile(BASE_USER, { logout })

    fireEvent.click(screen.getByRole('button', { name: 'Sign out everywhere' }))

    expect(fetchMock).not.toHaveBeenCalled()
    expect(logout).not.toHaveBeenCalled()
  })

  it('keeps the session and shows the server error when the revoke fails', async () => {
    stubRevokeFetch(500, { error: 'failed to revoke sessions' })
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const logout = vi.fn()
    const showToast = vi.fn()
    renderProfile(BASE_USER, { logout, showToast })

    fireEvent.click(screen.getByRole('button', { name: 'Sign out everywhere' }))

    await waitFor(() => expect(showToast).toHaveBeenCalledWith('failed to revoke sessions', 'error'))
    expect(logout).not.toHaveBeenCalled()
  })
})
