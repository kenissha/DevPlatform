import { Link, NavLink, Outlet, useMatch } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { useRepos } from '../repos/ReposContext'
import { BranchIcon, LogoMark, MergeIcon, OverviewIcon, RepoIcon, TaskIcon } from './icons'

// AppLayout is the persistent chrome every authenticated page renders
// inside: a top bar for identity and a left sidebar for navigation. The
// sidebar doubles as a repo switcher, and expands into per-repo
// navigation once you're inside one, so the current repo's sections stay
// one click away without a second nav bar.
export function AppLayout() {
  const { user, logout } = useAuth()
  const { repos } = useRepos()
  // useParams would return {} here: a layout route only sees params its own
  // path pattern matched, not its descendants'. useMatch runs against the
  // full location, so the layout can tell which repo the page below it is
  // showing. Both patterns are matched unconditionally — `??` between two
  // useMatch calls would short-circuit and skip a hook.
  const exactMatch = useMatch('/repos/:repo')
  const nestedMatch = useMatch('/repos/:repo/*')
  const repo = exactMatch?.params.repo ?? nestedMatch?.params.repo

  const initials = (user?.email || user?.subject || '?').slice(0, 2)

  return (
    <div className="app-shell">
      <header className="topbar">
        <Link to="/" className="brand">
          <LogoMark className="brand-mark" />
          DevPlatform
        </Link>
        <div className="topbar-spacer" />
        {user && (
          <div className="topbar-user">
            <div className="user-identity">
              <span className="avatar">{initials}</span>
              <span className="muted">{user.email || user.subject}</span>
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
          {repo && (
            <div className="sidebar-group">
              <div className="sidebar-heading">{repo}</div>
              <ul className="nav-list">
                <li>
                  <NavLink end to={`/repos/${encodeURIComponent(repo)}`} className={navClass}>
                    <OverviewIcon />
                    <span className="nav-label">Genel bakış</span>
                  </NavLink>
                </li>
                <li>
                  <NavLink to={`/repos/${encodeURIComponent(repo)}/tasks`} className={navClass}>
                    <TaskIcon />
                    <span className="nav-label">Görevler</span>
                  </NavLink>
                </li>
                <li>
                  <NavLink to={`/repos/${encodeURIComponent(repo)}/merge-requests`} className={navClass}>
                    <MergeIcon />
                    <span className="nav-label">Merge istekleri</span>
                  </NavLink>
                </li>
                <li>
                  <NavLink to={`/repos/${encodeURIComponent(repo)}/branches`} className={navClass}>
                    <BranchIcon />
                    <span className="nav-label">Branch'ler</span>
                  </NavLink>
                </li>
              </ul>
            </div>
          )}

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
          <Outlet />
        </main>
      </div>
    </div>
  )
}

function navClass({ isActive }: { isActive: boolean }) {
  return isActive ? 'nav-item active' : 'nav-item'
}
