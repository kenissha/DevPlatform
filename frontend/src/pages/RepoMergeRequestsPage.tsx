import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api } from '../api/client'
import type { MergeRequest } from '../api/types'
import { BranchIcon } from '../components/icons'
import { MR_STATUS_BADGE, MR_STATUS_LABELS, formatDate } from '../labels'

// A read-only history of every İnceleme İsteği ever opened for this
// repo — open, onaylandı, reddedildi. Opening a NEW one doesn't happen
// here: that starts from the branch itself (see RepoBranchDetailPage,
// linked below), the same way GitHub's own "compare" flow starts from a
// branch, not a bare form asking you to name one.
export function RepoMergeRequestsPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const [mergeRequests, setMergeRequests] = useState<MergeRequest[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api
      .listMergeRequests(repo)
      .then(setMergeRequests)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [repo])

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>İnceleme istekleri</h1>
          <p className="page-subtitle">{repo} için inceleme geçmişi</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="card">
        {mergeRequests === null && <p className="empty-state">Yükleniyor...</p>}
        {mergeRequests?.length === 0 && (
          <p className="empty-state">
            Henüz inceleme isteği yok. Yeni bir tane açmak için{' '}
            <Link to={`/repos/${encodeURIComponent(repo)}/branches`}>ilgili branch'e</Link> git.
          </p>
        )}
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

      {mergeRequests && mergeRequests.length > 0 && (
        <p className="muted" style={{ fontSize: 13 }}>
          Yeni bir inceleme isteği açmak için{' '}
          <Link to={`/repos/${encodeURIComponent(repo)}/branches`}>branch'ler</Link> sayfasından ilgili
          branch'e gir.
        </p>
      )}
    </div>
  )
}
