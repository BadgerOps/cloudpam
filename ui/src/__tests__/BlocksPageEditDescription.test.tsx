import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import BlocksPage from '../pages/BlocksPage'
import type { Block } from '../api/types'

const mockGet = vi.hoisted(() => vi.fn())
const mockPatch = vi.hoisted(() => vi.fn())
const mockDel = vi.hoisted(() => vi.fn())
const mockUseBlocks = vi.hoisted(() => vi.fn())
const mockUseAccounts = vi.hoisted(() => vi.fn())
const mockShowToast = vi.hoisted(() => vi.fn())

vi.mock('../api/client', () => ({
  get: (path: string) => mockGet(path),
  patch: (path: string, body: unknown) => mockPatch(path, body),
  del: (path: string) => mockDel(path),
}))

vi.mock('../hooks/useBlocks', () => ({
  useBlocks: () => mockUseBlocks(),
}))

vi.mock('../hooks/useAccounts', () => ({
  useAccounts: () => mockUseAccounts(),
}))

vi.mock('../hooks/useToast', () => ({
  useToast: () => ({ showToast: mockShowToast }),
}))

const block: Block = {
  id: 42,
  name: 'prod-subnet',
  cidr: '10.0.1.0/24',
  type: 'subnet',
  status: 'active',
  created_at: '2026-01-01T00:00:00Z',
}

function renderPage() {
  return render(
    <MemoryRouter>
      <BlocksPage />
    </MemoryRouter>,
  )
}

async function openEditModal() {
  renderPage()
  fireEvent.click(screen.getByTitle('Edit block'))
  // Wait for the pool detail fetch that seeds the description field.
  await waitFor(() => {
    expect(mockGet).toHaveBeenCalledWith('/api/v1/pools/42')
  })
  return screen.getByPlaceholderText('Optional description...') as HTMLTextAreaElement
}

describe('BlocksPage edit block description', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseBlocks.mockReturnValue({
      blocks: [block],
      total: 1,
      loading: false,
      error: null,
      fetchBlocks: vi.fn(),
    })
    mockUseAccounts.mockReturnValue({ accounts: [], fetchAccounts: vi.fn() })
    mockPatch.mockResolvedValue({})
    mockGet.mockResolvedValue({ ...block, description: 'original notes' })
  })

  it('loads the current description into the edit form', async () => {
    const textarea = await openEditModal()

    await waitFor(() => {
      expect(textarea.value).toBe('original notes')
    })
  })

  it('sends an empty description when the field is cleared', async () => {
    const textarea = await openEditModal()
    await waitFor(() => expect(textarea.value).toBe('original notes'))

    fireEvent.change(textarea, { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(mockPatch).toHaveBeenCalledWith('/api/v1/pools/42', { description: '' })
    })
  })

  it('sends an edited description', async () => {
    const textarea = await openEditModal()
    await waitFor(() => expect(textarea.value).toBe('original notes'))

    fireEvent.change(textarea, { target: { value: 'updated notes' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(mockPatch).toHaveBeenCalledWith('/api/v1/pools/42', { description: 'updated notes' })
    })
  })

  it('omits description entirely when it was not touched', async () => {
    const textarea = await openEditModal()
    await waitFor(() => expect(textarea.value).toBe('original notes'))

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(mockPatch).toHaveBeenCalledWith('/api/v1/pools/42', {})
    })
  })

  it('does not send a spurious clear when the block never had a description', async () => {
    mockGet.mockResolvedValue({ ...block, description: undefined })

    const textarea = await openEditModal()
    expect(textarea.value).toBe('')

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(mockPatch).toHaveBeenCalledWith('/api/v1/pools/42', {})
    })
  })
})
