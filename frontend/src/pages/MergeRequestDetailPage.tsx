import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { MergeRequestDetail } from '../api/types'
import { useAuth } from '../auth/AuthContext'

const STATUS_LABELS: Record<MergeRequestDetail['status'], string> = {
  open: 'Açık',
  approved: 'Onaylandı',
  rejected: 'Reddedildi',
}

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

  async function handleApprove() {
    setActing(true)
    setActionError(null)
    try {
      await api.approveMergeRequest(repo, id)
      reload()
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : 'Onaylanamadı')
    } finally {
      setActing(false)
    }
  }

  async function handleReject() {
    setActing(true)
    setActionError(null)
    try {
      await api.rejectMergeRequest(repo, id)
      reload()
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : 'Reddedilemedi')
    } finally {
      setActing(false)
    }
  }

  if (error) {
    return (
      <div className="page">
        <p>
          <Link to={`/repos/${encodeURIComponent(repo)}`}>← {repo}</Link>
        </p>
        <p className="error">{error}</p>
      </div>
    )
  }

  if (!mr) {
    return <p className="page-message">Yükleniyor...</p>
  }

  const canDecide = user?.role === 'admin' && mr.status === 'open'

  return (
    <div className="page">
      <p>
        <Link to={`/repos/${encodeURIComponent(repo)}`}>← {repo}</Link>
      </p>
      <h1>{mr.title}</h1>
      <p className="muted">
        <span className={`status-badge status-${mr.status}`}>{STATUS_LABELS[mr.status]}</span>{' '}
        {mr.sourceBranch} → {mr.targetBranch} · {mr.author} tarafından ·{' '}
        {new Date(mr.createdAt).toLocaleString('tr-TR')}
      </p>
      {mr.mergedCommit && (
        <p className="muted">
          Birleştirilen commit: <code>{mr.mergedCommit}</code>
        </p>
      )}

      {canDecide && (
        <div className="mr-actions">
          <button type="button" onClick={handleApprove} disabled={acting} className="approve-button">
            Onayla ve birleştir
          </button>
          <button type="button" onClick={handleReject} disabled={acting} className="reject-button">
            Reddet
          </button>
        </div>
      )}
      {actionError && <p className="error">{actionError}</p>}

      <section>
        <h2>Değişiklikler</h2>
        {mr.diff.stats.length === 0 && <p className="muted">Değişiklik bulunamadı.</p>}
        {mr.diff.stats.length > 0 && (
          <ul className="stat-list">
            {mr.diff.stats.map((s) => (
              <li key={s.name}>
                <code>{s.name}</code>
                <span className="stat-add">+{s.addition}</span>
                <span className="stat-del">-{s.deletion}</span>
              </li>
            ))}
          </ul>
        )}
        <pre className="diff-view">{mr.diff.unifiedDiff}</pre>
      </section>
    </div>
  )
}
