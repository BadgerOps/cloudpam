import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../App'
import type { AuthContextValue } from '../hooks/useAuth'

const mockUseAuthState = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useAuth', async () => {
  const actual = await vi.importActual<typeof import('../hooks/useAuth')>('../hooks/useAuth')
  return {
    ...actual,
    useAuthState: () => mockUseAuthState(),
  }
})

vi.mock('../components/Layout', async () => {
  const { Outlet } = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    default: () => <Outlet />,
  }
})

vi.mock('../pages/LoginPage', () => ({ default: () => <div>Login page</div> }))
vi.mock('../pages/SetupPage', () => ({ default: () => <div>Setup page</div> }))
vi.mock('../pages/DashboardPage', () => ({ default: () => <div>Dashboard page</div> }))
vi.mock('../pages/PoolsPage', () => ({ default: () => <div>Pools page</div> }))
vi.mock('../pages/BlocksPage', () => ({ default: () => <div>Blocks page</div> }))
vi.mock('../pages/AccountsPage', () => ({ default: () => <div>Accounts page</div> }))
vi.mock('../pages/AuditPage', () => ({ default: () => <div>Audit page</div> }))
vi.mock('../pages/DiscoveryPage', () => ({ default: () => <div>Discovery page</div> }))
vi.mock('../pages/SchemaPage', () => ({ default: () => <div>Schema page</div> }))
vi.mock('../pages/ApiKeysPage', () => ({ default: () => <div>API keys page</div> }))
vi.mock('../pages/UsersPage', () => ({ default: () => <div>Users page</div> }))
vi.mock('../pages/RecommendationsPage', () => ({ default: () => <div>Recommendations page</div> }))
vi.mock('../pages/DriftPage', () => ({ default: () => <div>Drift page</div> }))
vi.mock('../pages/AIPlannerPage', () => ({ default: () => <div>AI planner page</div> }))
vi.mock('../pages/ProfilePage', () => ({ default: () => <div>Profile page</div> }))
vi.mock('../pages/LogDestinationsPage', () => ({ default: () => <div>Log destinations page</div> }))
vi.mock('../pages/SecuritySettingsPage', () => ({ default: () => <div>Security settings page</div> }))
vi.mock('../pages/UpdatesPage', () => ({ default: () => <div>Updates page</div> }))
vi.mock('../pages/ChangelogPage', () => ({ default: () => <div>Changelog page</div> }))
vi.mock('../pages/IdentityPage', () => ({ default: () => <div>Identity page</div> }))
vi.mock('../pages/ConfigurationPage', () => ({ default: () => <div>Configuration page</div> }))

function authState(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  const permissions = overrides.permissions ?? []
  const role = overrides.role ?? 'viewer'

  return {
    keyName: null,
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
    ...overrides,
  }
}

describe('App routes', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.history.pushState({}, '', '/')
  })

  it('blocks direct API key management navigation without settings read permission', async () => {
    mockUseAuthState.mockReturnValue(authState({ permissions: [] }))
    window.history.pushState({}, '', '/config/api-keys')

    render(<App />)

    await waitFor(() => {
      expect(screen.getByText('Dashboard page')).toBeTruthy()
    })
    expect(screen.queryByText('API keys page')).toBeNull()
  })

  it('allows direct API key management navigation with settings read permission', async () => {
    mockUseAuthState.mockReturnValue(authState({ permissions: ['settings:read'] }))
    window.history.pushState({}, '', '/config/api-keys')

    render(<App />)

    expect(await screen.findByText('API keys page')).toBeTruthy()
  })
})
