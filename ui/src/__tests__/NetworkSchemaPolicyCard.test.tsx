import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import NetworkSchemaPolicyCard from '../components/NetworkSchemaPolicyCard'
import type { NetworkSchemaPolicy } from '../api/types'

const mockUseAuth = vi.fn()
vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))

const showToast = vi.fn()
vi.mock('../hooks/useToast', () => ({
  useToast: () => ({ showToast }),
}))

const POLICIES: Record<string, NetworkSchemaPolicy> = {
  account_level: { name: 'account_level', ownership_strategy: 'account', duplicate_scope: 'account', hierarchy_scope: 'account' },
  region_level: { name: 'region_level', ownership_strategy: 'region', duplicate_scope: 'region', hierarchy_scope: 'region' },
  global: { name: 'global', ownership_strategy: 'global', duplicate_scope: 'global', hierarchy_scope: 'global' },
  manual: { name: 'manual', ownership_strategy: 'manual', duplicate_scope: 'manual', hierarchy_scope: 'manual', manual_relationships: true },
}

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

interface Call { url: string; method: string; body: unknown }
const calls: Call[] = []
const fetchMock = vi.fn()

function serve(stored: NetworkSchemaPolicy, onPatch?: (body: Partial<NetworkSchemaPolicy>) => Response) {
  fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = {
      url: String(input),
      method: init?.method ?? 'GET',
      body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined,
    }
    calls.push(call)
    if (call.method === 'PATCH') {
      if (onPatch) return onPatch(call.body as Partial<NetworkSchemaPolicy>)
      const body = call.body as Partial<NetworkSchemaPolicy>
      const next = { ...POLICIES[body.name ?? 'account_level'] }
      if (body.name === 'manual' && body.duplicate_scope === 'global') next.duplicate_scope = 'global'
      return json(200, next)
    }
    return json(200, stored)
  })
}

function asUser(perms: string[]) {
  mockUseAuth.mockReturnValue({ hasPermission: (p: string) => perms.includes(p) })
}

const policySelect = () => screen.getByLabelText('Policy') as HTMLSelectElement
const patches = () => calls.filter((c) => c.method === 'PATCH')

async function renderCard() {
  render(<NetworkSchemaPolicyCard />)
  await screen.findByLabelText('Policy')
}

describe('NetworkSchemaPolicyCard', () => {
  beforeEach(() => {
    calls.length = 0
    fetchMock.mockReset()
    showToast.mockReset()
    vi.stubGlobal('fetch', fetchMock)
    asUser(['settings:read', 'settings:write'])
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('shows the stored policy from the settings endpoint', async () => {
    serve(POLICIES.region_level)
    await renderCard()

    expect(calls[0].url).toBe('/api/v1/settings/network-schema-policy')
    expect(policySelect().value).toBe('region_level')
    expect(screen.queryByLabelText('Duplicate CIDRs')).toBeNull()
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('saves a new policy name and shows what the server stored', async () => {
    serve(POLICIES.account_level)
    await renderCard()

    fireEvent.change(policySelect(), { target: { value: 'global' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0].url).toBe('/api/v1/settings/network-schema-policy')
    expect(patches()[0].body).toEqual({ name: 'global' })
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('Network schema policy saved', 'success'))
    expect(policySelect().value).toBe('global')
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('keeps the duplicate scope for the manual policy', async () => {
    serve(POLICIES.account_level)
    await renderCard()

    fireEvent.change(policySelect(), { target: { value: 'manual' } })
    const scope = screen.getByLabelText('Duplicate CIDRs') as HTMLSelectElement
    expect(scope.value).toBe('manual')
    fireEvent.change(scope, { target: { value: 'global' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0].body).toEqual({ name: 'manual', duplicate_scope: 'global' })
    await waitFor(() => expect((screen.getByLabelText('Duplicate CIDRs') as HTMLSelectElement).value).toBe('global'))
  })

  it('shows a stored custom policy with a global duplicate scope as manual', async () => {
    serve({ ...POLICIES.manual, name: 'custom', duplicate_scope: 'global' })
    await renderCard()

    expect(policySelect().value).toBe('manual')
    expect((screen.getByLabelText('Duplicate CIDRs') as HTMLSelectElement).value).toBe('global')
  })

  it('shows the server rejection and keeps the edit', async () => {
    serve(POLICIES.account_level, () => json(400, { error: 'invalid network schema policy', detail: 'duplicate_scope for manual policy must be manual or global' }))
    await renderCard()

    fireEvent.change(policySelect(), { target: { value: 'region_level' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await screen.findByText('invalid network schema policy: duplicate_scope for manual policy must be manual or global')
    expect(policySelect().value).toBe('region_level')
    expect(showToast).not.toHaveBeenCalled()
  })

  it('is read-only without settings:write', async () => {
    asUser(['settings:read'])
    serve(POLICIES.global)
    await renderCard()

    expect(policySelect().disabled).toBe(true)
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    expect(screen.getByText('You need settings:write to change the policy.')).toBeTruthy()
  })

  it('renders nothing and makes no request without settings:read', () => {
    asUser([])
    serve(POLICIES.global)
    const { container } = render(<NetworkSchemaPolicyCard />)

    expect(container.innerHTML).toBe('')
    expect(calls).toHaveLength(0)
  })
})
