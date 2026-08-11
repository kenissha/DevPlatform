import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'

export function RequireAuth() {
  const { status } = useAuth()

  if (status === 'loading') {
    return <div className="page-message">Yükleniyor...</div>
  }
  if (status === 'unauthenticated') {
    return <Navigate to="/login" replace />
  }
  return <Outlet />
}
