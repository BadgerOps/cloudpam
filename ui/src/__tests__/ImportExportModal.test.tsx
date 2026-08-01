import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ImportExportModal from '../components/ImportExportModal'
import { post, postRaw } from '../api/client'

vi.mock('../api/client', () => ({
  post: vi.fn(),
  postRaw: vi.fn(),
}))

const showToast = vi.fn()
vi.mock('../hooks/useToast', () => ({
  useToast: () => ({ showToast }),
}))

const mockPost = vi.mocked(post)
const mockPostRaw = vi.mocked(postRaw)

const csv = 'key,name\nacct-a,Account A\nacct-b,Account B\n'

function selectCSVFile() {
  const file = new File([csv], 'accounts.csv', { type: 'text/csv' })
  // jsdom's Blob implementation has no text(); browsers do.
  Object.defineProperty(file, 'text', { value: () => Promise.resolve(csv) })
  const input = document.querySelector('input[type="file"]') as HTMLInputElement
  fireEvent.change(input, { target: { files: [file] } })
}

describe('ImportExportModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('posts the CSV file contents as a raw body, not JSON', async () => {
    mockPostRaw.mockResolvedValue({ created: 2, skipped: 0, errors: [] })
    render(<ImportExportModal open onClose={() => {}} />)

    selectCSVFile()
    fireEvent.click(screen.getByRole('button', { name: 'Import' }))

    await waitFor(() => {
      expect(mockPostRaw).toHaveBeenCalledWith('/api/v1/import/accounts', csv)
    })
    expect(mockPost).not.toHaveBeenCalled()

    const [, body] = mockPostRaw.mock.calls[0]
    expect(body).not.toContain('\\n')
    expect(body).toBe(csv)
  })

  it('targets the selected import type', async () => {
    mockPostRaw.mockResolvedValue({ created: 0, skipped: 0, errors: [] })
    render(<ImportExportModal open onClose={() => {}} />)

    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'pools' } })
    selectCSVFile()
    fireEvent.click(screen.getByRole('button', { name: 'Import' }))

    await waitFor(() => {
      expect(mockPostRaw).toHaveBeenCalledWith('/api/v1/import/pools', csv)
    })
  })
})
