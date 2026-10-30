import type { ComponentType } from 'react'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ToastContext, useToastState } from './hooks/useToast'
import { AuthContext, useAuthState } from './hooks/useAuth'
import Layout from './components/Layout'
import ProtectedRoute from './components/ProtectedRoute'
import LoginPage from './pages/LoginPage'
import SetupPage from './pages/SetupPage'
import DashboardPage from './pages/DashboardPage'
import PoolsPage from './pages/PoolsPage'
import BlocksPage from './pages/BlocksPage'
import AccountsPage from './pages/AccountsPage'
import AuditPage from './pages/AuditPage'
import DiscoveryPage from './pages/DiscoveryPage'
import SchemaPage from './pages/SchemaPage'
import ApiKeysPage from './pages/ApiKeysPage'
import UsersPage from './pages/UsersPage'
import RecommendationsPage from './pages/RecommendationsPage'
import DriftPage from './pages/DriftPage'
import AIPlannerPage from './pages/AIPlannerPage'
import ProfilePage from './pages/ProfilePage'
import LogDestinationsPage from './pages/LogDestinationsPage'
import SecuritySettingsPage from './pages/SecuritySettingsPage'
import UpdatesPage from './pages/UpdatesPage'
import ChangelogPage from './pages/ChangelogPage'
import IdentityPage from './pages/IdentityPage'
import ConfigurationPage from './pages/ConfigurationPage'
import { APP_ROUTES } from './navigation'
import type { RoutePath } from './navigation'

const PAGES: Record<RoutePath, ComponentType> = {
  '/': DashboardPage,
  '/pools': PoolsPage,
  '/blocks': BlocksPage,
  '/accounts': AccountsPage,
  '/discovery': DiscoveryPage,
  '/drift': DriftPage,
  '/audit': AuditPage,
  '/schema': SchemaPage,
  '/recommendations': RecommendationsPage,
  '/ai-planner': AIPlannerPage,
  '/config/api-keys': ApiKeysPage,
  '/changelog': ChangelogPage,
  '/identity': IdentityPage,
  '/config': ConfigurationPage,
  '/config/log-destinations': LogDestinationsPage,
  '/config/updates': UpdatesPage,
  '/config/security': SecuritySettingsPage,
  '/config/users': UsersPage,
  '/profile': ProfilePage,
}

export default function App() {
  const toastState = useToastState()
  const authState = useAuthState()

  return (
    <AuthContext.Provider value={authState}>
      <ToastContext.Provider value={toastState}>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/setup" element={<SetupPage />} />
            <Route element={<ProtectedRoute />}>
              <Route element={<Layout />}>
                {APP_ROUTES.map(({ path }) => {
                  const Page = PAGES[path]
                  return (
                    <Route key={path} element={<ProtectedRoute route={path} />}>
                      <Route path={path} element={<Page />} />
                    </Route>
                  )
                })}
              </Route>
            </Route>
          </Routes>
        </BrowserRouter>
      </ToastContext.Provider>
    </AuthContext.Provider>
  )
}
