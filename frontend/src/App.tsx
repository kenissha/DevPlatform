import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import { AppLayout } from './components/AppLayout'
import { RequireAuth } from './components/RequireAuth'
import { NotificationsProvider } from './notifications/NotificationsContext'
import { AccessPage } from './pages/AccessPage'
import { AuditPage } from './pages/AuditPage'
import { DashboardPage } from './pages/DashboardPage'
import { DeployTargetsPage } from './pages/DeployTargetsPage'
import { HesabimPage } from './pages/HesabimPage'
import { LoginPage } from './pages/LoginPage'
import { MergeRequestDetailPage } from './pages/MergeRequestDetailPage'
import { NotificationsPage } from './pages/NotificationsPage'
import { RepoBranchesPage } from './pages/RepoBranchesPage'
import { RepoDeploymentsPage } from './pages/RepoDeploymentsPage'
import { RepoInsightsPage } from './pages/RepoInsightsPage'
import { RepoMergeRequestsPage } from './pages/RepoMergeRequestsPage'
import { RepoOverviewPage } from './pages/RepoOverviewPage'
import { RepoTasksPage } from './pages/RepoTasksPage'
import { ReposPage } from './pages/ReposPage'
import { ReposProvider } from './repos/ReposContext'

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          {/* Everything below the guard renders inside AppLayout's chrome.
              ReposProvider/NotificationsProvider sit inside RequireAuth so
              they only fetch once there's a token to fetch with. */}
          <Route element={<RequireAuth />}>
            <Route
              element={
                <ReposProvider>
                  <NotificationsProvider>
                    <AppLayout />
                  </NotificationsProvider>
                </ReposProvider>
              }
            >
              <Route path="/" element={<DashboardPage />} />
              <Route path="/repos" element={<ReposPage />} />
              <Route path="/audit" element={<AuditPage />} />
              <Route path="/access" element={<AccessPage />} />
              <Route path="/deploy-targets" element={<DeployTargetsPage />} />
              <Route path="/hesabim" element={<HesabimPage />} />
              <Route path="/notifications" element={<NotificationsPage />} />
              <Route path="/repos/:repo" element={<RepoOverviewPage />} />
              <Route path="/repos/:repo/tasks" element={<RepoTasksPage />} />
              <Route path="/repos/:repo/branches" element={<RepoBranchesPage />} />
              <Route path="/repos/:repo/insights" element={<RepoInsightsPage />} />
              <Route path="/repos/:repo/deployments" element={<RepoDeploymentsPage />} />
              <Route path="/repos/:repo/merge-requests" element={<RepoMergeRequestsPage />} />
              <Route path="/repos/:repo/merge-requests/:id" element={<MergeRequestDetailPage />} />
            </Route>
          </Route>
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  )
}
