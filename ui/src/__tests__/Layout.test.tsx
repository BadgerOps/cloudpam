import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import Layout from '../components/Layout'

const mockUseAuth = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))

vi.mock('../hooks/useSessionRefresh', () => ({
  useSessionRefresh: () => {},
}))

vi.mock('../components/UpdateBanner', () => ({ default: () => null }))
vi.mock('../components/ToastContainer', () => ({ default: () => null }))
vi.mock('../components/SearchModal', () => ({ default: () => null }))
vi.mock('../components/ImportExportModal', () => ({ default: () => null }))

function renderLayout() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<div>Dashboard page</div>} />
          <Route path="/pools" element={<div>Pools page</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

describe('Layout mobile navigation drawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseAuth.mockReturnValue({
      isAuthenticated: true,
      currentUser: { username: 'admin', display_name: 'Admin' },
      role: 'admin',
      permissions: [],
      hasPermission: () => true,
      logout: vi.fn(),
    })
  })

  it('renders the sidebar off-canvas below md and pinned at md and up', () => {
    renderLayout()

    const sidebar = screen.getByTestId('sidebar')
    expect(sidebar.classList.contains('-translate-x-full')).toBe(true)
    expect(sidebar.classList.contains('translate-x-0')).toBe(false)
    // Always visible on desktop regardless of drawer state.
    expect(sidebar.classList.contains('md:translate-x-0')).toBe(true)
    expect(sidebar.classList.contains('md:static')).toBe(true)
    expect(screen.queryByTestId('sidebar-backdrop')).toBeNull()
  })

  it('wires the header toggle to the sidebar for assistive technology', () => {
    renderLayout()

    const toggle = screen.getByRole('button', { name: 'Open navigation menu' })
    expect(toggle.getAttribute('aria-controls')).toBe('main-sidebar')
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(toggle.classList.contains('md:hidden')).toBe(true)
    expect(document.getElementById('main-sidebar')).toBe(screen.getByTestId('sidebar'))

    fireEvent.click(toggle)

    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(toggle.getAttribute('aria-label')).toBe('Close navigation menu')
  })

  it('opens and closes the drawer from the header toggle', () => {
    renderLayout()

    const sidebar = screen.getByTestId('sidebar')
    const toggle = screen.getByRole('button', { name: 'Open navigation menu' })

    fireEvent.click(toggle)
    expect(sidebar.classList.contains('translate-x-0')).toBe(true)
    expect(sidebar.classList.contains('-translate-x-full')).toBe(false)

    fireEvent.click(toggle)
    expect(sidebar.classList.contains('-translate-x-full')).toBe(true)
  })

  it('closes the drawer when the backdrop is clicked', () => {
    renderLayout()

    const sidebar = screen.getByTestId('sidebar')
    fireEvent.click(screen.getByRole('button', { name: 'Open navigation menu' }))

    const backdrop = screen.getByTestId('sidebar-backdrop')
    expect(backdrop.classList.contains('md:hidden')).toBe(true)

    fireEvent.click(backdrop)

    expect(sidebar.classList.contains('-translate-x-full')).toBe(true)
    expect(screen.queryByTestId('sidebar-backdrop')).toBeNull()
  })

  it('closes the drawer on route change', () => {
    renderLayout()

    const sidebar = screen.getByTestId('sidebar')
    fireEvent.click(screen.getByRole('button', { name: 'Open navigation menu' }))
    expect(sidebar.classList.contains('translate-x-0')).toBe(true)

    fireEvent.click(screen.getByRole('link', { name: 'Address Pools' }))

    expect(screen.getByText('Pools page')).toBeTruthy()
    expect(sidebar.classList.contains('-translate-x-full')).toBe(true)
    expect(screen.queryByTestId('sidebar-backdrop')).toBeNull()
  })

  it('closes the drawer from the in-drawer close button', () => {
    renderLayout()

    const sidebar = screen.getByTestId('sidebar')
    fireEvent.click(screen.getByRole('button', { name: 'Open navigation menu' }))

    // Both the header toggle and the drawer's own button share this label
    // while the drawer is open; the second one lives inside the drawer.
    const closeButtons = screen.getAllByLabelText('Close navigation menu')
    expect(closeButtons).toHaveLength(2)
    fireEvent.click(closeButtons[1])

    expect(sidebar.classList.contains('-translate-x-full')).toBe(true)
  })
})
