import { Link, NavLink, Outlet, useMatch } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { useNotifications } from '../notifications/useNotifications'
import { useRepos } from '../repos/ReposContext'
import { AuditIcon, BellIcon, DeployIcon, KeyIcon, LockIcon, LogoMark, OverviewIcon, RepoIcon } from './icons'
import { RepoTabBar } from './RepoTabBar'

// AppLayout is the persistent chrome every authenticated page renders
// inside: a top bar for identity, a left sidebar for global nav and the
// repo switcher, and — once a repo route is active — a RepoTabBar above
// the page content for that repo's own sub-pages.
export function AppLayout() {
  const { user, logout } = useAuth()
  const { repos } = useRepos()
  const { unreadCount } = useNotifications()
  // useParams would return {} here: a layout route only sees params its own
  // path pattern matched, not its descendants'. useMatch runs against the
  // full location, so the layout can tell which repo the page below it is
  // showing. Both patterns are matched unconditionally — `??` between two
  // useMatch calls would short-circuit and skip a hook.
  const exactMatch = useMatch('/repos/:repo')
  const nestedMatch = useMatch('/repos/:repo/*')
  const repo = exactMatch?.params.repo ?? nestedMatch?.params.repo

  const shownName = user?.displayName || user?.email || user?.subject || '?'
  const initials = shownName.slice(0, 2)

  return (
    <div className="app-shell">
      <header className="topbar">
        <Link to="/" className="brand">
          <LogoMark className="brand-mark" />
          STK Atölye
        </Link>
        <div className="topbar-spacer" />
        {user && (
          <div className="topbar-user">
            <div className="user-identity">
              <span className="avatar">{initials}</span>
              <span className="muted">{shownName}</span>
            </div>
            <span className={`badge ${user.role === 'admin' ? 'badge-warn' : 'badge-neutral'}`}>
              {user.role === 'admin' ? 'Yönetici' : 'Geliştirici'}
            </span>
            <button type="button" onClick={logout} className="btn-ghost">
              Çıkış
            </button>
          </div>
        )}
      </header>

      <div className="app-body">
        <nav className="sidebar">
          <div className="sidebar-group">
            <ul className="nav-list">
              <li>
                <NavLink end to="/" className={navClass}>
                  <OverviewIcon />
                  <span className="nav-label">Panel</span>
                </NavLink>
              </li>
              <li>
                <NavLink end to="/repos" className={navClass}>
                  <RepoIcon />
                  <span className="nav-label">Tüm repolar</span>
                </NavLink>
              </li>
              <li>
                <NavLink end to="/audit" className={navClass}>
                  <AuditIcon />
                  <span className="nav-label">Denetim kaydı</span>
                </NavLink>
              </li>
              <li>
                <NavLink end to="/notifications" className={navClass}>
                  <BellIcon />
                  <span className="nav-label">Bildirimler</span>
                  {unreadCount > 0 && <span className="nav-count">{unreadCount}</span>}
                </NavLink>
              </li>
              <li>
                <NavLink end to="/hesabim" className={navClass}>
                  <KeyIcon />
                  <span className="nav-label">Hesabım</span>
                </NavLink>
              </li>
              {user?.role === 'admin' && (
                <li>
                  <NavLink end to="/access" className={navClass}>
                    <LockIcon />
                    <span className="nav-label">Proje erişimi</span>
                  </NavLink>
                </li>
              )}
              {user?.role === 'admin' && (
                <li>
                  <NavLink end to="/deploy-targets" className={navClass}>
                    <DeployIcon />
                    <span className="nav-label">Deploy hedefleri</span>
                  </NavLink>
                </li>
              )}
            </ul>
          </div>

          <div className="sidebar-group">
            <div className="sidebar-heading">
              Repolar
              {repos && <span className="nav-count">{repos.length}</span>}
            </div>
            {repos === null && <p className="sidebar-empty">Yükleniyor...</p>}
            {repos?.length === 0 && <p className="sidebar-empty">Henüz repo yok.</p>}
            {repos && repos.length > 0 && (
              <ul className="nav-list">
                {repos.map((name) => (
                  <li key={name}>
                    <NavLink to={`/repos/${encodeURIComponent(name)}`} className={navClass}>
                      <RepoIcon />
                      <span className="nav-label">{name}</span>
                    </NavLink>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </nav>

        <main className="main">
          {repo && <RepoTabBar repo={repo} />}
          <Outlet />
        </main>
      </div>
    </div>
  )
}

function navClass({ isActive }: { isActive: boolean }) {
  return isActive ? 'nav-item active' : 'nav-item'
}
