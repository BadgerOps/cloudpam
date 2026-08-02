import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SetupPage from '../pages/SetupPage'

const mockUseAuth = vi.hoisted(() => vi.fn())
const mockNavigate = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

function renderPage() {
  return render(
    <MemoryRouter>
      <SetupPage />
    </MemoryRouter>,
  )
}

describe('SetupPage password visibility toggle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseAuth.mockReturnValue({ needsSetup: true, authChecked: true })
  })

  it('exposes an accessible name for assistive technology', () => {
    renderPage()

    expect(screen.getByRole('button', { name: 'Show password' })).toBeTruthy()
  })

  it('updates the accessible name and pressed state when toggled', () => {
    renderPage()

    const toggle = screen.getByRole('button', { name: 'Show password' })
    expect(toggle.getAttribute('aria-pressed')).toBe('false')

    fireEvent.click(toggle)

    const pressed = screen.getByRole('button', { name: 'Hide password' })
    expect(pressed.getAttribute('aria-pressed')).toBe('true')
  })

  it('reveals the password field when toggled on', () => {
    renderPage()

    const password = screen.getByPlaceholderText('Minimum 12 characters') as HTMLInputElement
    expect(password.type).toBe('password')

    fireEvent.click(screen.getByRole('button', { name: 'Show password' }))

    expect(password.type).toBe('text')
  })
})
