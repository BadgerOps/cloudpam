import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'

export default function ProtectedRoute() {
  const { isAuthenticated, authEnabled, authChecked } = useAuth()

  // Wait for the healthz check to finish before deciding
  if (!authChecked) {
    return null
  }

  // Only redirect to login when the backend requires auth and user isn't authenticated
  if (authEnabled && !isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <Outlet />
}
