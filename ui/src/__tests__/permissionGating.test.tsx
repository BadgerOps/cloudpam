import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ReactElement } from 'react'
import { AuthContext } from '../hooks/useAuth'
import type { AuthContextValue } from '../hooks/useAuth'
import ApiKeysPage from '../pages/ApiKeysPage'
import IdentityPage from '../pages/IdentityPage'
import SecuritySettingsPage from '../pages/SecuritySettingsPage'
import UsersAdminPanel from '../components/UsersAdminPanel'

const SECURITY_SETTINGS = {
  session_duration_hours: 24,
  session_idle_timeout_minutes: 60,
  max_sessions_per_user: 5,
  password_min_length: 12,
  password_require_complexity: false,
  login_max_attempts: 5,
  login_lockout_minutes: 15,
  trusted_proxies: [],
  local_auth_enabled: true,
  api_key_max_scopes_role: 'admin',
}

const USERS = [
  {
    id: 'u1',
    username: 'alice',
    email: 'alice@example.com',
    display_name: 'Alice',
    role: 'operator',
    is_active: true,
    failed_login_attempts: 0,
    created_at: '2024-01-01T00:00:00Z',
    last_login_at: '2024-01-02T00:00:00Z',
  },
]

// Minimal stub API surface: every screen under test loads via api/client's
// fetch wrapper, so route the handful of GETs they issue.
function stubFetch() {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    const body = (data: unknown) =>
      new Response(JSON.stringify(data), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })

    if (url.startsWith('/api/v1/settings/security')) return body(SECURITY_SETTINGS)
    if (url.startsWith('/api/v1/settings/oidc/providers')) return body({ providers: [] })
    if (url.startsWith('/api/v1/auth/users')) return body({ users: USERS, total: USERS.length })
    if (url.startsWith('/api/v1/auth/roles')) return body({ roles: [] })
    if (url.startsWith('/api/v1/auth/permissions')) return body({ permissions: [] })
    if (url.startsWith('/api/v1/auth/keys')) return body({ keys: [] })
    return body({})
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function authValue(permissions: string[], role = 'operator'): AuthContextValue {
  return {
    keyName: 'test',
    role,
    authType: 'session',
    currentUser: null,
    permissions,
    hasPermission: (permission: string) => role === 'admin' || permissions.includes(permission),
    isAuthenticated: true,
    authEnabled: true,
    localAuthEnabled: true,
    needsSetup: false,
    authChecked: true,
    loginWithPassword: async () => {},
    logout: () => {},
  }
}

function renderWith(permissions: string[], ui: ReactElement, role = 'operator') {
  return render(
    <AuthContext.Provider value={authValue(permissions, role)}>
      <MemoryRouter>{ui}</MemoryRouter>
    </AuthContext.Provider>
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('SecuritySettingsPage read-only gating', () => {
  it('hides the save control when the caller only holds settings:read', async () => {
    stubFetch()
    renderWith(['settings:read'], <SecuritySettingsPage />)

    expect(await screen.findByText(/settings:write required/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /save settings/i })).toBeNull()
  })

  it('shows the save control when the caller holds settings:write', async () => {
    stubFetch()
    renderWith(['settings:read', 'settings:write'], <SecuritySettingsPage />)

    expect(await screen.findByRole('button', { name: /save settings/i })).toBeTruthy()
    expect(screen.queryByText(/settings:write required/i)).toBeNull()
  })
})

describe('UsersAdminPanel action gating', () => {
  it('exposes no mutating controls to a caller holding only users:list', async () => {
    stubFetch()
    renderWith(['users:list'], <UsersAdminPanel />)

    expect(await screen.findByText('alice')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /create user/i })).toBeNull()
    expect(screen.queryByTitle('Deactivate user')).toBeNull()
    expect(screen.queryByTitle('Activate user')).toBeNull()
  })

  it('exposes create to a caller holding users:create', async () => {
    stubFetch()
    renderWith(['users:list', 'users:create'], <UsersAdminPanel />)

    expect(await screen.findByRole('button', { name: /create user/i })).toBeTruthy()
    // Still no deactivate: that needs users:delete.
    expect(screen.queryByTitle('Deactivate user')).toBeNull()
  })

  it('exposes deactivate to a caller holding users:delete', async () => {
    stubFetch()
    renderWith(['users:list', 'users:delete'], <UsersAdminPanel />)

    expect(await screen.findByTitle('Deactivate user')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /create user/i })).toBeNull()
  })
})

describe('ApiKeysPage feature gating', () => {
  it('refuses to list keys for a caller holding only settings:read', async () => {
    const fetchMock = stubFetch()
    renderWith(['settings:read'], <ApiKeysPage />)

    expect(await screen.findByText(/apikeys:list or apikeys:read is required/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /create key/i })).toBeNull()

    // The privileged listing request must never be issued.
    const keyRequests = fetchMock.mock.calls.filter(([input]) =>
      String(input).startsWith('/api/v1/auth/keys')
    )
    expect(keyRequests).toHaveLength(0)
  })

  it('lists keys but hides create for a caller holding only apikeys:list', async () => {
    const fetchMock = stubFetch()
    renderWith(['settings:read', 'apikeys:list'], <ApiKeysPage />)

    await waitFor(() => {
      const keyRequests = fetchMock.mock.calls.filter(([input]) =>
        String(input).startsWith('/api/v1/auth/keys')
      )
      expect(keyRequests.length).toBeGreaterThan(0)
    })
    expect(screen.queryByRole('button', { name: /create key/i })).toBeNull()
    expect(screen.queryByText(/apikeys:list or apikeys:read is required/i)).toBeNull()
  })

  it('shows create for a caller holding apikeys:create', async () => {
    stubFetch()
    renderWith(['settings:read', 'apikeys:list', 'apikeys:create'], <ApiKeysPage />)

    expect(await screen.findByRole('button', { name: /create key/i })).toBeTruthy()
  })
})

describe('IdentityPage tab gating', () => {
  it('shows only the Users tab to a caller holding users:list', async () => {
    stubFetch()
    renderWith(['users:list'], <IdentityPage />)

    expect(await screen.findByRole('button', { name: 'Users' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Providers' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'RBAC' })).toBeNull()
  })

  it('shows provider and RBAC tabs to a caller holding settings:read', async () => {
    stubFetch()
    renderWith(['settings:read'], <IdentityPage />)

    expect(await screen.findByRole('button', { name: 'Providers' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'RBAC' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Users' })).toBeNull()
  })

  it('hides provider mutation controls without settings:write', async () => {
    stubFetch()
    renderWith(['settings:read'], <IdentityPage />)

    await screen.findByRole('button', { name: 'Providers' })
    expect(screen.queryByRole('button', { name: /add provider/i })).toBeNull()
  })

  it('shows provider mutation controls with settings:write', async () => {
    stubFetch()
    renderWith(['settings:read', 'settings:write'], <IdentityPage />)

    expect(await screen.findByRole('button', { name: /add provider/i })).toBeTruthy()
  })
})
