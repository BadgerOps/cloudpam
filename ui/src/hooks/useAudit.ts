import { useState, useCallback, useRef } from 'react'
import { get } from '../api/client'
import type { AuditEvent, AuditListResponse } from '../api/types'

const PAGE_SIZE = 25

interface AuditQuery {
  limit: number
  action?: string
  resourceType?: string
}

export function useAudit() {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Paging used to re-request page N with no filters at all, so stepping
  // through a filtered log silently fell back to the unfiltered result set.
  // The last query is remembered here and reused by nextPage/prevPage.
  const lastQuery = useRef<AuditQuery>({ limit: PAGE_SIZE })

  const fetchEvents = useCallback(async (
    pageOffset = 0,
    limit = PAGE_SIZE,
    action?: string,
    resourceType?: string,
  ) => {
    lastQuery.current = { limit, action, resourceType }
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams()
      params.set('limit', String(limit))
      params.set('offset', String(pageOffset))
      if (action) params.set('action', action)
      if (resourceType) params.set('resource_type', resourceType)
      const data = await get<AuditListResponse>(`/api/v1/audit?${params}`)
      setEvents(data.events ?? [])
      setTotal(data.total)
      setOffset(pageOffset)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to fetch audit events')
    } finally {
      setLoading(false)
    }
  }, [])

  const goToOffset = useCallback((pageOffset: number) => {
    const { limit, action, resourceType } = lastQuery.current
    return fetchEvents(pageOffset, limit, action, resourceType)
  }, [fetchEvents])

  const nextPage = useCallback(() => {
    if (offset + lastQuery.current.limit < total) {
      goToOffset(offset + lastQuery.current.limit)
    }
  }, [offset, total, goToOffset])

  const prevPage = useCallback(() => {
    if (offset > 0) {
      goToOffset(Math.max(0, offset - lastQuery.current.limit))
    }
  }, [offset, goToOffset])

  return { events, total, offset, loading, error, fetchEvents, nextPage, prevPage, pageSize: PAGE_SIZE }
}
