import { useState, useCallback } from 'react'
import { get } from '../api/client'
import { useLatestRequest } from './useLatestRequest'
import type { AuditEvent, AuditListResponse } from '../api/types'

const PAGE_SIZE = 25

export function useAudit() {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { begin, isCurrent } = useLatestRequest()

  const fetchEvents = useCallback(async (
    offset = 0,
    limit = PAGE_SIZE,
    action?: string,
    resourceType?: string,
  ) => {
    const token = begin()
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams()
      params.set('limit', String(limit))
      params.set('offset', String(offset))
      if (action) params.set('action', action)
      if (resourceType) params.set('resource_type', resourceType)
      const data = await get<AuditListResponse>(`/api/v1/audit?${params}`)
      if (!isCurrent(token)) return
      setEvents(data.events ?? [])
      setTotal(data.total)
      setPage(Math.floor(offset / limit) + 1)
    } catch (e) {
      if (!isCurrent(token)) return
      setError(e instanceof Error ? e.message : 'Failed to fetch audit events')
    } finally {
      setLoading(false)
    }
  }, [begin, isCurrent])

  return { events, total, page, loading, error, fetchEvents, pageSize: PAGE_SIZE }
}
