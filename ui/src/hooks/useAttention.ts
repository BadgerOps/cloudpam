import { useEffect, useMemo, useState } from 'react'
import { CloudDownload, GitCompareArrows, Lightbulb, RefreshCw, ShieldCheck } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { get, post } from '../api/client'
import type {
  Account,
  ComplianceReport,
  DriftListResponse,
  RecommendationsListResponse,
  SyncJobsResponse,
  UpdateCheckResponse,
} from '../api/types'
import {
  ITEM_LIMIT,
  summarizeCompliance,
  summarizeDrift,
  summarizeRecommendations,
  summarizeSync,
  summarizeUpdate,
} from '../utils/attention'
import type { AccountSync, AttentionSummary } from '../utils/attention'
import { useAuth } from './useAuth'

export type AttentionKey = 'drift' | 'recommendations' | 'compliance' | 'sync' | 'updates'

export interface AttentionSource {
  key: AttentionKey
  title: string
  icon: LucideIcon
  link: { label: string; to: string }
  // Mirrors the permissions the source's endpoints enforce in internal/api.
  canLoad: (hasPermission: (permission: string) => boolean) => boolean
  load: () => Promise<AttentionSummary>
}

export type AttentionLoad =
  | { state: 'loading' }
  | { state: 'ready'; summary: AttentionSummary }
  | { state: 'error'; message: string }

// Fetching every account's latest job at once trips the global rate limiter
// on large estates, so a few workers drain the account list instead.
const SYNC_CONCURRENCY = 6

async function loadAccountSyncs(): Promise<AccountSync[]> {
  const accounts = await get<Account[]>('/api/v1/accounts')
  const rows: AccountSync[] = new Array(accounts.length)
  let next = 0
  async function worker() {
    while (next < accounts.length) {
      const i = next++
      const jobs = await get<SyncJobsResponse>(`/api/v1/discovery/sync?account_id=${accounts[i].id}&limit=1`)
      rows[i] = { account: accounts[i], latest: jobs.items[0] ?? null }
    }
  }
  await Promise.all(Array.from({ length: Math.min(SYNC_CONCURRENCY, accounts.length) }, worker))
  return rows
}

export const ATTENTION_SOURCES: readonly AttentionSource[] = [
  {
    key: 'drift',
    title: 'Open drift',
    icon: GitCompareArrows,
    link: { label: 'Review drift', to: '/drift' },
    canLoad: has => has('discovery:read'),
    load: async () => {
      const [open, critical] = await Promise.all([
        get<DriftListResponse>(`/api/v1/drift?status=open&page_size=${ITEM_LIMIT}`),
        get<DriftListResponse>(`/api/v1/drift?status=open&severity=critical&page_size=${ITEM_LIMIT}`),
      ])
      return summarizeDrift(open, critical)
    },
  },
  {
    key: 'recommendations',
    title: 'Pending recommendations',
    icon: Lightbulb,
    link: { label: 'Review recommendations', to: '/recommendations' },
    canLoad: has => has('pools:read'),
    load: async () => {
      const [pending, high] = await Promise.all([
        get<RecommendationsListResponse>(`/api/v1/recommendations?status=pending&page_size=${ITEM_LIMIT}`),
        get<RecommendationsListResponse>(`/api/v1/recommendations?status=pending&priority=high&page_size=${ITEM_LIMIT}`),
      ])
      return summarizeRecommendations(pending, high)
    },
  },
  {
    key: 'compliance',
    title: 'Compliance findings',
    icon: ShieldCheck,
    link: { label: 'Open pools', to: '/pools' },
    canLoad: has => has('pools:read'),
    load: async () =>
      summarizeCompliance(await post<ComplianceReport>('/api/v1/analysis/compliance', { pool_ids: [], include_children: false })),
  },
  {
    key: 'sync',
    title: 'Failed syncs',
    icon: RefreshCw,
    link: { label: 'Open discovery', to: '/discovery' },
    canLoad: has => has('discovery:read') && (has('accounts:list') || has('accounts:read')),
    load: async () => summarizeSync(await loadAccountSyncs()),
  },
  {
    key: 'updates',
    title: 'CloudPAM version',
    icon: CloudDownload,
    link: { label: 'Manage updates', to: '/config/updates' },
    canLoad: has => has('settings:read'),
    load: async () => summarizeUpdate(await get<UpdateCheckResponse>('/api/v1/updates')),
  },
]

function errorMessage(err: unknown): string {
  return err instanceof Error && err.message ? err.message : 'request failed'
}

/** Loads every attention source the caller may read; each settles on its own. */
export function useAttention(): { source: AttentionSource; load: AttentionLoad }[] {
  const { hasPermission } = useAuth()
  const sources = useMemo(() => ATTENTION_SOURCES.filter(s => s.canLoad(hasPermission)), [hasPermission])
  const [loads, setLoads] = useState<Partial<Record<AttentionKey, AttentionLoad>>>({})

  useEffect(() => {
    let cancelled = false
    setLoads({})
    for (const source of sources) {
      source.load().then(
        summary => {
          if (!cancelled) setLoads(prev => ({ ...prev, [source.key]: { state: 'ready', summary } }))
        },
        (err: unknown) => {
          if (!cancelled) setLoads(prev => ({ ...prev, [source.key]: { state: 'error', message: errorMessage(err) } }))
        },
      )
    }
    return () => {
      cancelled = true
    }
  }, [sources])

  return sources.map(source => ({ source, load: loads[source.key] ?? { state: 'loading' } }))
}
