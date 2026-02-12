import { useState, useCallback } from 'react'
import { get, post, del } from '../api/client'
import type { Account, CreateAccountRequest } from '../api/types'

export function useAccounts() {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetchAccounts = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await get<Account[]>('/api/v1/accounts')
      setAccounts(data)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to fetch accounts')
    } finally {
      setLoading(false)
    }
  }, [])

  const createAccount = useCallback(async (data: CreateAccountRequest) => {
    const account = await post<Account>('/api/v1/accounts', data)
    setAccounts(prev => [...prev, account])
    return account
  }, [])

  const deleteAccount = useCallback(async (id: number) => {
    await del(`/api/v1/accounts/${id}`)
    setAccounts(prev => prev.filter(a => a.id !== id))
  }, [])

  return { accounts, loading, error, fetchAccounts, createAccount, deleteAccount }
}
