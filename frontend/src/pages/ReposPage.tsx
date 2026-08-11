import { useEffect, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'

export function ReposPage() {
  const { user } = useAuth()
  const [repos, setRepos] = useState<string[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [newRepoName, setNewRepoName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  function reload() {
    api
      .listRepos()
      .then(setRepos)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  useEffect(reload, [])

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
      <h1>Repolar</h1>

      {error && <p className="error">{error}</p>}
      {repos === null && !error && <p className="muted">Yükleniyor...</p>}
      {repos !== null && repos.length === 0 && <p className="muted">Henüz repo yok.</p>}

      {repos !== null && repos.length > 0 && (
        <ul className="repo-list">
          {repos.map((name) => (
            <li key={name}>
              <Link to={`/repos/${encodeURIComponent(name)}`}>{name}</Link>
            </li>
          ))}
        </ul>
      )}

      {user?.role === 'admin' && (
        <form onSubmit={handleCreate} className="inline-form">
          <input
            type="text"
            value={newRepoName}
            onChange={(e) => setNewRepoName(e.target.value)}
            placeholder="yeni-repo-adi"
            pattern="[a-zA-Z0-9_-]+"
            title="Sadece harf, rakam, tire ve alt çizgi"
          />
          <button type="submit" disabled={creating || !newRepoName.trim()}>
            {creating ? 'Oluşturuluyor...' : 'Repo oluştur'}
          </button>
          {createError && <p className="error">{createError}</p>}
        </form>
      )}
    </div>
  )
}
