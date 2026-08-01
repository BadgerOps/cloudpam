import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import UsersAdminPanel from '../components/UsersAdminPanel'
import type { UserInfo } from '../api/types'

const mockUseUsers = vi.fn()
const mockUseRoles = vi.fn()

vi.mock('../hooks/useUsers', () => ({
  useUsers: () => mockUseUsers(),
}))

vi.mock('../hooks/useRoles', () => ({
  useRoles: () => mockUseRoles(),
}))

// The panel gates its mutating controls on RBAC permissions. These cases cover
// duplicate-submit handling, not authorization, so grant everything here;
// permission gating is covered by permissionGating.test.tsx.
vi.mock('../hooks/useAuth', () => ({
  useAuth: () => ({ hasPermission: () => true }),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

const createdUser = {
  id: 'u1',
  username: 'jdoe',
  role: 'viewer',
  is_active: true,
} as UserInfo

function fillForm() {
  fireEvent.change(screen.getByPlaceholderText('jdoe'), { target: { value: 'jdoe' } })
  fireEvent.change(screen.getByPlaceholderText('Minimum 8 characters'), {
    target: { value: 'correct horse battery' },
  })
}

describe('UsersAdminPanel', () => {
  const create = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()

    mockUseRoles.mockReturnValue({ roles: [] })
    mockUseUsers.mockReturnValue({
      users: [],
      loading: false,
      error: null,
      create,
      update: vi.fn(),
      deactivate: vi.fn(),
      unlock: vi.fn(),
    })
  })

  it('does not create a duplicate user when the button is clicked twice', async () => {
    const pending = deferred<UserInfo>()
    create.mockReturnValue(pending.promise)

    render(<UsersAdminPanel />)

    fireEvent.click(screen.getByRole('button', { name: 'Create User' }))
    fillForm()

    const submit = screen.getByRole('button', { name: 'Create' })
    fireEvent.click(submit)
    fireEvent.click(submit)

    expect(create).toHaveBeenCalledTimes(1)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Creating...' })).toHaveProperty('disabled', true)
    })

    fireEvent.click(screen.getByRole('button', { name: 'Creating...' }))
    expect(create).toHaveBeenCalledTimes(1)

    await act(async () => {
      pending.resolve(createdUser)
      await pending.promise
    })

    // The form closes on success
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: /Creat(e|ing)/ })).toHaveProperty(
        'textContent',
        'Create User',
      )
    })
  })

  it('re-enables the create button after a failed create', async () => {
    create.mockRejectedValue(new Error('username already exists'))

    render(<UsersAdminPanel />)

    fireEvent.click(screen.getByRole('button', { name: 'Create User' }))
    fillForm()
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => {
      expect(screen.getByText('username already exists')).toBeTruthy()
      expect(screen.getByRole('button', { name: 'Create' })).toHaveProperty('disabled', false)
    })
    expect(create).toHaveBeenCalledTimes(1)
  })
})
