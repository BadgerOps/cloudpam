import { useCallback, useState } from 'react'

interface PagedQuery<F> {
  filters: F
  page: number
}

// Filters and page live in one state so a filter change always lands on
// page 1; a stale page number with new filters cannot be represented.
// The query is only the request. Lists label their rows with the page the
// response carried, and setPage always yields a new query so a page whose
// request failed can be requested again.
export function usePagedFilters<F extends object>(initial: F) {
  const [query, setQuery] = useState<PagedQuery<F>>({ filters: initial, page: 1 })

  const setFilters = useCallback((patch: Partial<F>) => {
    setQuery(q => ({ filters: { ...q.filters, ...patch }, page: 1 }))
  }, [])

  const setPage = useCallback((page: number) => {
    setQuery(q => ({ ...q, page }))
  }, [])

  return { query, filters: query.filters, setFilters, setPage }
}
