import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import Header from '../components/Header'
import { ThemeContext, useThemeState } from '../hooks/useTheme'

const mockUseAuth = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))

function ThemedHeader() {
  const theme = useThemeState()
  return (
    <ThemeContext.Provider value={theme}>
      <Header onSearchClick={() => {}} onMenuClick={() => {}} sidebarOpen={false} />
    </ThemeContext.Provider>
  )
}

function renderHeader() {
  return render(
    <MemoryRouter>
      <ThemedHeader />
    </MemoryRouter>,
  )
}

function themeButton() {
  return screen.getByRole('button', { name: /theme/i })
}

describe('header theme control', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    document.documentElement.classList.remove('dark')
    // jsdom has no matchMedia.
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: false,
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
    }))
    mockUseAuth.mockReturnValue({
      isAuthenticated: true,
      currentUser: { username: 'admin', display_name: 'Admin' },
      role: 'admin',
      permissions: [],
      hasPermission: () => true,
      logout: vi.fn(),
    })
  })

  it('cycles through light, dark and system on successive clicks', () => {
    renderHeader()
    expect(themeButton().getAttribute('aria-label')).toMatch(/system/i)

    const visited: Array<{ stored: string | null; dark: boolean; label: string | null }> = []
    for (let i = 0; i < 3; i++) {
      fireEvent.click(themeButton())
      visited.push({
        stored: localStorage.getItem('theme'),
        dark: document.documentElement.classList.contains('dark'),
        label: themeButton().getAttribute('aria-label'),
      })
    }

    expect(visited.map((v) => v.stored)).toEqual(['light', 'dark', 'system'])
    expect(visited.map((v) => v.dark)).toEqual([false, true, false])
    expect(visited[0].label).toMatch(/light/i)
    expect(visited[1].label).toMatch(/dark/i)
    expect(visited[2].label).toMatch(/system/i)
  })

  it('renders no header button without an accessible name', () => {
    renderHeader()
    const header = screen.getByRole('banner')
    const nameless = within(header)
      .getAllByRole('button')
      .filter((b) => !b.getAttribute('aria-label') && !b.textContent?.trim())
    expect(nameless.map((b) => b.outerHTML)).toEqual([])
  })
})
