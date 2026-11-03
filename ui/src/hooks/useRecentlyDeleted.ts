import { useState, useCallback } from 'react'
import { get, post } from '../api/client'
import { useLatestRequest } from './useLatestRequest'
import type { Account, Pool } from '../api/types'

export function useRecentlyDeleted() {
  const [pools, setPools] = useState<Pool[]>([])
  const [accounts, setAccounts] = useState<Account[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { begin, isCurrent } = useLatestRequest()

  const fetchDeleted = useCallback(async (which: { pools: boolean; accounts: boolean }) => {
    const token = begin()
    setLoading(true)
    setError(null)
    try {
      const [p, a] = await Promise.all([
        which.pools ? get<Pool[]>('/api/v1/pools/deleted') : Promise.resolve([]),
        which.accounts ? get<Account[]>('/api/v1/accounts/deleted') : Promise.resolve([]),
      ])
      if (!isCurrent(token)) return
      setPools(p ?? [])
      setAccounts(a ?? [])
    } catch (e) {
      if (!isCurrent(token)) return
      setError(e instanceof Error ? e.message : 'Failed to load deleted records')
    } finally {
      setLoading(false)
    }
  }, [begin, isCurrent])

  const restorePool = useCallback(async (id: number) => {
    const pool = await post<Pool>(`/api/v1/pools/${id}/restore`, {})
    begin()
    setPools(prev => prev.filter(p => p.id !== id))
    return pool
  }, [begin])

  const restoreAccount = useCallback(async (id: number) => {
    const account = await post<Account>(`/api/v1/accounts/${id}/restore`, {})
    begin()
    setAccounts(prev => prev.filter(a => a.id !== id))
    return account
  }, [begin])

  return { pools, accounts, loading, error, fetchDeleted, restorePool, restoreAccount }
}
