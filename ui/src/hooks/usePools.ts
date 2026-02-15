import { useState, useCallback } from 'react'
import { get, post, patch, del } from '../api/client'
import type { Pool, PoolWithStats, CreatePoolRequest, UpdatePoolRequest } from '../api/types'

export function usePools() {
  const [pools, setPools] = useState<Pool[]>([])
  const [hierarchy, setHierarchy] = useState<PoolWithStats[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetchPools = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await get<Pool[]>('/api/v1/pools')
      setPools(data)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to fetch pools')
    } finally {
      setLoading(false)
    }
  }, [])

  const fetchHierarchy = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await get<{ pools: PoolWithStats[] }>('/api/v1/pools/hierarchy')
      setHierarchy(data.pools ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to fetch hierarchy')
    } finally {
      setLoading(false)
    }
  }, [])

  const getPool = useCallback(async (id: number) => {
    return get<PoolWithStats>(`/api/v1/pools/${id}`)
  }, [])

  const createPool = useCallback(async (data: CreatePoolRequest) => {
    const pool = await post<Pool>('/api/v1/pools', data)
    setPools(prev => [...prev, pool])
    return pool
  }, [])

  const updatePool = useCallback(async (id: number, data: UpdatePoolRequest) => {
    const updated = await patch<Pool>(`/api/v1/pools/${id}`, data)
    setPools(prev => prev.map(p => p.id === id ? updated : p))
    return updated
  }, [])

  const deletePool = useCallback(async (id: number, force = false) => {
    const qs = force ? '?force=true' : ''
    await del(`/api/v1/pools/${id}${qs}`)
    setPools(prev => prev.filter(p => p.id !== id))
  }, [])

  return { pools, hierarchy, loading, error, fetchPools, fetchHierarchy, getPool, createPool, updatePool, deletePool }
}
