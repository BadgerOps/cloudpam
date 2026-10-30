import { useState, useCallback } from 'react'
import { get, post, patch, del } from '../api/client'
import { useLatestRequest } from './useLatestRequest'
import type { Account, CreateAccountRequest, UpdateAccountRequest } from '../api/types'

export function useAccounts() {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { begin, isCurrent } = useLatestRequest()

  const fetchAccounts = useCallback(async () => {
    const token = begin()
    setLoading(true)
    setError(null)
    try {
      const data = await get<Account[]>('/api/v1/accounts')
      if (!isCurrent(token)) return
      setAccounts(data ?? [])
    } catch (e) {
      if (!isCurrent(token)) return
      setError(e instanceof Error ? e.message : 'Failed to fetch accounts')
    } finally {
      setLoading(false)
    }
  }, [begin, isCurrent])

  const createAccount = useCallback(async (data: CreateAccountRequest) => {
    const account = await post<Account>('/api/v1/accounts', data)
    // This write is newer than any list load still in flight
    begin()
    setAccounts(prev => [...prev, account])
    return account
  }, [begin])

  const updateAccount = useCallback(async (id: number, data: UpdateAccountRequest) => {
    const account = await patch<Account>(`/api/v1/accounts/${id}`, data)
    begin()
    setAccounts(prev => prev.map(a => (a.id === id ? account : a)))
    return account
  }, [begin])

  const deleteAccount = useCallback(async (id: number) => {
    await del(`/api/v1/accounts/${id}`)
    begin()
    setAccounts(prev => prev.filter(a => a.id !== id))
  }, [begin])

  return { accounts, loading, error, fetchAccounts, createAccount, updateAccount, deleteAccount }
}
