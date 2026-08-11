import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { RepoIcon } from '../components/icons'
import { useRepos } from '../repos/ReposContext'

export function ReposPage() {
  const { user } = useAuth()
  const { repos, error, reload } = useRepos()
  const [newRepoName, setNewRepoName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (!newRepoName.trim()) return
    setCreating(true)
    setCreateError(null)
    try {
      await api.createRepo(newRepoName.trim())
      setNewRepoName('')
      reload()
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : 'Repo oluşturulamadı')
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Repolar</h1>
          <p className="page-subtitle">Platform üzerinde barındırılan tüm depolar</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="card">
        {repos === null && <p className="empty-state">Yükleniyor...</p>}
        {repos?.length === 0 && (
          <p className="empty-state">
            Henüz repo yok.
            {user?.role === 'admin' ? ' Aşağıdan ilk repoyu oluşturabilirsiniz.' : ''}
          </p>
        )}
        {repos && repos.length > 0 && (
          <ul className="row-list">
            {repos.map((name) => (
              <li key={name}>
                <div className="row-main">
                  <RepoIcon className="muted" />
                  <Link to={`/repos/${encodeURIComponent(name)}`} className="row-title">
                    {name}
                  </Link>
                </div>
                <p className="row-meta">
                  <code>
                    git clone http://&lt;sunucu&gt;/git/{name}.git
                  </code>
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>

      {user?.role === 'admin' && (
        <>
          <div className="section-title">
            <h2>Yeni repo</h2>
          </div>
          <div className="card">
            <div className="card-body">
              <form onSubmit={handleCreate} className="inline-form">
                <input
                  type="text"
                  value={newRepoName}
                  onChange={(e) => setNewRepoName(e.target.value)}
                  placeholder="yeni-repo-adi"
                  pattern="[a-zA-Z0-9_-]+"
                  title="Sadece harf, rakam, tire ve alt çizgi"
                  aria-label="Yeni repo adı"
                />
                <button type="submit" className="btn-primary" disabled={creating || !newRepoName.trim()}>
                  {creating ? 'Oluşturuluyor...' : 'Oluştur'}
                </button>
              </form>
              {createError && <p className="error" style={{ marginTop: 10 }}>{createError}</p>}
            </div>
          </div>
        </>
      )}
    </div>
  )
}
