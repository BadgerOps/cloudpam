import { fireEvent, render, screen } from '@testing-library/react'
import { useState, type ComponentProps } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { Account, DiscoveredResource, Pool } from '../api/types'
import { ResourcesTab } from '../pages/DiscoveryPage'

const resources: DiscoveredResource[] = [
  {
    id: 'resource-active-vpc',
    account_id: 1,
    provider: 'aws',
    region: 'us-west-2',
    resource_type: 'vpc',
    resource_id: 'vpc-001',
    name: 'prod-vpc',
    cidr: '10.0.0.0/16',
    pool_id: null,
    status: 'active',
    discovered_at: '2026-01-01T00:00:00Z',
    last_seen_at: '2026-01-02T00:00:00Z',
  },
  {
    id: 'resource-stale-subnet',
    account_id: 1,
    provider: 'aws',
    region: 'us-west-2',
    resource_type: 'subnet',
    resource_id: 'subnet-001',
    name: 'stale-subnet',
    cidr: '10.0.1.0/24',
    pool_id: null,
    status: 'stale',
    discovered_at: '2026-01-01T00:00:00Z',
    last_seen_at: '2026-01-02T00:00:00Z',
  },
  {
    id: 'resource-linked-vpc',
    account_id: 1,
    provider: 'aws',
    region: 'us-west-2',
    resource_type: 'vpc',
    resource_id: 'vpc-linked',
    name: 'linked-vpc',
    cidr: '10.1.0.0/16',
    pool_id: 7,
    status: 'active',
    discovered_at: '2026-01-01T00:00:00Z',
    last_seen_at: '2026-01-02T00:00:00Z',
  },
]

const pools: Pool[] = [
  {
    id: 42,
    name: 'prod pool',
    cidr: '10.0.0.0/8',
    account_id: 1,
    type: 'supernet',
    status: 'active',
    source: 'manual',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 7,
    name: 'linked pool',
    cidr: '10.1.0.0/16',
    account_id: 1,
    type: 'vpc',
    status: 'active',
    source: 'manual',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 99,
    name: 'dev pool',
    cidr: '10.99.0.0/16',
    account_id: 2,
    type: 'vpc',
    status: 'active',
    source: 'manual',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
]

const accounts: Account[] = [
  {
    id: 1,
    key: 'aws:123456789012',
    name: 'Production AWS',
    provider: 'aws',
    external_id: '123456789012',
    created_at: '2026-01-01T00:00:00Z',
  },
]

function renderResourcesTab(overrides: Partial<ComponentProps<typeof ResourcesTab>> = {}) {
  const onBulkLink = vi.fn()
  const onPageChange = vi.fn()
  const onPageSizeChange = vi.fn()

  function Wrapper() {
    const [selectedResourceIds, setSelectedResourceIds] = useState<string[]>([])
    const toggleSelection = (resource: DiscoveredResource) => {
      if (resource.pool_id) return
      setSelectedResourceIds((current) =>
        current.includes(resource.id)
          ? current.filter((id) => id !== resource.id)
          : [...current, resource.id],
      )
    }

    const props: ComponentProps<typeof ResourcesTab> = {
      resources,
      loading: false,
      error: null,
      searchQuery: '',
      onSearchChange: vi.fn(),
      statusFilter: '',
      onStatusChange: vi.fn(),
      typeFilter: '',
      onTypeChange: vi.fn(),
      linkedFilter: '',
      onLinkedChange: vi.fn(),
      onLink: vi.fn(),
      onUnlink: vi.fn(),
      accounts,
      selectedAccountId: 1,
      pools,
      selectedResourceIds,
      onToggleSelection: toggleSelection,
      onSetVisibleSelection: (visibleResources, selected) => {
        const visibleIDs = visibleResources
          .filter((resource) => !resource.pool_id)
          .map((resource) => resource.id)
        setSelectedResourceIds((current) => {
          if (selected) {
            return Array.from(new Set([...current, ...visibleIDs]))
          }
          return current.filter((id) => !visibleIDs.includes(id))
        })
      },
      onClearSelection: () => setSelectedResourceIds([]),
      onBulkLink,
      total: resources.length,
      page: 1,
      pageSize: 25,
      onPageChange,
      onPageSizeChange,
      ...overrides,
    }

    return <ResourcesTab {...props} />
  }

  render(<Wrapper />)
  return { onBulkLink, onPageChange, onPageSizeChange }
}

describe('ResourcesTab', () => {
  it('selects multiple unlinked resources and links them to a pool', () => {
    const { onBulkLink } = renderResourcesTab()

    const activeCheckbox = screen.getAllByLabelText('Select prod-vpc')[0] as HTMLInputElement
    const staleCheckbox = screen.getAllByLabelText('Select stale-subnet')[0] as HTMLInputElement
    const linkedCheckbox = screen.getAllByLabelText('Select linked-vpc')[0] as HTMLInputElement

    expect(activeCheckbox.disabled).toBe(false)
    expect(staleCheckbox.disabled).toBe(false)
    expect(linkedCheckbox.disabled).toBe(true)

    fireEvent.click(activeCheckbox)
    fireEvent.click(staleCheckbox)

    expect(screen.getByText('2 selected')).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Pool for selected resources'), { target: { value: '42' } })
    fireEvent.click(screen.getByRole('button', { name: /Link selected/i }))

    expect(onBulkLink).toHaveBeenCalledWith(['resource-active-vpc', 'resource-stale-subnet'], 42)
  })

  it('filters bulk link pools to the selected account', () => {
    renderResourcesTab()

    const poolSelect = screen.getByLabelText('Pool for selected resources') as HTMLSelectElement

    expect(Array.from(poolSelect.options).map((option) => option.textContent)).toContain('prod pool (10.0.0.0/8)')
    expect(Array.from(poolSelect.options).map((option) => option.textContent)).not.toContain('dev pool (10.99.0.0/16)')
  })

  it('selects and clears all visible unlinked resources from the header checkbox', () => {
    renderResourcesTab()

    fireEvent.click(screen.getByLabelText('Select all visible resources'))

    expect(screen.getByText('2 selected')).toBeTruthy()

    fireEvent.click(screen.getByLabelText('Select all visible resources'))

    expect(screen.getByText('0 selected')).toBeTruthy()
  })

  it('shows account context and toggles configurable columns', () => {
    renderResourcesTab()

    expect(screen.getAllByText('Production AWS').length).toBeGreaterThan(0)
    expect(screen.getAllByText('123456789012').length).toBeGreaterThan(0)
    expect(screen.queryAllByRole('columnheader', { name: 'Account / Project' }).length).toBeGreaterThan(0)

    fireEvent.click(screen.getByRole('button', { name: /Columns/i }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Account / Project' }))

    expect(screen.queryAllByRole('columnheader', { name: 'Account / Project' })).toHaveLength(0)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Account / Project' }))

    expect(screen.queryAllByRole('columnheader', { name: 'Account / Project' }).length).toBeGreaterThan(0)
  })

  it('calls pagination handlers for page and page-size changes', () => {
    const { onPageChange, onPageSizeChange } = renderResourcesTab({
      total: 80,
      page: 2,
      pageSize: 25,
    })

    expect(screen.getByText('Showing 26-50 of 80')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    fireEvent.change(screen.getByLabelText('Rows per page'), { target: { value: '100' } })

    expect(onPageChange).toHaveBeenCalledWith(3)
    expect(onPageSizeChange).toHaveBeenCalledWith(100)
  })
})
