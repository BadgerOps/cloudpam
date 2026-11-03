import type {
  Account,
  ComplianceReport,
  ComplianceSeverity,
  DriftListResponse,
  DriftSeverity,
  RecommendationPriority,
  RecommendationsListResponse,
  SyncJob,
  UpdateCheckResponse,
} from '../api/types'
import type { MetricTone } from '../components/primitives'
import { formatTimeAgo } from './format'

export interface AttentionItem {
  key: string
  label: string
  detail: string
  tone: MetricTone
  to: string
}

export interface AttentionSummary {
  value: string
  tone: MetricTone
  detail: string
  items: AttentionItem[]
}

export const ITEM_LIMIT = 5

function countPhrase(counts: [number | undefined, string][]): string {
  return counts
    .filter(([n]) => (n ?? 0) > 0)
    .map(([n, label]) => `${n} ${label}`)
    .join(', ')
}

const DRIFT_TONE: Record<DriftSeverity, MetricTone> = { critical: 'critical', warning: 'warning', info: 'info' }

// open lists every open item; critical lists only the critical ones, which are
// shown first because the list endpoint orders by recency, not severity.
export function summarizeDrift(open: DriftListResponse, critical: DriftListResponse): AttentionSummary {
  if (open.total === 0) return { value: '0', tone: 'ok', detail: 'No open drift', items: [] }
  const bySeverity = open.summary.by_severity
  const shown = critical.items.length > 0 ? critical.items : open.items
  return {
    value: String(open.total),
    tone: bySeverity.critical ? 'critical' : bySeverity.warning ? 'warning' : 'info',
    detail: countPhrase([[bySeverity.critical, 'critical'], [bySeverity.warning, 'warning'], [bySeverity.info, 'info']]),
    items: shown.slice(0, ITEM_LIMIT).map(d => ({
      key: d.id,
      label: d.title,
      detail: `${d.severity} · ${d.type.replace(/_/g, ' ')}`,
      tone: DRIFT_TONE[d.severity] ?? 'neutral',
      to: '/drift',
    })),
  }
}

const PRIORITY_TONE: Record<RecommendationPriority, MetricTone> = { high: 'warning', medium: 'info', low: 'neutral' }

export function summarizeRecommendations(
  pending: RecommendationsListResponse,
  high: RecommendationsListResponse,
): AttentionSummary {
  if (pending.total === 0) return { value: '0', tone: 'ok', detail: 'No pending recommendations', items: [] }
  const shown = high.items.length > 0 ? high.items : pending.items
  return {
    value: String(pending.total),
    tone: high.total > 0 ? 'warning' : 'info',
    detail: high.total > 0 ? `${high.total} high priority` : 'None high priority',
    items: shown.slice(0, ITEM_LIMIT).map(r => ({
      key: r.id,
      label: r.title,
      detail: `${r.priority} priority · ${r.type}`,
      tone: PRIORITY_TONE[r.priority] ?? 'neutral',
      to: '/recommendations',
    })),
  }
}

const COMPLIANCE_RANK: Record<ComplianceSeverity, number> = { error: 0, warning: 1, info: 2 }
const COMPLIANCE_TONE: Record<ComplianceSeverity, MetricTone> = { error: 'critical', warning: 'warning', info: 'info' }

export function summarizeCompliance(report: ComplianceReport): AttentionSummary {
  if (report.total_checks === 0) return { value: '0', tone: 'neutral', detail: 'No pools to check', items: [] }
  const violations = report.violations ?? []
  if (violations.length === 0) {
    return { value: '0', tone: 'ok', detail: `All ${report.total_checks} checks passed`, items: [] }
  }
  const count = (s: ComplianceSeverity) => violations.filter(v => v.severity === s).length
  const sorted = [...violations].sort((a, b) => COMPLIANCE_RANK[a.severity] - COMPLIANCE_RANK[b.severity])
  return {
    value: String(violations.length),
    tone: COMPLIANCE_TONE[sorted[0].severity] ?? 'neutral',
    detail: countPhrase([[count('error'), 'error'], [count('warning'), 'warning'], [count('info'), 'info']]),
    items: sorted.slice(0, ITEM_LIMIT).map((v, i) => ({
      key: `${v.rule_id}:${v.pool_id}:${i}`,
      label: v.pool_name,
      detail: `${v.severity}: ${v.message}`,
      tone: COMPLIANCE_TONE[v.severity] ?? 'neutral',
      to: `/pools?selected=${v.pool_id}`,
    })),
  }
}

export interface AccountSync {
  account: Account
  latest: SyncJob | null
}

type SyncState = 'failed' | 'running' | 'pending' | 'completed' | 'never'

const SYNC_STATES: Record<SyncState, { rank: number; tone: MetricTone }> = {
  failed: { rank: 0, tone: 'critical' },
  running: { rank: 1, tone: 'info' },
  pending: { rank: 2, tone: 'info' },
  completed: { rank: 3, tone: 'ok' },
  never: { rank: 4, tone: 'neutral' },
}

function describeSync(job: SyncJob | null): string {
  if (!job) return 'never synced'
  switch (job.status) {
    case 'failed':
      return `failed ${formatTimeAgo(job.completed_at || job.created_at)}${job.error_message ? `: ${job.error_message}` : ''}`
    case 'running':
      return `running since ${formatTimeAgo(job.started_at || job.created_at)}`
    case 'pending':
      return `queued ${formatTimeAgo(job.created_at)}`
    case 'completed':
      return `synced ${formatTimeAgo(job.completed_at || job.created_at)} · ${job.resources_found} resources`
  }
}

function syncState(row: AccountSync): SyncState {
  return row.latest ? row.latest.status : 'never'
}

export function summarizeSync(rows: AccountSync[]): AttentionSummary {
  if (rows.length === 0) return { value: '0', tone: 'neutral', detail: 'No cloud accounts', items: [] }
  const counts: Record<SyncState, number> = { failed: 0, running: 0, pending: 0, completed: 0, never: 0 }
  for (const row of rows) counts[syncState(row)]++
  const sorted = [...rows].sort(
    (a, b) => SYNC_STATES[syncState(a)].rank - SYNC_STATES[syncState(b)].rank || a.account.name.localeCompare(b.account.name),
  )
  const tone: MetricTone =
    counts.failed > 0 ? 'critical' : counts.running + counts.pending > 0 ? 'info' : counts.completed > 0 ? 'ok' : 'neutral'
  return {
    value: String(counts.failed),
    tone,
    detail: countPhrase([
      [counts.failed, 'failed'],
      [counts.running, 'running'],
      [counts.pending, 'queued'],
      [counts.completed, 'synced'],
      [counts.never, 'never synced'],
    ]),
    items: sorted.slice(0, ITEM_LIMIT).map(row => ({
      key: String(row.account.id),
      label: row.account.name,
      detail: describeSync(row.latest),
      tone: SYNC_STATES[syncState(row)].tone,
      to: '/discovery',
    })),
  }
}

export function summarizeUpdate(update: UpdateCheckResponse): AttentionSummary {
  if (update.error) {
    return { value: update.current_version, tone: 'warning', detail: `Update check failed: ${update.error}`, items: [] }
  }
  if (update.update_available) {
    return {
      value: update.latest_version,
      tone: 'info',
      detail: `Running ${update.current_version}, ${update.latest_version} is available`,
      items: [],
    }
  }
  return { value: update.current_version, tone: 'ok', detail: 'Up to date', items: [] }
}
