import { describe, expect, it } from 'vitest'
import type {
  Account,
  ComplianceReport,
  DriftItem,
  DriftListResponse,
  Recommendation,
  RecommendationsListResponse,
  SyncJob,
} from '../api/types'
import {
  summarizeCompliance,
  summarizeDrift,
  summarizeRecommendations,
  summarizeSync,
  summarizeUpdate,
} from '../utils/attention'

function drift(id: string, severity: DriftItem['severity']): DriftItem {
  return {
    id,
    account_id: 1,
    type: 'cidr_mismatch',
    severity,
    status: 'open',
    title: `drift ${id}`,
    description: '',
    detected_at: '2026-11-01T00:00:00Z',
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
  } as DriftItem
}

function driftList(items: DriftItem[], total: number, bySeverity: Record<string, number> = {}): DriftListResponse {
  return {
    items,
    total,
    page: 1,
    page_size: 5,
    summary: { total_drifts: total, by_severity: bySeverity, by_type: {}, accounts_scanned: 0, resources_scanned: 0, pools_scanned: 0 },
  }
}

function rec(id: string, priority: Recommendation['priority']): Recommendation {
  return { id, pool_id: 1, type: 'allocation', status: 'pending', priority, title: `rec ${id}`, description: '', score: 1, created_at: '' } as Recommendation
}

function recList(items: Recommendation[], total: number): RecommendationsListResponse {
  return { items, total, page: 1, page_size: 5 }
}

function account(id: number, name: string): Account {
  return { id, key: `k${id}`, name, created_at: '' } as Account
}

function job(status: SyncJob['status'], extra: Partial<SyncJob> = {}): SyncJob {
  return {
    id: 'j',
    account_id: 1,
    status,
    resources_found: 7,
    resources_created: 0,
    resources_updated: 0,
    resources_deleted: 0,
    created_at: new Date().toISOString(),
    completed_at: new Date().toISOString(),
    ...extra,
  }
}

describe('summarizeDrift', () => {
  it('reports all clear when nothing is open', () => {
    expect(summarizeDrift(driftList([], 0), driftList([], 0))).toMatchObject({ value: '0', tone: 'ok', items: [] })
  })

  it('counts every open item and lists critical ones first', () => {
    const open = driftList([drift('w1', 'warning'), drift('i1', 'info')], 9, { critical: 1, warning: 6, info: 2 })
    const critical = driftList([drift('c1', 'critical')], 1, { critical: 1, warning: 6, info: 2 })

    const s = summarizeDrift(open, critical)

    expect(s.value).toBe('9')
    expect(s.tone).toBe('critical')
    expect(s.detail).toBe('1 critical, 6 warning, 2 info')
    expect(s.items.map(i => i.label)).toEqual(['drift c1'])
    expect(s.items[0].detail).toBe('critical · cidr mismatch')
  })

  it('falls back to the open list and warning tone without critical drift', () => {
    const s = summarizeDrift(driftList([drift('w1', 'warning')], 1, { warning: 1 }), driftList([], 0, { warning: 1 }))

    expect(s.tone).toBe('warning')
    expect(s.items.map(i => i.label)).toEqual(['drift w1'])
  })
})

describe('summarizeRecommendations', () => {
  it('reports none pending', () => {
    expect(summarizeRecommendations(recList([], 0), recList([], 0))).toMatchObject({ value: '0', tone: 'ok' })
  })

  it('warns and lists high priority first when any is pending', () => {
    const s = summarizeRecommendations(recList([rec('l', 'low')], 12), recList([rec('h', 'high')], 2))

    expect(s).toMatchObject({ value: '12', tone: 'warning', detail: '2 high priority' })
    expect(s.items.map(i => i.label)).toEqual(['rec h'])
  })

  it('stays informational when nothing is high priority', () => {
    const s = summarizeRecommendations(recList([rec('m', 'medium')], 1), recList([], 0))

    expect(s).toMatchObject({ value: '1', tone: 'info', detail: 'None high priority' })
    expect(s.items.map(i => i.label)).toEqual(['rec m'])
  })
})

describe('summarizeCompliance', () => {
  const report = (violations: ComplianceReport['violations'], total = 8): ComplianceReport => ({
    total_checks: total,
    passed: 0,
    failed: 0,
    warnings: 0,
    violations,
  })

  it('says there is nothing to check without pools', () => {
    expect(summarizeCompliance(report(null, 0))).toMatchObject({ tone: 'neutral', detail: 'No pools to check' })
  })

  it('passes when there are no violations, including a null list', () => {
    expect(summarizeCompliance(report(null))).toMatchObject({ value: '0', tone: 'ok', detail: 'All 8 checks passed' })
  })

  it('orders errors first and links each finding to its pool', () => {
    const s = summarizeCompliance(
      report([
        { rule_id: 'NAME-001', severity: 'info', pool_id: 3, pool_name: 'c', message: 'name' },
        { rule_id: 'OVERLAP-001', severity: 'error', pool_id: 4, pool_name: 'd', message: 'overlaps e' },
        { rule_id: 'EMPTY-001', severity: 'warning', pool_id: 5, pool_name: 'f', message: 'empty' },
      ]),
    )

    expect(s).toMatchObject({ value: '3', tone: 'critical', detail: '1 error, 1 warning, 1 info' })
    expect(s.items.map(i => i.label)).toEqual(['d', 'f', 'c'])
    expect(s.items[0]).toMatchObject({ detail: 'error: overlaps e', to: '/pools?selected=4' })
  })
})

describe('summarizeSync', () => {
  it('says there are no accounts', () => {
    expect(summarizeSync([])).toMatchObject({ value: '0', tone: 'neutral', detail: 'No cloud accounts' })
  })

  it('counts failures and lists every account by urgency', () => {
    const s = summarizeSync([
      { account: account(1, 'zeta'), latest: job('completed') },
      { account: account(2, 'alpha'), latest: null },
      { account: account(3, 'beta'), latest: job('failed', { error_message: 'access denied' }) },
      { account: account(4, 'gamma'), latest: job('running', { started_at: new Date().toISOString() }) },
    ])

    expect(s).toMatchObject({ value: '1', tone: 'critical', detail: '1 failed, 1 running, 1 synced, 1 never synced' })
    expect(s.items.map(i => i.label)).toEqual(['beta', 'gamma', 'zeta', 'alpha'])
    expect(s.items.map(i => i.detail)).toEqual([
      'failed just now: access denied',
      'running since just now',
      'synced just now · 7 resources',
      'never synced',
    ])
  })

  it('is healthy when every latest sync completed', () => {
    expect(summarizeSync([{ account: account(1, 'a'), latest: job('completed') }])).toMatchObject({ value: '0', tone: 'ok' })
  })
})

describe('summarizeUpdate', () => {
  const base = { current_version: '0.23.0', latest_version: '0.23.0', update_available: false, checked_at: '' }

  it('is up to date', () => {
    expect(summarizeUpdate(base)).toMatchObject({ value: '0.23.0', tone: 'ok', detail: 'Up to date' })
  })

  it('shows the newer version', () => {
    expect(summarizeUpdate({ ...base, latest_version: '0.24.0', update_available: true })).toMatchObject({
      value: '0.24.0',
      tone: 'info',
      detail: 'Running 0.23.0, 0.24.0 is available',
    })
  })

  it('surfaces a failed check', () => {
    expect(summarizeUpdate({ ...base, error: 'rate limited' })).toMatchObject({
      tone: 'warning',
      detail: 'Update check failed: rate limited',
    })
  })
})
