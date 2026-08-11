import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import { RequireAuth } from './components/RequireAuth'
import { TopBar } from './components/TopBar'
import { LoginPage } from './pages/LoginPage'
import { MergeRequestDetailPage } from './pages/MergeRequestDetailPage'
import { RepoDetailPage } from './pages/RepoDetailPage'
import { ReposPage } from './pages/ReposPage'

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <TopBar />
        <main>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route element={<RequireAuth />}>
              <Route path="/" element={<ReposPage />} />
              <Route path="/repos/:repo" element={<RepoDetailPage />} />
              <Route path="/repos/:repo/merge-requests/:id" element={<MergeRequestDetailPage />} />
            </Route>
          </Routes>
        </main>
      </AuthProvider>
    </BrowserRouter>
  )
}
