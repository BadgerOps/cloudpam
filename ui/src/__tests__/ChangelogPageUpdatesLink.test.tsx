import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ChangelogPage from '../pages/ChangelogPage'

const mockUseAuth = vi.hoisted(() => vi.fn())
const mockGetChangelogMarkdown = vi.hoisted(() => vi.fn())
const mockGetSystemInfo = vi.hoisted(() => vi.fn())

vi.mock('../hooks/useAuth', () => ({
  useAuth: () => mockUseAuth(),
}))

vi.mock('../api/client', () => ({
  getChangelogMarkdown: () => mockGetChangelogMarkdown(),
  getSystemInfo: () => mockGetSystemInfo(),
}))

// Mirrors how App.tsx gates /config/updates: settings:read, with admin implied.
function auth(permissions: string[], role = 'viewer') {
  return {
    role,
    permissions,
    hasPermission: (p: string) => role === 'admin' || permissions.includes(p),
  }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <ChangelogPage />
    </MemoryRouter>,
  )
}

async function updatesLink() {
  await waitFor(() => {
    expect(mockGetChangelogMarkdown).toHaveBeenCalled()
  })
  return screen.queryByRole('link', { name: 'Updates' })
}

describe('ChangelogPage Updates link', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetChangelogMarkdown.mockResolvedValue('# Changelog\n\n## [0.9.0] - 2026-03-31\n\n### Added\n- Thing\n')
    mockGetSystemInfo.mockResolvedValue({ version: '0.9.0' })
  })

  it('shows the link to a non-admin who holds settings:read', async () => {
    mockUseAuth.mockReturnValue(auth(['settings:read']))

    renderPage()

    const link = await updatesLink()
    expect(link).not.toBeNull()
    expect(link?.getAttribute('href')).toBe('/config/updates')
  })

  it('shows the link to an admin', async () => {
    mockUseAuth.mockReturnValue(auth([], 'admin'))

    renderPage()

    expect(await updatesLink()).not.toBeNull()
  })

  it('hides the link from a user without settings:read', async () => {
    mockUseAuth.mockReturnValue(auth(['pools:read']))

    renderPage()

    expect(await updatesLink()).toBeNull()
  })
})
