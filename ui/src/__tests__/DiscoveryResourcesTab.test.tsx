import { fireEvent, render, screen } from '@testing-library/react'
import { useState, type ComponentProps } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { DiscoveredResource, Pool } from '../api/types'
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
    type: 'vpc',
    status: 'active',
    source: 'manual',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
]

function renderResourcesTab(overrides: Partial<ComponentProps<typeof ResourcesTab>> = {}) {
  const onBulkLink = vi.fn()

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
      pools,
      selectedResourceIds,
      onToggleSelection: toggleSelection,
      onSelectVisible: (visibleResources) => {
        setSelectedResourceIds(
          visibleResources
            .filter((resource) => !resource.pool_id)
            .map((resource) => resource.id),
        )
      },
      onClearSelection: () => setSelectedResourceIds([]),
      onBulkLink,
      ...overrides,
    }

    return <ResourcesTab {...props} />
  }

  render(<Wrapper />)
  return { onBulkLink }
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
})
