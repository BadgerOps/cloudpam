import { useState, useCallback } from 'react'
import { get } from '../api/client'
import type { AuditEvent, AuditListResponse } from '../api/types'

const PAGE_SIZE = 25

export function useAudit() {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetchEvents = useCallback(async (
    pageOffset = 0,
    limit = PAGE_SIZE,
    action?: string,
    resourceType?: string,
  ) => {
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

  const nextPage = useCallback(() => {
    if (offset + PAGE_SIZE < total) {
      fetchEvents(offset + PAGE_SIZE)
    }
  }, [offset, total, fetchEvents])

  const prevPage = useCallback(() => {
    if (offset > 0) {
      fetchEvents(Math.max(0, offset - PAGE_SIZE))
    }
  }, [offset, fetchEvents])

  return { events, total, offset, loading, error, fetchEvents, nextPage, prevPage, pageSize: PAGE_SIZE }
}
