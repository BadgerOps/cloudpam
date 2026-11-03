import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ApiKeysPage from '../pages/ApiKeysPage'
import type { ApiKeyInfo } from '../api/types'
import { getStatusBadgeClass } from '../utils/format'

const mockUseApiKeys = vi.fn()

vi.mock('../hooks/useApiKeys', () => ({
  useApiKeys: () => mockUseApiKeys(),
}))

vi.mock('../hooks/useSettings', () => ({
  useSecuritySettings: () => ({ settings: null }),
}))

function key(id: string, fields: Partial<ApiKeyInfo>): ApiKeyInfo {
  return { id, prefix: `cp_${id}`, name: id, scopes: [], created_at: '2026-01-01T00:00:00Z', revoked: false, ...fields }
}

describe('ApiKeysPage status pills', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseApiKeys.mockReturnValue({
      keys: [
        key('revoked', { revoked: true, expiry_status: 'revoked' }),
        key('expired', { expiry_status: 'expired', expires_in_days: 0 }),
        key('expiring', { expiry_status: 'expiring', expires_in_days: 3 }),
        key('forever', { expiry_status: 'no_expiry' }),
        key('active', { expiry_status: 'active', expires_in_days: 90 }),
      ],
      loading: false,
      error: null,
      canList: true,
      canCreate: false,
      canRevoke: false,
      create: vi.fn(),
      revoke: vi.fn(),
    })
  })

  it('colours each key state from the shared status vocabulary', () => {
    render(<ApiKeysPage />)

    expect(screen.getByText('Revoked').className).toContain(getStatusBadgeClass('revoked'))
    expect(screen.getByText('Expired').className).toContain(getStatusBadgeClass('expired'))
    expect(screen.getByText('Expiring 3d').className).toContain(getStatusBadgeClass('expiring'))
    expect(screen.getByText('No expiry').className).toContain(getStatusBadgeClass('no_expiry'))
    expect(screen.getByText('Active').className).toContain(getStatusBadgeClass('active'))
  })

  it('shows a revoked key as revoked even when the server reports it as expiring', () => {
    mockUseApiKeys.mockReturnValue({
      ...mockUseApiKeys(),
      keys: [key('stale', { revoked: true, expiry_status: 'expiring', expires_in_days: 2 })],
    })

    render(<ApiKeysPage />)

    expect(screen.getByText('Revoked')).toBeTruthy()
    expect(screen.queryByText(/Expiring/)).toBeNull()
  })
})
