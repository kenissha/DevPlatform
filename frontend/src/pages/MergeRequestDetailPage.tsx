import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { MergeRequestDetail } from '../api/types'
import { BranchIcon, CheckIcon } from '../components/icons'
import { MR_STATUS_BADGE, MR_STATUS_LABELS, formatDate, formatRelative } from '../labels'
import { useAuth } from '../auth/AuthContext'

export function MergeRequestDetailPage() {
  const { repo = '', id = '' } = useParams<{ repo: string; id: string }>()
  const { user } = useAuth()
  const [mr, setMr] = useState<MergeRequestDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [acting, setActing] = useState(false)
  const [note, setNote] = useState('')

  function reload() {
    setError(null)
    api
      .getMergeRequest(repo, id)
      .then(setMr)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  useEffect(reload, [repo, id])

  // Approve/Reject never touch git — see backend/internal/mergerequest's
  // package doc comment. By the time a Yönetici clicks Onayla they've
  // already merged the real result themselves (with real git, outside
  // this panel) and pushed it — this call is only their record of that,
  // with note as an optional comment (why it was rejected, or a
  // reference to what was actually merged).
  async function decide(action: 'approve' | 'reject') {
    setActing(true)
    setActionError(null)
    try {
      if (action === 'approve') {
        await api.approveMergeRequest(repo, id, note.trim())
      } else {
        await api.rejectMergeRequest(repo, id, note.trim())
      }
      setNote('')
      reload()
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : 'İşlem başarısız')
    } finally {
      setActing(false)
    }
  }

  if (error) {
    return (
      <div className="page">
        <p>
          <Link to={`/repos/${encodeURIComponent(repo)}/merge-requests`}>← İnceleme istekleri</Link>
        </p>
        <p className="error">{error}</p>
      </div>
    )
  }

  if (!mr) {
    return <p className="page-message">Yükleniyor...</p>
  }

  const canDecide = user?.role === 'admin' && mr.status === 'open'
  const isAuthor = user?.subject === mr.author
  const totals = mr.diff.stats.reduce(
    (acc, s) => ({ add: acc.add + s.addition, del: acc.del + s.deletion }),
    { add: 0, del: 0 },
  )

  return (
    <div className="page">
      <p>
        <Link to={`/repos/${encodeURIComponent(repo)}/merge-requests`}>← Merge istekleri</Link>
      </p>

      <div className="page-header">
        <div className="page-title-group">
          <h1>{mr.title}</h1>
          <p className="page-subtitle row-meta">
            <span className={`badge ${MR_STATUS_BADGE[mr.status]}`}>{MR_STATUS_LABELS[mr.status]}</span>
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
        </div>
      </div>

      {mr.description && (
        <div className="card mr-description">
          <div className="card-body">
            <p className="mr-description-label">{mr.author} yazdı</p>
            <p className="task-desc">{mr.description}</p>
          </div>
        </div>
      )}

      {mr.status !== 'open' && <DecisionOutcome mr={mr} repo={repo} isAuthor={isAuthor} />}

      {canDecide && (
        <div className="decision-panel">
          <div className="decision-head">
            <h2>Kararın</h2>
            <p className="decision-sub">
              Onaylamak git tarafında bir şey yapmaz — birleştirmeyi sen yapıyorsun, bu ekran o
              kararın kaydı. Reddedersen {mr.author} bildirim alır ve notun ona gider.
            </p>
          </div>

          <label className="field">
            <span className="field-label">
              Not <span className="field-optional">— reddederken neden olduğunu yaz</span>
            </span>
            <textarea
              rows={3}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="örn. testleri de ekler misin — ya da birleştirdiğin commit'in referansı"
            />
          </label>

          {actionError && <p className="error">{actionError}</p>}

          <div className="decision-actions">
            <button type="button" className="btn-primary" onClick={() => decide('approve')} disabled={acting}>
              <CheckIcon /> Onayla
            </button>
            <button type="button" className="btn-danger" onClick={() => decide('reject')} disabled={acting}>
              Geri gönder
            </button>
          </div>
        </div>
      )}

      <div className="section-title">
        <h2>Değişiklikler</h2>
        {mr.diff.stats.length > 0 && (
          <span className="muted mono" style={{ fontSize: 12 }}>
            {mr.diff.stats.length} dosya <span className="add">+{totals.add}</span>{' '}
            <span className="del">−{totals.del}</span>
          </span>
        )}
      </div>

      <div className="card">
        {mr.diff.stats.length === 0 ? (
          <p className="empty-state">Değişiklik bulunamadı.</p>
        ) : (
          <>
            <div className="diff-stats">
              {mr.diff.stats.map((s) => (
                <span key={s.name} className="diff-stat">
                  {s.name}
                  <span className="add">+{s.addition}</span>
                  <span className="del">−{s.deletion}</span>
                </span>
              ))}
            </div>
            <DiffView text={mr.diff.unifiedDiff} />
          </>
        )}
      </div>
    </div>
  )
}

// DecisionOutcome is what a decided request says to whoever opens it —
// and, for a rejected one, where its author goes next. Without the way
// forward a rejection is a dead end: the note explains what to fix but
// nothing on the page leads back to the branch it has to be fixed on.
function DecisionOutcome({
  mr,
  repo,
  isAuthor,
}: {
  mr: MergeRequestDetail
  repo: string
  isAuthor: boolean
}) {
  const rejected = mr.status === 'rejected'
  return (
    <div className={rejected ? 'outcome-panel is-rejected' : 'outcome-panel is-approved'}>
      <div className="outcome-head">
        <span className={`badge ${MR_STATUS_BADGE[mr.status]}`}>{MR_STATUS_LABELS[mr.status]}</span>
        <span className="outcome-when">{formatRelative(mr.createdAt)} açılmıştı</span>
      </div>

      {mr.note ? (
        <p className="outcome-note">“{mr.note}”</p>
      ) : (
        <p className="review-panel-sub">Not bırakılmamış.</p>
      )}

      {rejected && (
        <div className="outcome-next">
          <p className="review-panel-sub">
            {isAuthor
              ? 'Düzeltip aynı branch üzerinden tekrar gönderebilirsin — yeni bir istek açılır.'
              : 'Yazarı düzeltip aynı branch üzerinden tekrar gönderebilir.'}
          </p>
          <Link
            to={`/repos/${encodeURIComponent(repo)}/branches/${mr.sourceBranch}`}
            className="btn-secondary"
          >
            {mr.sourceBranch} branch'ine git →
          </Link>
        </div>
      )}
    </div>
  )
}

// Renders a unified diff with per-line colouring, the way a reviewer
// expects to read one. Classification is purely prefix-based on the raw
// text the backend already produced — no diff re-parsing, so what's shown
// is exactly what git would print.
function DiffView({ text }: { text: string }) {
  const lines = text.replace(/\n$/, '').split('\n')
  return (
    <pre className="diff">
      <code>
        {lines.map((line, i) => (
          <span key={i} className={`diff-line ${lineClass(line)}`}>
            {line || ' '}
          </span>
        ))}
      </code>
    </pre>
  )
}

function lineClass(line: string): string {
  if (line.startsWith('@@')) return 'l-hunk'
  if (line.startsWith('+++') || line.startsWith('---')) return 'l-meta'
  if (line.startsWith('diff --git') || line.startsWith('index ') || line.startsWith('new file') ||
      line.startsWith('deleted file') || line.startsWith('similarity ') || line.startsWith('rename ')) {
    return 'l-meta'
  }
  if (line.startsWith('+')) return 'l-add'
  if (line.startsWith('-')) return 'l-del'
  return ''
}
