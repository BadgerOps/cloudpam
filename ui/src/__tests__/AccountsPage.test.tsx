import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import AccountsPage from '../pages/AccountsPage'
import type { Account } from '../api/types'

const mockUseAccounts = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useAccounts', () => ({
  useAccounts: () => mockUseAccounts(),
}))

const accounts: Account[] = [
  { id: 1, key: 'aws:111111111111', name: 'prod-platform', provider: 'aws', created_at: '2026-01-01T00:00:00Z' },
  { id: 2, key: 'gcp:staging-net', name: 'staging-net', provider: 'gcp', created_at: '2026-01-01T00:00:00Z' },
]

describe('AccountsPage deep link', () => {
  it('narrows the list to the account named in ?selected', async () => {
    mockUseAccounts.mockReturnValue({
      accounts,
      loading: false,
      error: null,
      fetchAccounts: vi.fn(),
      createAccount: vi.fn(),
      deleteAccount: vi.fn(),
    })
    render(
      <MemoryRouter initialEntries={['/accounts?selected=2']}>
        <AccountsPage />
      </MemoryRouter>,
    )

    await waitFor(() => {
      expect(screen.getByRole('cell', { name: 'staging-net' })).toBeTruthy()
      expect(screen.queryByRole('cell', { name: 'prod-platform' })).toBeNull()
    })
  })
})
