import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ToastContext, useToastState } from './hooks/useToast'
import Layout from './components/Layout'
import DashboardPage from './pages/DashboardPage'
import PoolsPage from './pages/PoolsPage'
import BlocksPage from './pages/BlocksPage'
import AccountsPage from './pages/AccountsPage'
import AuditPage from './pages/AuditPage'
import DiscoveryPage from './pages/DiscoveryPage'
import SchemaPage from './pages/SchemaPage'

export default function App() {
  const toastState = useToastState()

  return (
    <ToastContext.Provider value={toastState}>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<DashboardPage />} />
            <Route path="pools" element={<PoolsPage />} />
            <Route path="blocks" element={<BlocksPage />} />
            <Route path="accounts" element={<AccountsPage />} />
            <Route path="audit" element={<AuditPage />} />
            <Route path="discovery" element={<DiscoveryPage />} />
            <Route path="schema" element={<SchemaPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ToastContext.Provider>
  )
}
