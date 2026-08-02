import { useCallback, useRef, useState } from 'react'

/**
 * Gates an async action so it cannot be fired again while it is still running.
 *
 * `pending` drives the button's disabled state; the ref guard additionally
 * blocks a second click that lands before React re-renders with the new state.
 * The flag is always cleared in a `finally`, so a failed action re-enables the
 * control instead of leaving it stuck.
 */
export function usePendingAction<A extends unknown[], R>(
  action: (...args: A) => Promise<R>,
): { pending: boolean; run: (...args: A) => Promise<R | undefined> } {
  const [pending, setPending] = useState(false)
  const inFlightRef = useRef(false)

  const run = useCallback(
    async (...args: A): Promise<R | undefined> => {
      if (inFlightRef.current) return undefined
      inFlightRef.current = true
      setPending(true)
      try {
        return await action(...args)
      } finally {
        inFlightRef.current = false
        setPending(false)
      }
    },
    [action],
  )

  return { pending, run }
}
