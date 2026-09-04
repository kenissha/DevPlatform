import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { BranchPreview, Commit } from '../api/types'
import { BranchIcon } from '../components/icons'
import { formatDate } from '../labels'

// The branch's own page — GitHub's branch view, scaled to what this
// platform actually needs: commits this branch adds on top of main, the
// resulting diff, and (the whole point) a way to tell the Yönetici
// "işim bitti, incele" from right here instead of a separate form with
// source/target dropdowns. See backend/internal/mergerequest's package
// doc comment for why Onayla itself performs no git operation — the
// Yönetici reviews and merges for real, outside this panel, using this
// page (and the İnceleme İsteği it opens) as the review record.
export function RepoBranchDetailPage() {
  // "*" (the splat param), not a named ":branch" — see App.tsx's route
  // comment: branch names may contain slashes (e.g.
  // "feature/hakem-raporlari"), which a named param would truncate at.
  const { repo = '', '*': branch = '' } = useParams<{ repo: string; '*': string }>()
  const navigate = useNavigate()
  const [commits, setCommits] = useState<Commit[] | null>(null)
  const [preview, setPreview] = useState<BranchPreview | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [title, setTitle] = useState('')
  const [requesting, setRequesting] = useState(false)
  const [requestError, setRequestError] = useState<string | null>(null)

  function reload() {
    setError(null)
    Promise.all([api.listBranchCommits(repo, branch), api.getBranchPreview(repo, branch)])
      .then(([c, p]) => {
        setCommits(c)
        setPreview(p)
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  useEffect(reload, [repo, branch])
  useEffect(() => {
    setTitle(branch)
  }, [branch])

  async function requestReview(e: FormEvent) {
    e.preventDefault()
    if (!title.trim()) return
    setRequesting(true)
    setRequestError(null)
    try {
      const mr = await api.createMergeRequest(repo, title.trim(), branch, 'main')
      navigate(`/repos/${encodeURIComponent(repo)}/merge-requests/${mr.id}`)
    } catch (err) {
      setRequestError(err instanceof ApiError ? err.message : 'İnceleme isteği açılamadı')
    } finally {
      setRequesting(false)
    }
  }

  if (error) {
    return (
      <div className="page">
        <p>
          <Link to={`/repos/${encodeURIComponent(repo)}/branches`}>← Branch'ler</Link>
        </p>
        <p className="error">{error}</p>
      </div>
    )
  }

  const isMain = branch === 'main'
  const totals = preview?.diff.stats.reduce(
    (acc, s) => ({ add: acc.add + s.addition, del: acc.del + s.deletion }),
    { add: 0, del: 0 },
  )

  return (
    <div className="page">
      <p>
        <Link to={`/repos/${encodeURIComponent(repo)}/branches`}>← Branch'ler</Link>
      </p>

      <div className="page-header">
        <div className="page-title-group">
          <h1>
            <BranchIcon /> {branch}
          </h1>
          <p className="page-subtitle">
            {isMain ? 'Korumalı — doğrudan push kapalı' : `${repo} · main'e göre karşılaştırma`}
          </p>
        </div>
      </div>

      {!isMain && (
        <div className="card">
          <div className="card-body">
            {preview?.openRequest && (
              <p className="muted" style={{ fontSize: 13 }}>
                Bu branch için zaten <strong>açık</strong> bir inceleme isteğin var —{' '}
                <Link to={`/repos/${encodeURIComponent(repo)}/merge-requests/${preview.openRequest.id}`}>
                  {preview.openRequest.title}
                </Link>
              </p>
            )}

            {!preview?.openRequest && preview?.lastRejected && (
              <>
                <p className="error" style={{ fontSize: 13 }}>
                  Son isteğin reddedildi
                  {preview.lastRejected.note ? `: ${preview.lastRejected.note}` : '.'}
                </p>
                <form onSubmit={requestReview} className="stacked-form">
                  <div className="field">
                    <label htmlFor="req-title">Başlık</label>
                    <input id="req-title" value={title} onChange={(e) => setTitle(e.target.value)} required />
                  </div>
                  <div className="form-actions">
                    <button type="submit" className="btn-primary" disabled={requesting || !title.trim()}>
                      {requesting ? 'Açılıyor...' : 'Tekrar iste'}
                    </button>
                    {requestError && <p className="error">{requestError}</p>}
                  </div>
                </form>
              </>
            )}

            {!preview?.openRequest && !preview?.lastRejected && (
              <form onSubmit={requestReview} className="stacked-form">
                <div className="field">
                  <label htmlFor="req-title">Başlık</label>
                  <input id="req-title" value={title} onChange={(e) => setTitle(e.target.value)} required />
                </div>
                <div className="form-actions">
                  <button type="submit" className="btn-primary" disabled={requesting || !title.trim()}>
                    {requesting ? 'Açılıyor...' : 'İşim bitti, incele'}
                  </button>
                  {requestError && <p className="error">{requestError}</p>}
                </div>
              </form>
            )}
          </div>
        </div>
      )}

      <div className="section-title">
        <h2>Commit'ler</h2>
        {commits && commits.length > 0 && (
          <span className="muted mono" style={{ fontSize: 12 }}>
            main'e göre {commits.length} commit önde
          </span>
        )}
      </div>
      <div className="card">
        {commits === null && <p className="empty-state">Yükleniyor...</p>}
        {commits?.length === 0 && <p className="empty-state">main'e göre farkı yok.</p>}
        {commits && commits.length > 0 && (
          <ul className="row-list">
            {commits.map((c) => (
              <li key={c.hash}>
                <div className="row-main">
                  <span className="commit-msg">{firstLine(c.message)}</span>
                  <div className="spacer" />
                  <span className="commit-hash">{c.shortHash}</span>
                </div>
                <p className="row-meta">
                  {c.authorName}
                  <span>·</span>
                  {formatDate(c.when)}
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>

      {preview && preview.diff.stats.length > 0 && totals && (
        <>
          <div className="section-title">
            <h2>Değişiklikler</h2>
            <span className="muted mono" style={{ fontSize: 12 }}>
              {preview.diff.stats.length} dosya <span className="add">+{totals.add}</span>{' '}
              <span className="del">−{totals.del}</span>
            </span>
          </div>
          <div className="card">
            <div className="diff-stats">
              {preview.diff.stats.map((s) => (
                <span key={s.name} className="diff-stat">
                  {s.name}
                  <span className="add">+{s.addition}</span>
                  <span className="del">−{s.deletion}</span>
                </span>
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  )
}

// Commit messages carry their full body; a list row shows the subject only.
function firstLine(message: string): string {
  return message.split('\n')[0]
}
