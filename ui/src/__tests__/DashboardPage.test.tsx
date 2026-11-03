import { render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DashboardPage from '../pages/DashboardPage'

const mockGet = vi.hoisted(() => vi.fn())
const mockPost = vi.hoisted(() => vi.fn())
const mockUseAuth = vi.hoisted(() => vi.fn())

vi.mock('../api/client', async importOriginal => ({
  ...(await importOriginal<typeof import('../api/client')>()),
  get: mockGet,
  post: mockPost,
}))
vi.mock('../hooks/useAuth', () => ({ useAuth: () => mockUseAuth() }))

const now = new Date().toISOString()

const hierarchy = {
  pools: [
    {
      id: 1,
      name: 'corp',
      cidr: '10.0.0.0/8',
      stats: { total_ips: 100, used_ips: 90, available_ips: 10, utilization: 90, child_count: 0, direct_children: 0 },
    },
  ],
}

const accounts = [
  { id: 7, key: 'aws:1', name: 'prod-aws', provider: 'aws', created_at: now },
  { id: 8, key: 'gcp:1', name: 'lab-gcp', provider: 'gcp', created_at: now },
]

const driftSummary = { total_drifts: 3, by_severity: { critical: 1, warning: 2 }, by_type: {}, accounts_scanned: 0, resources_scanned: 0, pools_scanned: 0 }
const driftItem = { id: 'd1', account_id: 7, type: 'unmanaged', severity: 'critical', status: 'open', title: 'Unmanaged VPC vpc-1', description: '' }

const routes: Record<string, unknown> = {
  '/api/v1/pools/hierarchy': hierarchy,
  '/api/v1/accounts': accounts,
  '/api/v1/drift?status=open&page_size=5': { items: [driftItem], total: 3, page: 1, page_size: 5, summary: driftSummary },
  '/api/v1/drift?status=open&severity=critical&page_size=5': { items: [driftItem], total: 1, page: 1, page_size: 5, summary: driftSummary },
  '/api/v1/recommendations?status=pending&page_size=5': { items: [], total: 0, page: 1, page_size: 5 },
  '/api/v1/recommendations?status=pending&priority=high&page_size=5': { items: [], total: 0, page: 1, page_size: 5 },
  '/api/v1/discovery/sync?account_id=7&limit=1': {
    items: [{ id: 'j1', account_id: 7, status: 'failed', error_message: 'access denied', resources_found: 0, resources_created: 0, resources_updated: 0, resources_deleted: 0, created_at: now, completed_at: now }],
  },
  '/api/v1/discovery/sync?account_id=8&limit=1': { items: [] },
  '/api/v1/updates': { current_version: '0.23.0', latest_version: '0.24.0', update_available: true, checked_at: now },
}

function permissions(granted: string[] | 'admin') {
  mockUseAuth.mockReturnValue({ hasPermission: (p: string) => granted === 'admin' || granted.includes(p) })
}

function renderPage() {
  return render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  )
}

function card(name: string) {
  return screen.getByRole('region', { name })
}

async function settled(name: string) {
  await waitFor(() => expect(card(name).getAttribute('aria-busy')).toBeNull())
  return card(name)
}

beforeEach(() => {
  vi.clearAllMocks()
  mockGet.mockImplementation(async (path: string) => {
    if (path.startsWith('/api/v1/audit')) return { events: [], total: 0 }
    if (!(path in routes)) throw new Error(`unexpected GET ${path}`)
    return routes[path]
  })
  mockPost.mockImplementation(async (path: string) => {
    if (path !== '/api/v1/analysis/compliance') throw new Error(`unexpected POST ${path}`)
    return {
      total_checks: 4,
      passed: 3,
      failed: 1,
      warnings: 0,
      violations: [{ rule_id: 'OVERLAP-001', severity: 'error', pool_id: 1, pool_name: 'corp', message: 'overlaps lab' }],
    }
  })
})

describe('DashboardPage', () => {
  it('answers what needs attention with a link into each workflow', async () => {
    permissions('admin')
    renderPage()

    const drift = await settled('Open drift')
    expect(within(drift).getByText('3')).toBeTruthy()
    expect(within(drift).getByText('1 critical, 2 warning')).toBeTruthy()
    expect(within(drift).getByRole('link', { name: /Unmanaged VPC vpc-1/ }).getAttribute('href')).toBe('/drift')
    expect(drift.getAttribute('data-tone')).toBe('critical')

    const recs = await settled('Pending recommendations')
    expect(within(recs).getByText('No pending recommendations')).toBeTruthy()
    expect(recs.getAttribute('data-tone')).toBe('ok')

    const compliance = await settled('Compliance findings')
    expect(within(compliance).getByRole('link', { name: /corp.*overlaps lab/ }).getAttribute('href')).toBe('/pools?selected=1')

    const sync = await settled('Failed syncs')
    expect(within(sync).getByText('1 failed, 1 never synced')).toBeTruthy()
    const syncRows = within(sync).getAllByRole('listitem').map(li => li.textContent)
    expect(syncRows[0]).toContain('prod-aws')
    expect(syncRows[0]).toContain('access denied')
    expect(syncRows[1]).toContain('lab-gcp')
    expect(syncRows[1]).toContain('never synced')

    const version = await settled('CloudPAM version')
    expect(within(version).getByText('0.24.0')).toBeTruthy()
    expect(within(version).getByRole('link', { name: 'Manage updates' }).getAttribute('href')).toBe('/config/updates')

    const utilization = await settled('Pool utilization')
    expect(within(utilization).getByRole('link', { name: /corp.*90\.0% used/ }).getAttribute('href')).toBe('/pools?selected=1')
  })

  it('shows a viewer only the sources their role can read', async () => {
    permissions(['pools:read', 'pools:list', 'accounts:read', 'accounts:list', 'discovery:read', 'discovery:list'])
    renderPage()

    await settled('Failed syncs')
    expect(screen.queryByRole('region', { name: 'CloudPAM version' })).toBeNull()
    expect(screen.queryByText('Recent Activity')).toBeNull()
    const requested = mockGet.mock.calls.map(([path]) => path as string)
    expect(requested.some(p => p.startsWith('/api/v1/updates'))).toBe(false)
    expect(requested.some(p => p.startsWith('/api/v1/audit'))).toBe(false)
  })

  it('keeps the other cards when one source fails', async () => {
    permissions('admin')
    mockPost.mockRejectedValue(new Error('analysis unavailable'))
    renderPage()

    const compliance = await settled('Compliance findings')
    expect(within(compliance).getByText('Could not load: analysis unavailable')).toBeTruthy()
    expect(within(await settled('Open drift')).getByText('3')).toBeTruthy()
  })
})
