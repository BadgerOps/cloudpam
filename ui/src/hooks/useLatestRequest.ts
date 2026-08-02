import { useCallback, useRef } from 'react'

/**
 * Guards state updates against out-of-order async responses.
 *
 * Overlapping requests (fast typing, quick filter changes, context switches)
 * can resolve in any order, so a slower earlier response may land last and
 * clobber the newer result. Call `begin()` when a request starts and check
 * `isCurrent(token)` before committing its result:
 *
 *   const token = begin()
 *   const resp = await get(...)
 *   if (!isCurrent(token)) return
 *   setData(resp)
 *
 * Mutations that write the same state should also call `begin()` right before
 * committing, so an in-flight load cannot overwrite the newer local value.
 *
 * Use one instance per piece of state; sharing an instance across unrelated
 * state would invalidate requests that never competed with each other.
 */
export function useLatestRequest() {
  const seqRef = useRef(0)

  /** Starts a new request and invalidates every earlier one. */
  const begin = useCallback(() => {
    seqRef.current += 1
    return seqRef.current
  }, [])

  /** Reports whether `token` still belongs to the most recent request. */
  const isCurrent = useCallback((token: number) => seqRef.current === token, [])

  return { begin, isCurrent }
}
