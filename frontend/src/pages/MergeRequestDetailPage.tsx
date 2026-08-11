import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { MergeRequestDetail } from '../api/types'
import { BranchIcon } from '../components/icons'
import { MR_STATUS_BADGE, MR_STATUS_LABELS, formatDate } from '../labels'
import { useAuth } from '../auth/AuthContext'

export function MergeRequestDetailPage() {
  const { repo = '', id = '' } = useParams<{ repo: string; id: string }>()
  const { user } = useAuth()
  const [mr, setMr] = useState<MergeRequestDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [acting, setActing] = useState(false)

  function reload() {
    setError(null)
    api
      .getMergeRequest(repo, id)
      .then(setMr)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  useEffect(reload, [repo, id])

  async function decide(action: 'approve' | 'reject') {
    setActing(true)
    setActionError(null)
    try {
      if (action === 'approve') {
        await api.approveMergeRequest(repo, id)
      } else {
        await api.rejectMergeRequest(repo, id)
      }
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
          <Link to={`/repos/${encodeURIComponent(repo)}/merge-requests`}>← Merge istekleri</Link>
        </p>
        <p className="error">{error}</p>
      </div>
    )
  }

  if (!mr) {
    return <p className="page-message">Yükleniyor...</p>
  }

  const canDecide = user?.role === 'admin' && mr.status === 'open'
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

      {mr.mergedCommit && (
        <div className="card">
          <div className="card-body">
            <p className="muted" style={{ fontSize: 13 }}>
              <strong style={{ color: 'var(--success)' }}>Birleştirildi</strong> —{' '}
              <code>{mr.targetBranch}</code> artık <code>{mr.mergedCommit.slice(0, 10)}</code> commit'inde.
            </p>
          </div>
        </div>
      )}

      {canDecide && (
        <div className="card">
          <div className="card-body">
            <div className="form-actions" style={{ marginTop: 0 }}>
              <button type="button" className="btn-primary" onClick={() => decide('approve')} disabled={acting}>
                Onayla ve birleştir
              </button>
              <button type="button" className="btn-danger" onClick={() => decide('reject')} disabled={acting}>
                Reddet
              </button>
              {actionError && <p className="error">{actionError}</p>}
            </div>
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
