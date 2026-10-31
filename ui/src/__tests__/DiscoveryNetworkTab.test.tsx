import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { NetworkTab } from '../pages/DiscoveryPage'
import type { NetworkObject } from '../api/types'

const object: NetworkObject = {
  id: 42,
  object_type: 'vpc',
  provider: 'aws',
  account_id: 1,
  name: 'prod-vpc',
  cidr: '10.0.0.0/16',
  state: 'active',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const deleteObject = vi.fn()
const fetchFlat = vi.fn()
const fetchHierarchy = vi.fn()
const fetchConflicts = vi.fn()
const fetchObjects = vi.fn()
const fetchRelationships = vi.fn()
const hasPermission = vi.fn()
const showToast = vi.fn()

vi.mock('../hooks/useNetwork', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../hooks/useNetwork')>()),
  useNetworkView: () => ({
    flat: null,
    hierarchy: null,
    conflicts: null,
    objects: { items: [object], total: 1 },
    relationships: null,
    loading: false,
    error: null,
    fetchFlat,
    fetchHierarchy,
    fetchConflicts,
    fetchObjects,
    fetchRelationships,
    deleteObject,
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
  useAuth: () => ({ hasPermission: (permission: string) => hasPermission(permission) }),
}))

vi.mock('../hooks/useToast', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../hooks/useToast')>()),
  useToast: () => ({ showToast }),
}))

function renderObjectsMode() {
  render(<NetworkTab selectedAccountId={1} accounts={[]} pools={[]} />)
  fireEvent.click(screen.getByRole('button', { name: 'Objects' }))
  expect(screen.getByText('prod-vpc')).toBeTruthy()
}

beforeEach(() => {
  deleteObject.mockReset().mockResolvedValue(undefined)
  for (const fetcher of [fetchFlat, fetchHierarchy, fetchConflicts, fetchObjects, fetchRelationships]) {
    fetcher.mockReset().mockResolvedValue(undefined)
  }
  hasPermission.mockReset()
  showToast.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('NetworkTab objects mode', () => {
  it('hides the delete action without discovery:update', () => {
    hasPermission.mockReturnValue(false)
    renderObjectsMode()
    expect(screen.queryByLabelText('Delete network object prod-vpc')).toBeNull()
  })

  it('deletes the object through the network hook with discovery:update', async () => {
    hasPermission.mockImplementation((permission: string) => permission === 'discovery:update')
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderObjectsMode()

    fireEvent.click(screen.getByLabelText('Delete network object prod-vpc'))

    await waitFor(() => expect(deleteObject).toHaveBeenCalledTimes(1))
    expect(deleteObject).toHaveBeenCalledWith(42)
    expect(hasPermission).toHaveBeenCalledWith('discovery:update')
  })
})
