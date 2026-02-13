import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'

export default function ProtectedRoute() {
  const { isAuthenticated, authEnabled, localAuthEnabled, authChecked } = useAuth()

  // Wait for the healthz check to finish before deciding
  if (!authChecked) {
    return null
  }

  // Redirect to login when any auth mode is enabled and user isn't authenticated
  if ((authEnabled || localAuthEnabled) && !isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <Outlet />
}
