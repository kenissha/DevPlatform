import { NavLink } from 'react-router-dom'
import { BranchIcon, ChartIcon, DeployIcon, MergeIcon, OverviewIcon, TaskIcon } from './icons'

function tabClass({ isActive }: { isActive: boolean }) {
  return isActive ? 'repo-tab active' : 'repo-tab'
}

// RepoTabBar is the GitHub-style horizontal nav shown above a repo's
// sub-pages once AppLayout has determined which repo the current route is
// showing (see AppLayout's useMatch-based `repo` detection). It replaces
// the old scheme of mixing this nav into the global sidebar: those two nav
// levels (cross-repo vs. inside-one-repo) were previously in the same
// list, which is what made "where am I" hard to tell at a glance.
export function RepoTabBar({ repo }: { repo: string }) {
  const encoded = encodeURIComponent(repo)
  return (
    <nav className="repo-tabbar">
      <div className="repo-tabbar-title">{repo}</div>
      <ul className="repo-tab-list">
        <li>
          <NavLink end to={`/repos/${encoded}`} className={tabClass}>
            <OverviewIcon />
            <span>Genel bakış</span>
          </NavLink>
        </li>
        <li>
          <NavLink to={`/repos/${encoded}/tasks`} className={tabClass}>
            <TaskIcon />
            <span>Görevler</span>
          </NavLink>
        </li>
        <li>
          <NavLink to={`/repos/${encoded}/merge-requests`} className={tabClass}>
            <MergeIcon />
            <span>İnceleme istekleri</span>
          </NavLink>
        </li>
        <li>
          <NavLink to={`/repos/${encoded}/branches`} className={tabClass}>
            <BranchIcon />
            <span>Branch'ler</span>
          </NavLink>
        </li>
        <li>
          <NavLink to={`/repos/${encoded}/insights`} className={tabClass}>
            <ChartIcon />
            <span>İstatistikler</span>
          </NavLink>
        </li>
        <li>
          <NavLink to={`/repos/${encoded}/deployments`} className={tabClass}>
            <DeployIcon />
            <span>Deploy</span>
          </NavLink>
        </li>
      </ul>
    </nav>
  )
}
