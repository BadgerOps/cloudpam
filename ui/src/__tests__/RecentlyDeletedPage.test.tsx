import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RecentlyDeletedPage from '../pages/RecentlyDeletedPage'
import { ApiRequestError } from '../api/client'
import type { Account, Pool } from '../api/types'

const mockGet = vi.hoisted(() => vi.fn())
const mockPost = vi.hoisted(() => vi.fn())
const showToast = vi.hoisted(() => vi.fn())
const permissions = vi.hoisted(() => ({ granted: [] as string[] }))

vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client')
  return { ...actual, get: mockGet, post: mockPost }
})
vi.mock('../hooks/useToast', () => ({ useToast: () => ({ showToast }) }))
vi.mock('../hooks/useAuth', () => ({
  useAuth: () => ({ hasPermission: (p: string) => permissions.granted.includes(p) }),
}))

const pool = (id: number, name: string, cidr: string, parent_id?: number): Pool => ({
  id, name, cidr, parent_id, type: 'subnet', status: 'active', source: 'manual',
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z', deleted_at: '2026-11-01T00:00:00Z',
})

const parent = pool(1, 'prod-vpc', '10.0.0.0/16')
const child = pool(2, 'prod-app', '10.0.1.0/24', 1)
const account: Account = { id: 7, key: 'aws:111111111111', name: 'prod-account', created_at: '2026-01-01T00:00:00Z', deleted_at: '2026-11-02T00:00:00Z' }

beforeEach(() => {
  vi.clearAllMocks()
  permissions.granted = ['pools:delete', 'accounts:delete']
  mockGet.mockImplementation(async (path: string) => {
    if (path === '/api/v1/pools/deleted') return [child, parent]
    if (path === '/api/v1/accounts/deleted') return [account]
    throw new Error(`unexpected GET ${path}`)
  })
})

describe('RecentlyDeletedPage', () => {
  it('restores a parent pool, which unblocks its deleted child', async () => {
    mockPost.mockResolvedValue({ ...parent, deleted_at: undefined })
    render(<RecentlyDeletedPage />)

    const childButton = await screen.findByRole<HTMLButtonElement>('button', { name: 'Restore pool prod-app' })
    expect(childButton.disabled).toBe(true)
    expect(childButton.title).toBe('Restore prod-vpc first')
    screen.getByText('inside deleted pool prod-vpc')

    fireEvent.click(screen.getByRole('button', { name: 'Restore pool prod-vpc' }))

    await waitFor(() => expect(screen.queryByRole('button', { name: 'Restore pool prod-vpc' })).toBeNull())
    expect(mockPost).toHaveBeenCalledWith('/api/v1/pools/1/restore', {})
    expect(showToast).toHaveBeenCalledWith('Restored prod-vpc', 'success')
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Restore pool prod-app' }).disabled).toBe(false)
  })

  it('keeps the row and shows the server reason when a restore conflicts', async () => {
    mockPost.mockRejectedValue(new ApiRequestError(409, { error: 'account key "aws:111111111111" already exists: conflict' }))
    render(<RecentlyDeletedPage />)

    fireEvent.click(await screen.findByRole('button', { name: 'Restore account prod-account' }))

    await waitFor(() => expect(showToast).toHaveBeenCalledWith('account key "aws:111111111111" already exists: conflict', 'error'))
    expect(mockPost).toHaveBeenCalledWith('/api/v1/accounts/7/restore', {})
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Restore account prod-account' }).disabled).toBe(false)
  })

  it('shows and loads only the sections the user may restore', async () => {
    permissions.granted = ['accounts:delete']
    render(<RecentlyDeletedPage />)

    await screen.findByRole('button', { name: 'Restore account prod-account' })
    expect(screen.queryByRole('heading', { name: 'Pools' })).toBeNull()
    expect(mockGet).not.toHaveBeenCalledWith('/api/v1/pools/deleted')
  })
})
