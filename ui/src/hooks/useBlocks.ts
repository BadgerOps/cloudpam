import { useState, useCallback } from 'react'
import { get } from '../api/client'
import { useLatestRequest } from './useLatestRequest'
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
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { begin, isCurrent } = useLatestRequest()

  const fetchBlocks = useCallback(async (filters?: BlockFilters) => {
    const token = begin()
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
      if (!isCurrent(token)) return
      setBlocks(data.items ?? [])
      setTotal(data.total ?? 0)
      setPage(data.page || 1)
      if (data.page_size > 0) setPageSize(data.page_size)
    } catch (e) {
      if (!isCurrent(token)) return
      setError(e instanceof Error ? e.message : 'Failed to fetch blocks')
    } finally {
      setLoading(false)
    }
  }, [begin, isCurrent])

  return { blocks, total, page, pageSize, loading, error, fetchBlocks }
}
