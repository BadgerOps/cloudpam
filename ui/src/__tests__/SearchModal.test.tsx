import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SearchModal from '../components/SearchModal'

const mockUseSearch = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useSearch', () => ({
  useSearch: () => mockUseSearch(),
}))

function renderModal(open: boolean, onClose: () => void) {
  return render(
    <MemoryRouter>
      <SearchModal open={open} onClose={onClose} />
    </MemoryRouter>,
  )
}

describe('SearchModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseSearch.mockReturnValue({
      query: '',
      setQuery: vi.fn(),
      results: null,
      loading: false,
      error: null,
    })
  })

  it('closes when Escape is pressed, as the footer hint advertises', () => {
    const onClose = vi.fn()
    renderModal(true, onClose)

    expect(screen.getByText('Esc')).toBeTruthy()

    fireEvent.keyDown(document, { key: 'Escape' })

    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes when Escape is pressed while the search input has focus', () => {
    const onClose = vi.fn()
    renderModal(true, onClose)

    const input = screen.getByPlaceholderText('Search pools, accounts, or enter an IP address...')
    fireEvent.keyDown(input, { key: 'Escape' })

    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('ignores other keys', () => {
    const onClose = vi.fn()
    renderModal(true, onClose)

    fireEvent.keyDown(document, { key: 'Enter' })
    fireEvent.keyDown(document, { key: 'a' })

    expect(onClose).not.toHaveBeenCalled()
  })

  it('does not listen for Escape while closed', () => {
    const onClose = vi.fn()
    renderModal(false, onClose)

    fireEvent.keyDown(document, { key: 'Escape' })

    expect(onClose).not.toHaveBeenCalled()
  })

  it('removes the key listener when it unmounts', () => {
    const onClose = vi.fn()
    const { unmount } = renderModal(true, onClose)

    unmount()
    fireEvent.keyDown(document, { key: 'Escape' })

    expect(onClose).not.toHaveBeenCalled()
  })

  it('still closes when the backdrop is clicked', () => {
    const onClose = vi.fn()
    renderModal(true, onClose)

    fireEvent.click(screen.getByTestId('search-panel').parentElement as HTMLElement)

    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
