import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import LoginPage from '../pages/LoginPage'
import type { AuthContextValue } from '../hooks/useAuth'

interface TestProvider {
  id: string
  name: string
}

const mockUseAuth = vi.hoisted(() => vi.fn())
const mockUseOIDCProviders = vi.hoisted(() => vi.fn())
const mockNavigate = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))

vi.mock('../hooks/useOIDCProviders', () => ({
  useOIDCProviders: () => mockUseOIDCProviders(),
}))

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

const loginWithPassword = vi.fn()

function auth(overrides: Partial<AuthContextValue> = {}) {
  return {
    loginWithPassword,
    isAuthenticated: false,
    needsSetup: false,
    authChecked: true,
    localAuthEnabled: true,
    ...overrides,
  }
}

function providers(list: TestProvider[] = [], loading = false) {
  return { providers: list, loading }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <LoginPage />
    </MemoryRouter>,
  )
}

async function signIn(username: string, password: string) {
  fireEvent.change(screen.getByPlaceholderText('admin'), { target: { value: username } })
  const passwordInput = document.querySelector('input[autocomplete="current-password"]') as HTMLInputElement
  fireEvent.change(passwordInput, { target: { value: password } })
  fireEvent.click(screen.getByRole('button', { name: 'Sign In' }))
}

describe('LoginPage auth routing', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseOIDCProviders.mockReturnValue(providers())
  })

  it('renders a loading placeholder until the auth config is known', () => {
    mockUseAuth.mockReturnValue(auth({ authChecked: false }))

    renderPage()

    expect(screen.getByText('Loading...')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Sign In' })).toBeNull()
    expect(mockNavigate).not.toHaveBeenCalled()
  })

  it('redirects to setup on a fresh install', () => {
    mockUseAuth.mockReturnValue(auth({ needsSetup: true }))

    renderPage()

    expect(mockNavigate).toHaveBeenCalledWith('/setup', { replace: true })
  })

  it('redirects an already authenticated user to the dashboard', () => {
    mockUseAuth.mockReturnValue(auth({ isAuthenticated: true }))

    renderPage()

    expect(mockNavigate).toHaveBeenCalledWith('/', { replace: true })
  })

  it('prefers the setup redirect over the authenticated redirect', () => {
    mockUseAuth.mockReturnValue(auth({ needsSetup: true, isAuthenticated: true }))

    renderPage()

    expect(mockNavigate).toHaveBeenCalledWith('/setup', { replace: true })
    expect(mockNavigate).not.toHaveBeenCalledWith('/', { replace: true })
  })

  it('stays put when unauthenticated and setup is complete', () => {
    mockUseAuth.mockReturnValue(auth())

    renderPage()

    expect(mockNavigate).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Sign In' })).toBeTruthy()
  })
})

describe('LoginPage sign-in modes', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseAuth.mockReturnValue(auth())
    mockUseOIDCProviders.mockReturnValue(providers())
  })

  it('shows the password form when local auth is enabled', () => {
    renderPage()

    expect(screen.getByPlaceholderText('admin')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Sign In' })).toBeTruthy()
  })

  it('hides the password form when local auth is disabled', () => {
    mockUseAuth.mockReturnValue(auth({ localAuthEnabled: false }))
    mockUseOIDCProviders.mockReturnValue(
      providers([{ id: 'okta', name: 'Okta' }]),
    )

    renderPage()

    expect(screen.queryByRole('button', { name: 'Sign In' })).toBeNull()
    expect(screen.getByText('Sign in with Okta')).toBeTruthy()
    expect(screen.getByText('Local password sign-in is disabled for this deployment.')).toBeTruthy()
  })

  it('renders an SSO link per enabled provider alongside local auth', () => {
    mockUseOIDCProviders.mockReturnValue(
      providers([
        { id: 'okta', name: 'Okta' },
        { id: 'entra id', name: 'Entra ID' },
      ]),
    )

    renderPage()

    expect(screen.getByRole('button', { name: 'Sign In' })).toBeTruthy()
    expect(screen.getByText('or')).toBeTruthy()

    const okta = screen.getByText('Sign in with Okta').closest('a') as HTMLAnchorElement
    expect(okta.getAttribute('href')).toBe('/api/v1/auth/oidc/login?provider_id=okta')

    const entra = screen.getByText('Sign in with Entra ID').closest('a') as HTMLAnchorElement
    expect(entra.getAttribute('href')).toBe('/api/v1/auth/oidc/login?provider_id=entra%20id')
  })

  it('warns when neither local auth nor SSO is available', () => {
    mockUseAuth.mockReturnValue(auth({ localAuthEnabled: false }))

    renderPage()

    expect(
      screen.getByText('Local authentication is disabled and no SSO providers are enabled.'),
    ).toBeTruthy()
  })

  it('does not warn while providers are still loading', () => {
    mockUseAuth.mockReturnValue(auth({ localAuthEnabled: false }))
    mockUseOIDCProviders.mockReturnValue(providers([], true))

    renderPage()

    expect(
      screen.queryByText('Local authentication is disabled and no SSO providers are enabled.'),
    ).toBeNull()
  })

  it('submits credentials and navigates home on success', async () => {
    loginWithPassword.mockResolvedValue(undefined)

    renderPage()
    await signIn('  admin  ', 'correct-horse-battery')

    await waitFor(() => {
      expect(loginWithPassword).toHaveBeenCalledWith('admin', 'correct-horse-battery')
    })
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/')
    })
  })

  it('shows the server error and stays on the page when sign-in fails', async () => {
    loginWithPassword.mockRejectedValue(new Error('invalid credentials'))

    renderPage()
    await signIn('admin', 'wrong-password')

    expect(await screen.findByText('invalid credentials')).toBeTruthy()
    expect(mockNavigate).not.toHaveBeenCalled()
  })

  it('keeps the submit button disabled until both fields are filled', () => {
    renderPage()

    const submit = screen.getByRole('button', { name: 'Sign In' }) as HTMLButtonElement
    expect(submit.disabled).toBe(true)

    fireEvent.change(screen.getByPlaceholderText('admin'), { target: { value: 'admin' } })
    expect(submit.disabled).toBe(true)

    const passwordInput = document.querySelector('input[autocomplete="current-password"]') as HTMLInputElement
    fireEvent.change(passwordInput, { target: { value: 'secret' } })
    expect(submit.disabled).toBe(false)
  })

  it('toggles password visibility with an accessible control', () => {
    renderPage()

    const passwordInput = document.querySelector('input[autocomplete="current-password"]') as HTMLInputElement
    expect(passwordInput.type).toBe('password')

    fireEvent.click(screen.getByRole('button', { name: 'Show password' }))
    expect(passwordInput.type).toBe('text')

    fireEvent.click(screen.getByRole('button', { name: 'Hide password' }))
    expect(passwordInput.type).toBe('password')
  })
})
