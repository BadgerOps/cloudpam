import { useState, useEffect, useRef, useCallback } from 'react'
import { get } from '../api/client'
import type { SearchResponse } from '../api/types'

const DEBOUNCE_MS = 300

// Detect if input looks like a CIDR or IP address
function isCIDROrIP(s: string): boolean {
  return /^\d{1,3}\.\d{1,3}(\.\d{1,3}(\.\d{1,3})?)?(\/\d{1,2})?$/.test(s.trim())
}

export function useSearch() {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const search = useCallback(async (q: string) => {
    if (!q.trim()) {
      setResults(null)
      setLoading(false)
      return
    }

    // Cancel any in-flight request
    abortRef.current?.abort()
    abortRef.current = new AbortController()

    setLoading(true)
    setError(null)

    try {
      const params = new URLSearchParams()

      if (isCIDROrIP(q.trim())) {
        // Auto-detect CIDR/IP and use cidr_contains for containment search
        params.set('cidr_contains', q.trim())
      } else {
        params.set('q', q.trim())
      }
      params.set('page_size', '20')

      const resp = await get<SearchResponse>(`/api/v1/search?${params}`)
      setResults(resp)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : 'Search failed')
      setResults(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (timerRef.current) clearTimeout(timerRef.current)

    if (!query.trim()) {
      setResults(null)
      setLoading(false)
      return
    }

    setLoading(true)
    timerRef.current = setTimeout(() => search(query), DEBOUNCE_MS)

    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [query, search])

  return { query, setQuery, results, loading, error }
}
