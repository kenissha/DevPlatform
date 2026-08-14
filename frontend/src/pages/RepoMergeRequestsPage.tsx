import { useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { MergeRequest } from '../api/types'
import { BranchIcon } from '../components/icons'
import { MR_STATUS_BADGE, MR_STATUS_LABELS, formatDate } from '../labels'

export function RepoMergeRequestsPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const [mergeRequests, setMergeRequests] = useState<MergeRequest[] | null>(null)
  const [branches, setBranches] = useState<string[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [title, setTitle] = useState('')
  const [sourceBranch, setSourceBranch] = useState('')
  const [targetBranch, setTargetBranch] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  function reload() {
    Promise.all([api.listMergeRequests(repo), api.listBranches(repo)])
      .then(([mrs, b]) => {
        setMergeRequests(mrs)
        setBranches(b)
        setSourceBranch((cur) => cur || b[0] || '')
        // "main" is the protected default branch, so it's the target
        // essentially every time — preselect it even when the repo has no
        // commits on main yet (targetOptions always offers "main" as a
        // choice in that case, see below, so this must match).
        setTargetBranch((cur) => cur || 'main')
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  useEffect(reload, [repo])

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (!title.trim() || !sourceBranch || !targetBranch) return
    setCreating(true)
    setCreateError(null)
    try {
      await api.createMergeRequest(repo, title.trim(), sourceBranch, targetBranch)
      setTitle('')
      reload()
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : 'Talep oluşturulamadı')
    } finally {
      setCreating(false)
    }
  }

  // The target may legitimately be a branch that doesn't exist yet (the
  // backend creates it on approval — that's how a new repo's protected
  // "main" gets its first commit), so the picker offers a free-text
  // fallback rather than only listing existing branches.
  const targetOptions = branches?.includes('main') ? branches : ['main', ...(branches ?? [])]

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Merge istekleri</h1>
          <p className="page-subtitle">{repo} için inceleme bekleyen değişiklikler</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="card">
        {mergeRequests === null && <p className="empty-state">Yükleniyor...</p>}
        {mergeRequests?.length === 0 && <p className="empty-state">Henüz merge isteği yok.</p>}
        {mergeRequests && mergeRequests.length > 0 && (
          <ul className="row-list">
            {mergeRequests.map((mr) => (
              <li key={mr.id}>
                <div className="row-main">
                  <Link
                    to={`/repos/${encodeURIComponent(repo)}/merge-requests/${mr.id}`}
                    className="row-title"
                  >
                    {mr.title}
                  </Link>
                  <div className="spacer" />
                  <span className={`badge ${MR_STATUS_BADGE[mr.status]}`}>{MR_STATUS_LABELS[mr.status]}</span>
                </div>
                <p className="row-meta">
                  <span className="branch-chip">
                    <BranchIcon />
                    {mr.sourceBranch}
                  </span>
                  <span className="arrow">→</span>
                  <span className="branch-chip">
                    <BranchIcon />
                    {mr.targetBranch}
                  </span>
                  <span>·</span>
                  {mr.author} açtı
                  <span>·</span>
                  {formatDate(mr.createdAt)}
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>

      {branches && branches.length > 0 && (
        <>
          <div className="section-title">
            <h2>Yeni merge isteği</h2>
          </div>
          <div className="card">
            <div className="card-body">
              <form onSubmit={handleCreate}>
                <div className="field">
                  <label htmlFor="mr-title">Başlık</label>
                  <input id="mr-title" value={title} onChange={(e) => setTitle(e.target.value)} required />
                </div>
                <div className="field field-row">
                  <div>
                    <label htmlFor="mr-source">Kaynak branch</label>
                    <select
                      id="mr-source"
                      value={sourceBranch}
                      onChange={(e) => setSourceBranch(e.target.value)}
                    >
                      {branches.map((b) => (
                        <option key={b} value={b}>
                          {b}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div>
                    <label htmlFor="mr-target">Hedef branch</label>
                    <select
                      id="mr-target"
                      value={targetBranch}
                      onChange={(e) => setTargetBranch(e.target.value)}
                    >
                      {targetOptions.map((b) => (
                        <option key={b} value={b}>
                          {b}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>
                <div className="form-actions">
                  <button
                    type="submit"
                    className="btn-primary"
                    disabled={creating || !title.trim() || !sourceBranch || !targetBranch || sourceBranch === targetBranch}
                  >
                    {creating ? 'Oluşturuluyor...' : 'Merge isteği oluştur'}
                  </button>
                  {sourceBranch === targetBranch && (
                    <p className="error">Kaynak ve hedef branch aynı olamaz.</p>
                  )}
                  {createError && <p className="error">{createError}</p>}
                </div>
              </form>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
