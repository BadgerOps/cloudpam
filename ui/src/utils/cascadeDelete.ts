import { ApiRequestError } from '../api/client'
import type { Pool } from '../api/types'

function isConflict(err: unknown): boolean {
  return err instanceof ApiRequestError && err.status === 409
}

// The API answers a plain delete with 409 while the target still owns pools
// and only removes them when asked again with force. Resolves false when the
// user declines the cascade; any other failure propagates to the caller.
export async function deleteWithCascade(
  del: (force: boolean) => Promise<void>,
  describeCascade: () => Promise<string>,
): Promise<boolean> {
  try {
    await del(false)
    return true
  } catch (err) {
    if (!isConflict(err)) throw err
  }
  if (!window.confirm(await describeCascade())) return false
  await del(true)
  return true
}

// Pools an account cascade removes: those assigned to the account and every
// pool below them, counted once.
export function cascadePoolCount(pools: Pool[], accountId: number): number {
  const childrenOf = new Map<number, Pool[]>()
  for (const p of pools) {
    if (p.parent_id == null) continue
    const siblings = childrenOf.get(p.parent_id) ?? []
    siblings.push(p)
    childrenOf.set(p.parent_id, siblings)
  }
  const removed = new Set<number>()
  const pending = pools.filter(p => p.account_id === accountId)
  while (pending.length > 0) {
    const p = pending.pop()!
    if (removed.has(p.id)) continue
    removed.add(p.id)
    pending.push(...(childrenOf.get(p.id) ?? []))
  }
  return removed.size
}

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`
}
