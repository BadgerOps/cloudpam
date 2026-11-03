import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { usePagedFilters } from '../hooks/usePagedFilters'

describe('usePagedFilters', () => {
  it('returns to page 1 when a filter changes and keeps the other filters', () => {
    const { result } = renderHook(() => usePagedFilters({ type: '', status: 'open' }))

    act(() => result.current.setPage(3))
    expect(result.current.query.page).toBe(3)

    act(() => result.current.setFilters({ type: 'unmanaged' }))
    expect(result.current.query.page).toBe(1)
    expect(result.current.filters).toEqual({ type: 'unmanaged', status: 'open' })
  })

  it('keeps the filters when the page changes', () => {
    const { result } = renderHook(() => usePagedFilters({ status: 'pending' }))

    act(() => result.current.setPage(2))
    expect(result.current.filters).toEqual({ status: 'pending' })
    expect(result.current.query.page).toBe(2)
  })

  it('issues a new query when the same page is requested again', () => {
    const { result } = renderHook(() => usePagedFilters({ status: '' }))

    act(() => result.current.setPage(2))
    const first = result.current.query
    act(() => result.current.setPage(2))
    expect(result.current.query).not.toBe(first)
    expect(result.current.query).toEqual(first)
  })
})
