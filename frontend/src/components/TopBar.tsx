import { Link } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'

export function TopBar() {
  const { user, logout } = useAuth()

  return (
    <header className="topbar">
      <Link to="/" className="topbar-brand">
        DevPlatform
      </Link>
      {user && (
        <div className="topbar-user">
          <span className="topbar-email">{user.email || user.subject}</span>
          <span className={`role-badge role-${user.role}`}>{user.role}</span>
          <button type="button" onClick={logout} className="link-button">
            Çıkış
          </button>
        </div>
      )}
    </header>
  )
}
