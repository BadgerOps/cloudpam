import { fireEvent, render, screen, within } from '@testing-library/react'
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

vi.mock('../hooks/useSessionRefresh', () => ({ useSessionRefresh: () => {} }))
vi.mock('../components/UpdateBanner', () => ({ default: () => null }))
vi.mock('../components/ToastContainer', () => ({ default: () => null }))
vi.mock('../components/SearchModal', () => ({ default: () => null }))
vi.mock('../components/ImportExportModal', () => ({ default: () => null }))

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

const VIEWER = ['pools:read', 'pools:list', 'accounts:read', 'accounts:list', 'discovery:read', 'discovery:list']

const PROFILES: Array<{ name: string; role: string; permissions: string[] }> = [
  { name: 'no permissions', role: 'viewer', permissions: [] },
  { name: 'built-in viewer', role: 'viewer', permissions: VIEWER },
  { name: 'built-in auditor', role: 'auditor', permissions: ['audit:read', 'audit:list'] },
  { name: 'audit:list only', role: 'custom', permissions: ['audit:list'] },
  { name: 'settings:read only', role: 'custom', permissions: ['settings:read'] },
  { name: 'users:list only', role: 'custom', permissions: ['users:list'] },
  { name: 'apikeys:list only', role: 'custom', permissions: ['apikeys:list'] },
  { name: 'admin', role: 'admin', permissions: [] },
]

function authState(role: string, permissions: string[]): AuthContextValue {
  return {
    keyName: null,
    role,
    authType: 'session',
    currentUser: { id: 'u1', username: 'tester', role, is_active: true, created_at: '', failed_login_attempts: 0 },
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

function renderAt(path: string, role: string, permissions: string[]) {
  mockUseAuthState.mockReturnValue(authState(role, permissions))
  window.history.pushState({}, '', path)
  return render(<App />)
}

function sidebarLinks(role: string, permissions: string[]): Set<string> {
  const { unmount } = renderAt('/', role, permissions)
  const nav = screen.getByRole('navigation', { name: 'Main navigation' })
  const hrefs = within(nav).getAllByRole('link').map(link => link.getAttribute('href') ?? '')
  unmount()
  return new Set(hrefs)
}

function headerApiKeysVisible(role: string, permissions: string[]): boolean {
  const { unmount } = renderAt('/', role, permissions)
  fireEvent.click(screen.getByRole('button', { name: 'tester' }))
  const visible = screen.queryByRole('button', { name: /my api keys/i }) !== null
  unmount()
  return visible
}

function canOpen(path: string, role: string, permissions: string[]): boolean {
  const { unmount } = renderAt(path, role, permissions)
  const opened = window.location.pathname === path
  unmount()
  return opened
}

describe('sidebar and routes agree on permissions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  const allLinks = () => sidebarLinks('admin', [])

  it('admin sees a sidebar link for every navigable page and can open each', () => {
    const links = [...allLinks()]
    expect(links.length).toBeGreaterThan(10)
    expect(links.filter(href => !canOpen(href, 'admin', []))).toEqual([])
  })

  it.each(PROFILES)('$name: a sidebar link renders exactly when its route opens', ({ role, permissions }) => {
    const visible = sidebarLinks(role, permissions)
    const mismatches = [...allLinks()]
      .map(href => ({ href, visible: visible.has(href), opens: canOpen(href, role, permissions) }))
      .filter(row => row.visible !== row.opens)
    expect(mismatches).toEqual([])
  })

  it.each(PROFILES)('$name: the header API keys shortcut renders exactly when its route opens', ({ role, permissions }) => {
    expect(headerApiKeysVisible(role, permissions)).toBe(canOpen('/config/api-keys', role, permissions))
  })
})
