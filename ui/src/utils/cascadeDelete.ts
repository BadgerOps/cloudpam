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

// Ids of the given pools and every pool below them, each once. A root that
// is not in the list is still included, so a caller can drop it by id.
export function subtreeIds(pools: Pool[], rootIds: Iterable<number>): Set<number> {
  const childrenOf = new Map<number, number[]>()
  for (const p of pools) {
    if (p.parent_id == null) continue
    const siblings = childrenOf.get(p.parent_id) ?? []
    siblings.push(p.id)
    childrenOf.set(p.parent_id, siblings)
  }
  const ids = new Set<number>()
  const pending = [...rootIds]
  while (pending.length > 0) {
    const id = pending.pop()!
    if (ids.has(id)) continue
    ids.add(id)
    pending.push(...(childrenOf.get(id) ?? []))
  }
  return ids
}

// Pools an account cascade removes: those assigned to the account and every
// pool below them.
export function cascadePoolCount(pools: Pool[], accountId: number): number {
  return subtreeIds(pools, pools.filter(p => p.account_id === accountId).map(p => p.id)).size
}

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`
}
