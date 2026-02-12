import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'

export default function ProtectedRoute() {
  const { isAuthenticated, authEnabled } = useAuth()

  if (authEnabled && !isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <Outlet />
}
