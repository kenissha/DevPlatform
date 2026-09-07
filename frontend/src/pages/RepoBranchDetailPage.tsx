import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { BranchPreview, Commit } from '../api/types'
import { BranchIcon, CheckIcon, LockIcon, MergeIcon } from '../components/icons'
import { formatRelative, MR_STATUS_BADGE, MR_STATUS_LABELS, formatDate } from '../labels'

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
  const [commits, setCommits] = useState<Commit[] | null>(null)
  const [preview, setPreview] = useState<BranchPreview | null>(null)
  const [error, setError] = useState<string | null>(null)

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
  const lastCommit = commits?.[0]

  return (
    <div className="page">
      <p>
        <Link to={`/repos/${encodeURIComponent(repo)}/branches`}>← Branch'ler</Link>
      </p>

      <div className="branch-hero">
        <h1 className="branch-hero-title">
          <BranchIcon />
          {branch}
          {isMain && (
            <span className="badge badge-warn">
              <LockIcon /> Korumalı
            </span>
          )}
        </h1>
        <p className="page-subtitle">
          {isMain ? `${repo} · doğrudan push kapalı` : `${repo} · main'e göre karşılaştırma`}
        </p>

        {!isMain && (
          <div className="branch-facts">
            <span>
              <strong>{commits?.length ?? '—'}</strong> commit önde
            </span>
            <span>
              <strong>{preview?.diff.stats.length ?? '—'}</strong> dosya değişti
            </span>
            {totals && (totals.add > 0 || totals.del > 0) && (
              <span className="mono">
                <span className="add">+{totals.add}</span> <span className="del">−{totals.del}</span>
              </span>
            )}
            {lastCommit && <span>son hareket {formatRelative(lastCommit.when)}</span>}
          </div>
        )}
      </div>

      {!isMain && (
        <ReviewPanel repo={repo} branch={branch} preview={preview} onOpened={reload} />
      )}

      <div className="section-title">
        <h2>Commit'ler</h2>
        {commits && commits.length > 0 && (
          <span className="badge badge-neutral">{commits.length}</span>
        )}
      </div>
      <div className="card">
        {commits === null && <p className="empty-state">Yükleniyor...</p>}
        {commits?.length === 0 && <p className="empty-state">main'e göre farkı yok.</p>}
        {commits && commits.length > 0 && (
          <ul className="row-list scroll-list">
            {commits.map((c) => (
              <li key={c.hash}>
                <div className="row-main">
                  <span className="row-title">{firstLine(c.message)}</span>
                  <div className="spacer" />
                  <code className="commit-hash">{c.shortHash}</code>
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
            <h2>Değişen dosyalar</h2>
            <span className="muted mono" style={{ fontSize: 12 }}>
              {preview.diff.stats.length} dosya
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

// ReviewPanel is this page's reason to exist: handing the branch over for
// review. It has three states — already handed over, sent back with a
// reason, or not yet asked — and each one leads somewhere, so a branch is
// never a dead end.
function ReviewPanel({
  repo,
  branch,
  preview,
  onOpened,
}: {
  repo: string
  branch: string
  preview: BranchPreview | null
  onOpened: () => void
}) {
  if (!preview) return null

  if (preview.openRequest) {
    const mr = preview.openRequest
    return (
      <div className="review-panel is-open">
        <div className="review-panel-head">
          <MergeIcon />
          <div>
            <p className="review-panel-title">İnceleme bekliyor</p>
            <p className="review-panel-sub">
              Bu branch için açık bir isteğin var — yönetici bakınca haberin olacak.
            </p>
          </div>
          <span className={`badge ${MR_STATUS_BADGE[mr.status]}`}>{MR_STATUS_LABELS[mr.status]}</span>
        </div>
        <Link
          to={`/repos/${encodeURIComponent(repo)}/merge-requests/${mr.id}`}
          className="btn-secondary"
        >
          {mr.title} →
        </Link>
      </div>
    )
  }

  return (
    <div className={preview.lastRejected ? 'review-panel is-rejected' : 'review-panel'}>
      {preview.lastRejected && (
        <div className="review-panel-head">
          <div>
            <p className="review-panel-title">Son isteğin geri gönderildi</p>
            {preview.lastRejected.note ? (
              <p className="review-panel-note">“{preview.lastRejected.note}”</p>
            ) : (
              <p className="review-panel-sub">Not bırakılmamış.</p>
            )}
          </div>
        </div>
      )}
      <RequestForm
        repo={repo}
        branch={branch}
        again={Boolean(preview.lastRejected)}
        onOpened={onOpened}
      />
    </div>
  )
}

function RequestForm({
  repo,
  branch,
  again,
  onOpened,
}: {
  repo: string
  branch: string
  again: boolean
  onOpened: () => void
}) {
  const navigate = useNavigate()
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [requesting, setRequesting] = useState(false)
  const [requestError, setRequestError] = useState<string | null>(null)

  // Prefilled with the branch name so the field is never empty, but the
  // branch name is a poor title and people should say what they did — the
  // label and placeholder below push for that.
  useEffect(() => {
    setTitle(branch)
  }, [branch])

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!title.trim()) return
    setRequesting(true)
    setRequestError(null)
    try {
      const mr = await api.createMergeRequest(repo, title.trim(), description.trim(), branch, 'main')
      onOpened()
      navigate(`/repos/${encodeURIComponent(repo)}/merge-requests/${mr.id}`)
    } catch (err) {
      setRequestError(err instanceof ApiError ? err.message : 'İnceleme isteği açılamadı')
    } finally {
      setRequesting(false)
    }
  }

  return (
    <form onSubmit={submit} className="review-form">
      {!again && (
        <div className="review-panel-head">
          <CheckIcon />
          <div>
            <p className="review-panel-title">İşin bitti mi?</p>
            <p className="review-panel-sub">
              Bu branch'i incelemeye gönder. Yöneticiye bildirim gider, karar verince sana da gelir.
            </p>
          </div>
        </div>
      )}

      <label className="field">
        <span className="field-label">Ne yaptın?</span>
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="örn. Hakem raporu ekranı bitti"
          required
        />
        <span className="field-hint">Tek satır — listede bu görünecek.</span>
      </label>

      <label className="field">
        <span className="field-label">
          Açıklama <span className="field-optional">— inceleyecek kişi bunu okuyacak</span>
        </span>
        <textarea
          rows={4}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Neyi değiştirdin, nereye dikkat etmesini istersin, eksik kalan bir şey var mı?"
        />
      </label>

      <div className="form-actions">
        <button type="submit" className="btn-primary" disabled={requesting || !title.trim()}>
          {requesting ? 'Gönderiliyor...' : again ? 'Tekrar gönder' : 'İncelemeye gönder'}
        </button>
        {requestError && <p className="error">{requestError}</p>}
      </div>
    </form>
  )
}

// Commit messages carry their full body; a list row shows the subject only.
function firstLine(message: string): string {
  return message.split('\n')[0]
}
