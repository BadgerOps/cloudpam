import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAuthState } from '../hooks/useAuth'

const health = { auth_enabled: true, local_auth_enabled: true, needs_setup: false }

function mockFetch(meStatus: number, meBody?: unknown) {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url === '/healthz') {
      return { ok: true, status: 200, json: async () => health } as Response
    }
    if (url === '/api/v1/auth/me') {
      return {
        ok: meStatus >= 200 && meStatus < 300,
        status: meStatus,
        json: async () => meBody ?? {},
      } as Response
    }
    throw new Error(`unexpected fetch: ${url}`)
  })
}

describe('useAuthState does not trust persisted authorization state', () => {
  beforeEach(() => {
    window.localStorage.clear()
    vi.restoreAllMocks()
  })

  afterEach(() => {
    window.localStorage.clear()
  })

  it('ignores a role forged in localStorage', async () => {
    window.localStorage.setItem('cloudpam_role', 'admin')
    window.localStorage.setItem('cloudpam_permissions', JSON.stringify(['settings:read', 'users:list']))
    vi.stubGlobal('fetch', mockFetch(401))

    const { result } = renderHook(() => useAuthState())

    // Before the server responds, nothing is granted.
    expect(result.current.role).toBeNull()
    expect(result.current.permissions).toEqual([])
    expect(result.current.hasPermission('settings:read')).toBe(false)

    await waitFor(() => {
      expect(result.current.authChecked).toBe(true)
    })

    expect(result.current.isAuthenticated).toBe(false)
    expect(result.current.role).toBeNull()
    expect(result.current.hasPermission('settings:read')).toBe(false)
  })

  it('clears legacy role and permission keys on an unauthenticated check', async () => {
    window.localStorage.setItem('cloudpam_role', 'admin')
    window.localStorage.setItem('cloudpam_permissions', JSON.stringify(['settings:read']))
    vi.stubGlobal('fetch', mockFetch(401))

    const { result } = renderHook(() => useAuthState())
    await waitFor(() => {
      expect(result.current.authChecked).toBe(true)
    })

    expect(window.localStorage.getItem('cloudpam_role')).toBeNull()
    expect(window.localStorage.getItem('cloudpam_permissions')).toBeNull()
  })

  it('uses only the server response for role and permissions', async () => {
    window.localStorage.setItem('cloudpam_role', 'admin')
    vi.stubGlobal(
      'fetch',
      mockFetch(200, {
        role: 'viewer',
        auth_type: 'session',
        permissions: ['pools:read'],
        user: { username: 'alice', display_name: 'Alice', role: 'viewer' },
      }),
    )

    const { result } = renderHook(() => useAuthState())
    await waitFor(() => {
      expect(result.current.isAuthenticated).toBe(true)
    })

    expect(result.current.role).toBe('viewer')
    expect(result.current.permissions).toEqual(['pools:read'])
    expect(result.current.hasPermission('pools:read')).toBe(true)
    expect(result.current.hasPermission('settings:read')).toBe(false)
  })

  it('does not persist role or permissions after a server-validated session', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch(200, {
        role: 'admin',
        auth_type: 'session',
        permissions: ['settings:read'],
        user: { username: 'root', display_name: 'Root', role: 'admin' },
      }),
    )

    const { result } = renderHook(() => useAuthState())
    await waitFor(() => {
      expect(result.current.isAuthenticated).toBe(true)
    })

    expect(window.localStorage.getItem('cloudpam_role')).toBeNull()
    expect(window.localStorage.getItem('cloudpam_permissions')).toBeNull()
    // Display-only metadata is still persisted.
    expect(window.localStorage.getItem('cloudpam_key_name')).toBe('Root')
  })

  it('drops role and permissions on a forced logout event', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch(200, {
        role: 'admin',
        auth_type: 'session',
        permissions: ['settings:read'],
        user: { username: 'root', display_name: 'Root', role: 'admin' },
      }),
    )

    const { result } = renderHook(() => useAuthState())
    await waitFor(() => {
      expect(result.current.hasPermission('settings:read')).toBe(true)
    })

    act(() => {
      window.dispatchEvent(new Event('auth:logout'))
    })

    expect(result.current.role).toBeNull()
    expect(result.current.permissions).toEqual([])
    expect(result.current.hasPermission('settings:read')).toBe(false)
  })
})
