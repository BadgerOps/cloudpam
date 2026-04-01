import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'

interface ProtectedRouteProps {
  requiredRole?: string
}

export default function ProtectedRoute({ requiredRole }: ProtectedRouteProps) {
  const { isAuthenticated, authEnabled, localAuthEnabled, needsSetup, authChecked, role } = useAuth()

  // Wait for the healthz check to finish before deciding
  if (!authChecked) {
    return null
  }

  // Redirect to setup wizard on fresh install
  if (needsSetup) {
    return <Navigate to="/setup" replace />
  }

  // Redirect to login when any auth mode is enabled and user isn't authenticated
  if ((authEnabled || localAuthEnabled) && !isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  if (requiredRole && role !== requiredRole) {
    return <Navigate to="/" replace />
  }

  return <Outlet />
}
