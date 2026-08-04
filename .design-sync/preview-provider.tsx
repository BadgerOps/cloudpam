// Preview provider for design-sync cards.
//
// CloudPAM's UI is an application, not a component library: its components read
// react-router-dom and three React contexts. The contexts all have defaults, but
// those defaults represent a signed-out user (hasPermission -> false), which
// makes Sidebar's nav filter to empty and Header render its logged-out state.
// This wrapper supplies a router plus a realistic signed-in admin so preview
// cards show the UI as an operator actually sees it.
//
// The contexts are re-exported so an individual preview can override one
// locally (e.g. ToastContainer supplying its own toasts) without every other
// card inheriting that state.
import { MemoryRouter } from 'react-router-dom'
import { AuthContext } from '../ui/src/hooks/useAuth'
import { ThemeContext } from '../ui/src/hooks/useTheme'
import { ToastContext } from '../ui/src/hooks/useToast'

// Re-exported so previews get the SAME react-router-dom module instance that
// is bundled here. A preview that imports 'react-router-dom' directly gets a
// second copy whose <Routes> can't see this file's <MemoryRouter>, and any
// route-composed preview (Layout, ProtectedRoute) silently renders blank.
export { Routes, Route, Navigate, Outlet } from 'react-router-dom'

export { AuthContext } from '../ui/src/hooks/useAuth'
export { ThemeContext } from '../ui/src/hooks/useTheme'
export { ToastContext } from '../ui/src/hooks/useToast'

export const previewUser = {
  id: 'u-1024',
  username: 'dana.reyes',
  email: 'dana.reyes@example.com',
  display_name: 'Dana Reyes',
  role: 'admin',
  is_active: true,
  created_at: '2026-01-14T09:20:00Z',
  updated_at: '2026-07-02T16:41:00Z',
  last_login_at: '2026-08-01T08:12:00Z',
  failed_login_attempts: 0,
}

export const previewAuth = {
  keyName: null,
  role: 'admin',
  authType: 'session' as const,
  currentUser: previewUser,
  permissions: ['*'],
  hasPermission: () => true,
  isAuthenticated: true,
  authEnabled: true,
  localAuthEnabled: true,
  needsSetup: false,
  authChecked: true,
  loginWithPassword: async () => {},
  logout: () => {},
}

export const previewTheme = {
  mode: 'light' as const,
  resolvedTheme: 'light' as const,
  cycle: () => {},
  toggle: () => {},
}

// Empty by default so cards don't all render floating toasts; ToastContainer's
// own preview supplies toasts via a nested ToastContext.Provider.
export const previewToast = {
  toasts: [],
  showToast: () => {},
}

export function DSPreviewProvider({ children }: { children?: React.ReactNode }) {
  return (
    <MemoryRouter initialEntries={['/pools']}>
      <ThemeContext.Provider value={previewTheme}>
        <AuthContext.Provider value={previewAuth}>
          <ToastContext.Provider value={previewToast}>{children}</ToastContext.Provider>
        </AuthContext.Provider>
      </ThemeContext.Provider>
    </MemoryRouter>
  )
}
