import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ToastContext, useToastState } from './hooks/useToast'
import { AuthContext, useAuthState } from './hooks/useAuth'
import Layout from './components/Layout'
import ProtectedRoute from './components/ProtectedRoute'
import LoginPage from './pages/LoginPage'
import DashboardPage from './pages/DashboardPage'
import PoolsPage from './pages/PoolsPage'
import BlocksPage from './pages/BlocksPage'
import AccountsPage from './pages/AccountsPage'
import AuditPage from './pages/AuditPage'
import DiscoveryPage from './pages/DiscoveryPage'
import SchemaPage from './pages/SchemaPage'
import ApiKeysPage from './pages/ApiKeysPage'
import UsersPage from './pages/UsersPage'

export default function App() {
  const toastState = useToastState()
  const authState = useAuthState()

  return (
    <AuthContext.Provider value={authState}>
      <ToastContext.Provider value={toastState}>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route element={<ProtectedRoute />}>
              <Route element={<Layout />}>
                <Route index element={<DashboardPage />} />
                <Route path="pools" element={<PoolsPage />} />
                <Route path="blocks" element={<BlocksPage />} />
                <Route path="accounts" element={<AccountsPage />} />
                <Route path="audit" element={<AuditPage />} />
                <Route path="discovery" element={<DiscoveryPage />} />
                <Route path="schema" element={<SchemaPage />} />
                <Route path="settings/api-keys" element={<ApiKeysPage />} />
                <Route path="settings/users" element={<UsersPage />} />
              </Route>
            </Route>
          </Routes>
        </BrowserRouter>
      </ToastContext.Provider>
    </AuthContext.Provider>
  )
}
