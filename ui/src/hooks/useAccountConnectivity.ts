import { useCallback, useSyncExternalStore } from 'react'
import { post } from '../api/client'
import type { ConnectivityTestResult } from '../api/types'

export type ConnectivityState =
  | { phase: 'untested' }
  | { phase: 'testing'; previous?: ConnectivityTestResult }
  | { phase: 'tested'; result: ConnectivityTestResult }
  | { phase: 'error'; message: string }

const UNTESTED: ConnectivityState = { phase: 'untested' }

// One store for the whole SPA session, so the Accounts page and the discovery
// wizard show the same last result for an account.
const states = new Map<number, ConnectivityState>()
const listeners = new Set<() => void>()

function setState(accountId: number, state: ConnectivityState) {
  states.set(accountId, state)
  listeners.forEach(l => l())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function resetAccountConnectivity() {
  states.clear()
  listeners.forEach(l => l())
}

export async function testAccountConnectivity(accountId: number): Promise<void> {
  const current = states.get(accountId)
  if (current?.phase === 'testing') return
  setState(accountId, { phase: 'testing', previous: current?.phase === 'tested' ? current.result : undefined })
  try {
    const result = await post<ConnectivityTestResult>(`/api/v1/accounts/${accountId}/test`, {})
    setState(accountId, { phase: 'tested', result })
  } catch (e) {
    setState(accountId, { phase: 'error', message: e instanceof Error ? e.message : 'connectivity test failed' })
  }
}

export function useAccountConnectivity(accountId: number) {
  const state = useSyncExternalStore(subscribe, () => states.get(accountId) ?? UNTESTED)
  const test = useCallback(() => testAccountConnectivity(accountId), [accountId])
  return { state, test }
}
