import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AuthContext } from '../hooks/useAuth'
import type { AuthContextValue } from '../hooks/useAuth'
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

function renderProfile(currentUser: UserInfo) {
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
    logout: () => {},
  }
  return render(
    <AuthContext.Provider value={value}>
      <ProfilePage />
    </AuthContext.Provider>
  )
}

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
