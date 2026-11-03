import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { NetworkTab } from '../pages/DiscoveryPage'
import type { NetworkViewResponse } from '../api/types'

const fetchHierarchy = vi.fn()
let hierarchy: NetworkViewResponse | null = null

vi.mock('../hooks/useNetwork', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../hooks/useNetwork')>()),
  useNetworkView: () => ({
    flat: null,
    hierarchy,
    conflicts: null,
    objects: null,
    relationships: null,
    loading: false,
    error: null,
    fetchFlat: vi.fn(),
    fetchHierarchy,
    fetchConflicts: vi.fn(),
    fetchObjects: vi.fn(),
    fetchRelationships: vi.fn(),
    deleteObject: vi.fn(),
    resolveConflict: vi.fn(),
    linkConflict: vi.fn(),
    importConflict: vi.fn(),
    createPlaceholderParentConflict: vi.fn(),
    resolveNetworkRelationship: vi.fn(),
  }),
}))

vi.mock('../hooks/useDiscovery', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../hooks/useDiscovery')>()),
  useSyncJobs: () => ({ previewDiscoveryImport: vi.fn() }),
}))

vi.mock('../hooks/useAuth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../hooks/useAuth')>()),
  useAuth: () => ({ hasPermission: () => false }),
}))

function storedPolicyView(name: string): NetworkViewResponse {
  return {
    items: [],
    total: 0,
    conflict_count: 0,
    schema_policy: { name, ownership_strategy: 'region', duplicate_scope: 'region', hierarchy_scope: 'region' },
  }
}

const policySelect = () => screen.getByLabelText('Schema policy') as HTMLSelectElement

beforeEach(() => {
  fetchHierarchy.mockReset().mockResolvedValue(undefined)
  hierarchy = null
})

afterEach(() => {
  cleanup()
})

describe('NetworkTab schema policy', () => {
  it('asks for the stored policy and shows the one the server applied', () => {
    hierarchy = storedPolicyView('region_level')
    render(<NetworkTab selectedAccountId={null} accounts={[]} pools={[]} />)

    expect(fetchHierarchy).toHaveBeenCalled()
    expect(fetchHierarchy.mock.calls[0][0].schema_policy).toBeUndefined()
    expect(policySelect().value).toBe('region_level')
    expect(screen.getByText('region policy, stored default')).toBeTruthy()
  })

  it('falls back to the account policy before any view has loaded', () => {
    render(<NetworkTab selectedAccountId={null} accounts={[]} pools={[]} />)

    expect(policySelect().value).toBe('account_level')
    expect(screen.getByText('account policy, stored default')).toBeTruthy()
  })

  it('sends a request-scoped policy once the user picks one', async () => {
    hierarchy = storedPolicyView('region_level')
    render(<NetworkTab selectedAccountId={null} accounts={[]} pools={[]} />)

    fireEvent.change(policySelect(), { target: { value: 'global' } })

    await waitFor(() => expect(fetchHierarchy).toHaveBeenLastCalledWith(expect.objectContaining({ schema_policy: 'global' })))
    expect(policySelect().value).toBe('global')
    expect(screen.getByText('global policy, request-scoped')).toBeTruthy()
  })
})
