import { useCallback, useState } from 'react'

interface PagedQuery<F> {
  filters: F
  page: number
}

// Filters and page live in one state so a filter change always lands on
// page 1; a stale page number with new filters cannot be represented.
export function usePagedFilters<F extends object>(initial: F) {
  const [query, setQuery] = useState<PagedQuery<F>>({ filters: initial, page: 1 })

  const setFilters = useCallback((patch: Partial<F>) => {
    setQuery(q => ({ filters: { ...q.filters, ...patch }, page: 1 }))
  }, [])

  const setPage = useCallback((page: number) => {
    setQuery(q => (q.page === page ? q : { ...q, page }))
  }, [])

  return { filters: query.filters, page: query.page, setFilters, setPage }
}
