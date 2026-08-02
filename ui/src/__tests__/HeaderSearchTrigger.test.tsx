import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import Layout from '../components/Layout'
import Header from '../components/Header'

const mockUseAuth = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))

vi.mock('../hooks/useSessionRefresh', () => ({
  useSessionRefresh: () => {},
}))

vi.mock('../components/UpdateBanner', () => ({ default: () => null }))
vi.mock('../components/ToastContainer', () => ({ default: () => null }))
vi.mock('../components/ImportExportModal', () => ({ default: () => null }))

// A stub search modal that counts how many times it is mounted, so a
// double-fired open callback is observable.
const openCount = vi.hoisted(() => ({ value: 0 }))
vi.mock('../components/SearchModal', () => ({
  default: ({ open }: { open: boolean }) => {
    if (open) openCount.value++
    return open ? <div data-testid="search-modal">search</div> : null
  },
}))

function renderLayout() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route element={<Layout />}>
          <Route
            path="/"
            element={
              <form>
                <input aria-label="pool name" />
                <textarea aria-label="notes" />
              </form>
            }
          />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

describe('header search trigger', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    openCount.value = 0
    mockUseAuth.mockReturnValue({
      isAuthenticated: true,
      currentUser: { username: 'admin', display_name: 'Admin' },
      role: 'admin',
      permissions: [],
      hasPermission: () => true,
      logout: vi.fn(),
    })
  })

  function renderHeader(onSearchClick: () => void) {
    return render(
      <MemoryRouter>
        <Header onSearchClick={onSearchClick} onMenuClick={() => {}} sidebarOpen={false} />
      </MemoryRouter>,
    )
  }

  // Regression: the trigger fired on both focus and click, so a single mouse
  // click ran the open callback twice. The callback is asserted directly —
  // React would collapse the duplicate state update, hiding the double call.
  it('fires the open callback exactly once per mouse click', () => {
    const onSearchClick = vi.fn()
    renderHeader(onSearchClick)
    const trigger = screen.getByLabelText('Search pools, CIDRs, accounts')

    // A real click focuses the element first, then dispatches click.
    fireEvent.focus(trigger)
    fireEvent.click(trigger)

    expect(onSearchClick).toHaveBeenCalledTimes(1)
  })

  it('stays keyboard accessible via Enter and Space', () => {
    const onSearchClick = vi.fn()
    renderHeader(onSearchClick)
    const trigger = screen.getByLabelText('Search pools, CIDRs, accounts')

    fireEvent.keyDown(trigger, { key: 'Enter' })
    expect(onSearchClick).toHaveBeenCalledTimes(1)

    fireEvent.keyDown(trigger, { key: ' ' })
    expect(onSearchClick).toHaveBeenCalledTimes(2)
  })

  it('opens the modal through the layout on click', () => {
    renderLayout()
    fireEvent.click(screen.getByLabelText('Search pools, CIDRs, accounts'))
    expect(screen.getByTestId('search-modal')).toBeTruthy()
  })
})

describe('global search shortcut', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    openCount.value = 0
    mockUseAuth.mockReturnValue({
      isAuthenticated: true,
      currentUser: { username: 'admin', display_name: 'Admin' },
      role: 'admin',
      permissions: [],
      hasPermission: () => true,
      logout: vi.fn(),
    })
  })

  it('opens search on Ctrl+K outside a form field', () => {
    renderLayout()
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    expect(screen.queryByTestId('search-modal')).toBeTruthy()
  })

  // Regression: Ctrl/Cmd+K is a text-editing shortcut, and the global handler
  // swallowed it while the user was typing into a routed form.
  it('does not hijack Ctrl/Cmd+K while typing in a text input', () => {
    renderLayout()
    const field = screen.getByLabelText('pool name')
    field.focus()

    fireEvent.keyDown(field, { key: 'k', ctrlKey: true, bubbles: true })
    expect(screen.queryByTestId('search-modal')).toBeNull()

    fireEvent.keyDown(field, { key: 'k', metaKey: true, bubbles: true })
    expect(screen.queryByTestId('search-modal')).toBeNull()
  })

  it('does not hijack Ctrl+K while typing in a textarea', () => {
    renderLayout()
    const notes = screen.getByLabelText('notes')
    notes.focus()

    fireEvent.keyDown(notes, { key: 'k', ctrlKey: true, bubbles: true })
    expect(screen.queryByTestId('search-modal')).toBeNull()
  })

  // The header trigger is a read-only input acting as a button, so the
  // shortcut must still work when it happens to hold focus.
  it('still opens search when the read-only trigger has focus', () => {
    renderLayout()
    const trigger = screen.getByLabelText('Search pools, CIDRs, accounts')

    fireEvent.keyDown(trigger, { key: 'k', ctrlKey: true, bubbles: true })
    expect(screen.queryByTestId('search-modal')).toBeTruthy()
  })
})
