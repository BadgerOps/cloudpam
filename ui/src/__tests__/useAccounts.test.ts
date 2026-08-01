import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAccounts } from '../hooks/useAccounts'
import { get, post } from '../api/client'
import type { Account } from '../api/types'

vi.mock('../api/client', () => ({
  get: vi.fn(),
  post: vi.fn(),
  del: vi.fn(),
}))

const mockGet = vi.mocked(get)
const mockPost = vi.mocked(post)

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => {
    resolve = res
  })
  return { promise, resolve }
}

function account(id: number, name: string): Account {
  return { id, key: `key-${id}`, name, created_at: '2026-01-01T00:00:00Z' }
}

describe('useAccounts', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('does not let a slow initial fetch drop a newly created account', async () => {
    const list = deferred<Account[]>()
    mockGet.mockReturnValueOnce(list.promise as Promise<never>)
    mockPost.mockResolvedValue(account(2, 'New Account'))

    const { result } = renderHook(() => useAccounts())

    act(() => {
      void result.current.fetchAccounts()
    })
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(1))

    await act(async () => {
      await result.current.createAccount({ key: 'key-2', name: 'New Account' })
    })

    // The initial list request finally lands, without the new account in it
    await act(async () => {
      list.resolve([account(1, 'Existing')])
      await list.promise
    })

    expect(result.current.accounts.map(a => a.name)).toEqual(['New Account'])
    expect(result.current.loading).toBe(false)
  })
})
