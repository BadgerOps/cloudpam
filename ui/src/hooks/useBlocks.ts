import { useState, useCallback } from 'react'
import { get } from '../api/client'
import type { Block, BlocksListResponse } from '../api/types'

interface BlockFilters {
  accounts?: number[]
  pools?: number[]
  page?: number
  pageSize?: number
}

export function useBlocks() {
  const [blocks, setBlocks] = useState<Block[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetchBlocks = useCallback(async (filters?: BlockFilters) => {
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams()
      if (filters?.accounts?.length) params.set('accounts', filters.accounts.join(','))
      if (filters?.pools?.length) params.set('pools', filters.pools.join(','))
      if (filters?.page) params.set('page', String(filters.page))
      if (filters?.pageSize) params.set('page_size', String(filters.pageSize))
      const qs = params.toString()
      const data = await get<BlocksListResponse>(`/api/v1/blocks${qs ? `?${qs}` : ''}`)
      setBlocks(data.items ?? [])
      setTotal(data.total ?? 0)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to fetch blocks')
    } finally {
      setLoading(false)
    }
  }, [])

  return { blocks, total, loading, error, fetchBlocks }
}
