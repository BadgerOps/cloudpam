import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { NetworkConflictList } from '../pages/DiscoveryPage'
import type { DiscoveryImportPreviewResponse, NetworkConflict, Pool } from '../api/types'

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
}

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

const preview: DiscoveryImportPreviewResponse = {
  items: [],
  importable: 1,
  blocked: 0,
  linked_only: 0,
  already_linked: 0,
  conflict_count: 0,
}

function renderList(overrides: Partial<ComponentProps<typeof NetworkConflictList>> = {}) {
  const props: ComponentProps<typeof NetworkConflictList> = {
    conflicts: [conflict],
    loading: false,
    selected: conflict,
    onSelect: vi.fn(),
    onResolve: vi.fn(),
    onLink: vi.fn(),
    onImport: vi.fn(),
    onPreviewImport: vi.fn().mockResolvedValue(preview),
    pools,
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

  it('submits a link action with selected resource, pool, reason, and override', () => {
    const props = renderList()

    fireEvent.click(screen.getByRole('button', { name: /Link to pool/i }))
    fireEvent.change(screen.getByPlaceholderText('Reason'), { target: { value: 'exact match reviewed' } })
    fireEvent.click(screen.getByLabelText('Override validation'))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm link' }))

    expect(props.onLink).toHaveBeenCalledWith(
      conflict,
      '00000000-0000-0000-0000-000000000001',
      42,
      'exact match reviewed',
      true,
    )
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
    expect(props.onImport).toHaveBeenCalledWith(
      conflict,
      ['00000000-0000-0000-0000-000000000001'],
      undefined,
      '',
      false,
    )
  })
})
