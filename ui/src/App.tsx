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
import AIPlannerPage from './pages/AIPlannerPage'
import ProfilePage from './pages/ProfilePage'
import LogDestinationsPage from './pages/LogDestinationsPage'

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
                <Route index element={<DashboardPage />} />
                <Route path="pools" element={<PoolsPage />} />
                <Route path="blocks" element={<BlocksPage />} />
                <Route path="accounts" element={<AccountsPage />} />
                <Route path="audit" element={<AuditPage />} />
                <Route path="discovery" element={<DiscoveryPage />} />
                <Route path="schema" element={<SchemaPage />} />
                <Route path="recommendations" element={<RecommendationsPage />} />
                <Route path="ai-planner" element={<AIPlannerPage />} />
                <Route path="profile" element={<ProfilePage />} />
                <Route path="config/api-keys" element={<ApiKeysPage />} />
                <Route path="config/users" element={<UsersPage />} />
                <Route path="config/log-destinations" element={<LogDestinationsPage />} />
              </Route>
            </Route>
          </Routes>
        </BrowserRouter>
      </ToastContext.Provider>
    </AuthContext.Provider>
  )
}
