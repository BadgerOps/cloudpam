import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { NetworkConflictList } from '../pages/DiscoveryPage'
import type { Account, DiscoveryImportPreviewResponse, NetworkConflict, NetworkConflictActionResponse, Pool } from '../api/types'

const conflict: NetworkConflict = {
  id: 'unlinked-exact-pool:00000000-0000-0000-0000-000000000001:42',
  type: 'unlinked_exact_pool',
  severity: 'warning',
  status: 'open',
  title: 'Discovered CIDR matches managed pool',
  description: 'vpc-prod exactly matches prod-vpc',
  recommended_action: 'Link the discovered resource to the matching managed pool.',
  discovered_ids: ['00000000-0000-0000-0000-000000000001'],
  pool_ids: [42],
  account_ids: [7],
  cidr: '10.0.0.0/16',
  evidence: ['pool_id=42', 'pool_cidr=10.0.0.0/16'],
  available_decisions: ['skip', 'ignore', 'defer'],
  relationships: [{
    id: 'matches/discovered/00000000-0000-0000-0000-000000000001/pool/42',
    type: 'matches',
    source_kind: 'discovered',
    source_id: '00000000-0000-0000-0000-000000000001',
    target_kind: 'pool',
    target_id: '42',
    confidence: 1,
    resolution_state: 'open',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }],
}

const accounts: Account[] = [{
  id: 7,
  key: 'aws:123456789012',
  name: 'Production AWS',
  provider: 'aws',
  created_at: '2026-01-01T00:00:00Z',
}]

const pools: Pool[] = [
  {
    id: 42,
    name: 'prod-vpc',
    cidr: '10.0.0.0/16',
    type: 'vpc',
    status: 'active',
    source: 'manual',
    account_id: 7,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
]

const missingParentConflict: NetworkConflict = {
  ...conflict,
  id: 'missing-parent:00000000-0000-0000-0000-000000000001',
  type: 'missing_parent',
  title: 'Discovered resource references missing parent',
  description: 'subnet-1 references vpc-missing',
  recommended_action: 'Create a placeholder parent, import, or mark reviewed.',
  pool_ids: [],
  evidence: ['parent_resource_id=vpc-missing'],
}

const relinkConflict: NetworkConflict = {
  ...conflict,
  id: 'alternate-exact-pool:00000000-0000-0000-0000-000000000001:42',
  type: 'alternate_exact_pool',
  title: 'Discovered CIDR matches an alternate pool',
  description: 'vpc-prod is linked to one pool and exactly matches prod-vpc',
  recommended_action: 'Relink the discovered resource to the alternate matching managed pool.',
  evidence: ['current_pool_id=41', 'alternate_pool_id=42'],
}

const duplicateConflict: NetworkConflict = {
  ...conflict,
  id: 'duplicate-cidr:account:172.31.0.0_16',
  type: 'duplicate_cidr',
  severity: 'critical',
  title: 'Duplicate CIDR in account',
  description: '172.31.0.0/16 is discovered multiple times in the same account',
  recommended_action: 'Choose the authoritative account or mark the duplicate reviewed.',
  pool_ids: [],
  account_ids: [7, 8],
  cidr: '172.31.0.0/16',
  evidence: ['policy=account_level', 'duplicate_scope=account'],
}

const preview: DiscoveryImportPreviewResponse = {
  items: [],
  importable: 1,
  blocked: 0,
  linked_only: 0,
  already_linked: 0,
  conflict_count: 0,
}

const actionResponse: NetworkConflictActionResponse = {
  conflict,
  action: 'link',
  resource_linked: true,
  discovered_id: '00000000-0000-0000-0000-000000000001',
  pool_id: 42,
  relationships: [],
}

function renderList(overrides: Partial<ComponentProps<typeof NetworkConflictList>> = {}) {
  const props: ComponentProps<typeof NetworkConflictList> = {
    conflicts: [conflict],
    loading: false,
    selected: conflict,
    onSelect: vi.fn(),
    onResolve: vi.fn(),
    onLink: vi.fn().mockResolvedValue(actionResponse),
    onImport: vi.fn().mockResolvedValue({ ...actionResponse, action: 'import' }),
    onPlaceholderParent: vi.fn().mockResolvedValue({ ...actionResponse, action: 'create_placeholder_parent' }),
    onPreviewImport: vi.fn().mockResolvedValue(preview),
    onViewFlat: vi.fn(),
    onViewHierarchy: vi.fn(),
    onShowRelationships: vi.fn(),
    pools,
    accounts,
    ...overrides,
  }
  render(<NetworkConflictList {...props} />)
  return props
}

describe('NetworkConflictList', () => {
  it('keeps passive review decisions separate from mutating actions', () => {
    const props = renderList()

    fireEvent.click(screen.getByRole('button', { name: 'skip' }))
    expect(props.onResolve).toHaveBeenCalledWith(conflict, 'skip')
    expect(screen.getByText('Review')).toBeTruthy()
    expect(screen.getByText('Actions')).toBeTruthy()
    expect(screen.getByRole('button', { name: /Link to pool/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /Import as pool/i })).toBeTruthy()
  })

  it('renders structured conflict detail sections and context navigation actions', () => {
    const props = renderList()

    expect(screen.getByText('Affected resources')).toBeTruthy()
    expect(screen.getAllByText(/00000000-0000-0000-0000-000000000001/).length).toBeGreaterThan(0)
    expect(screen.getByText('Ownership')).toBeTruthy()
    expect(screen.getByText(/Production AWS \(7\)/)).toBeTruthy()
    expect(screen.getByText('Pools and parent chain')).toBeTruthy()
    expect(screen.getByText(/prod-vpc \(42, 10.0.0.0\/16\)/)).toBeTruthy()
    expect(screen.getByText('CIDR/IP evidence')).toBeTruthy()
    expect(screen.getAllByText('Relationships').length).toBeGreaterThan(0)
    expect(screen.getByText('Operator note: link candidate')).toBeTruthy()
    expect(screen.getByText(/Use Link to pool when the managed pool is the authoritative record/i)).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'View in flat' }))
    fireEvent.click(screen.getByRole('button', { name: 'View in hierarchy' }))
    fireEvent.click(screen.getByRole('button', { name: 'Show relationships' }))

    expect(props.onViewFlat).toHaveBeenCalledWith(conflict)
    expect(props.onViewHierarchy).toHaveBeenCalledWith(conflict)
    expect(props.onShowRelationships).toHaveBeenCalledWith(conflict)
  })

  it('submits a link action with selected resource, pool, reason, and override', async () => {
    const props = renderList()

    fireEvent.click(screen.getByRole('button', { name: /Link to pool/i }))
    fireEvent.change(screen.getByPlaceholderText('Reason'), { target: { value: 'exact match reviewed' } })
    fireEvent.click(screen.getByLabelText('Override validation'))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm link' }))

    await waitFor(() => {
      expect(props.onLink).toHaveBeenCalledWith(
        conflict,
        '00000000-0000-0000-0000-000000000001',
        42,
        'exact match reviewed',
        true,
      )
    })
  })

  it('requires import preview before apply and submits selected resources', async () => {
    const props = renderList()

    fireEvent.click(screen.getByRole('button', { name: /Import as pool/i }))
    expect((screen.getByRole('button', { name: 'Apply import' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }))

    await waitFor(() => {
      expect(props.onPreviewImport).toHaveBeenCalledWith(
        conflict,
        ['00000000-0000-0000-0000-000000000001'],
        undefined,
      )
      expect(screen.getByText('1 importable, 0 blocked, 0 conflicts')).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: 'Apply import' }))
    await waitFor(() => {
      expect(props.onImport).toHaveBeenCalledWith(
        conflict,
        ['00000000-0000-0000-0000-000000000001'],
        undefined,
        '',
        false,
      )
    })
  })

  it('submits a placeholder-parent action for missing parent conflicts', async () => {
    const props = renderList({
      conflicts: [missingParentConflict],
      selected: missingParentConflict,
    })

    fireEvent.click(screen.getByRole('button', { name: /Placeholder parent/i }))
    fireEvent.change(screen.getByPlaceholderText('Placeholder name'), { target: { value: 'placeholder-vpc' } })
    fireEvent.change(screen.getByPlaceholderText('Reason'), { target: { value: 'parent not scanned yet' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create placeholder' }))

    await waitFor(() => {
      expect(props.onPlaceholderParent).toHaveBeenCalledWith(
        missingParentConflict,
        '00000000-0000-0000-0000-000000000001',
        'placeholder-vpc',
        'parent not scanned yet',
      )
    })
  })

  it('uses relink wording and displays previous pool result details for alternate exact pool conflicts', async () => {
    const props = renderList({
      conflicts: [relinkConflict],
      selected: relinkConflict,
      onLink: vi.fn().mockResolvedValue({
        ...actionResponse,
        conflict: relinkConflict,
        previous_pool_id: 41,
        pool_id: 42,
        relationships: [{ id: 'rel/one', type: 'matches', source_kind: 'discovered', source_id: '00000000-0000-0000-0000-000000000001', target_kind: 'pool', target_id: '42', confidence: 1, resolution_state: 'accepted', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }],
      }),
    })

    fireEvent.click(screen.getByRole('button', { name: /Relink to pool/i }))
    expect(screen.getByText(/updates the discovered resource pool association/i)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm relink' }))

    await waitFor(() => {
      expect(props.onLink).toHaveBeenCalledWith(
        relinkConflict,
        '00000000-0000-0000-0000-000000000001',
        42,
        '',
        false,
      )
      expect(screen.getByText('Resource relinked')).toBeTruthy()
      expect(screen.getByText('Previous pool: 41')).toBeTruthy()
      expect(screen.getByText('Target pool: 42')).toBeTruthy()
      expect(screen.getByText('1 relationship recorded')).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: 'View affected row' }))
    fireEvent.click(screen.getByRole('button', { name: 'View relationships' }))
    expect(props.onViewFlat).toHaveBeenCalledWith(relinkConflict)
    expect(props.onShowRelationships).toHaveBeenCalledWith(relinkConflict)
  })

  it('explains duplicate CIDR conflicts as review or ignore candidates', () => {
    renderList({
      conflicts: [duplicateConflict],
      selected: duplicateConflict,
    })

    expect(screen.getByText('Operator note: duplicate address space')).toBeTruthy()
    expect(screen.getByText(/Mark expected reuse ignored or defer it/i)).toBeTruthy()
  })
})
