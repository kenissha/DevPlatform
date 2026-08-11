import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api } from '../api/client'
import type { MergeRequest, Task } from '../api/types'
import { BranchIcon, RepoIcon } from '../components/icons'
import { MR_STATUS_BADGE, MR_STATUS_LABELS, TASK_STATUS_BADGE, TASK_STATUS_LABELS } from '../labels'

export function RepoOverviewPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const [branches, setBranches] = useState<string[] | null>(null)
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [mergeRequests, setMergeRequests] = useState<MergeRequest[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setError(null)
    Promise.all([api.listBranches(repo), api.listTasks(repo), api.listMergeRequests(repo)])
      .then(([b, t, m]) => {
        setBranches(b)
        setTasks(t)
        setMergeRequests(m)
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [repo])

  const openTasks = tasks?.filter((t) => t.status !== 'done') ?? []
  const openMrs = mergeRequests?.filter((m) => m.status === 'open') ?? []

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1 className="repo-title">
            <RepoIcon />
            {repo}
          </h1>
          <p className="page-subtitle">
            <code>git clone http://&lt;sunucu&gt;/git/{repo}.git</code>
          </p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="section-title">
        <h2>Açık görevler</h2>
        <span className="badge badge-neutral">{openTasks.length}</span>
        <div className="spacer" />
        <Link to={`/repos/${encodeURIComponent(repo)}/tasks`}>Tümü →</Link>
      </div>
      <div className="card">
        {tasks === null && <p className="empty-state">Yükleniyor...</p>}
        {tasks !== null && openTasks.length === 0 && <p className="empty-state">Açık görev yok.</p>}
        {openTasks.length > 0 && (
          <ul className="row-list">
            {openTasks.slice(0, 5).map((task) => (
              <li key={task.id} className={task.urgent ? 'urgent' : undefined}>
                <div className="row-main">
                  {task.urgent && <span className="badge badge-danger">Acil</span>}
                  <span className="row-title">{task.title}</span>
                  <div className="spacer" />
                  <span className={`badge ${TASK_STATUS_BADGE[task.status]}`}>
                    {TASK_STATUS_LABELS[task.status]}
                  </span>
                </div>
                <p className="row-meta">{task.assignedTo ? `${task.assignedTo} üzerinde` : 'Atanmamış'}</p>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>Açık merge istekleri</h2>
        <span className="badge badge-neutral">{openMrs.length}</span>
        <div className="spacer" />
        <Link to={`/repos/${encodeURIComponent(repo)}/merge-requests`}>Tümü →</Link>
      </div>
      <div className="card">
        {mergeRequests === null && <p className="empty-state">Yükleniyor...</p>}
        {mergeRequests !== null && openMrs.length === 0 && (
          <p className="empty-state">Açık merge isteği yok.</p>
        )}
        {openMrs.length > 0 && (
          <ul className="row-list">
            {openMrs.slice(0, 5).map((mr) => (
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
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>Branch'ler</h2>
        <span className="badge badge-neutral">{branches?.length ?? 0}</span>
        <div className="spacer" />
        <Link to={`/repos/${encodeURIComponent(repo)}/branches`}>Tümü →</Link>
      </div>
      <div className="card">
        {branches === null && <p className="empty-state">Yükleniyor...</p>}
        {branches?.length === 0 && (
          <p className="empty-state">
            Henüz branch yok — ilk push'unuzu yaptığınızda burada görünecek.
          </p>
        )}
        {branches && branches.length > 0 && (
          <ul className="row-list">
            {branches.slice(0, 5).map((b) => (
              <li key={b}>
                <div className="row-main">
                  <span className="branch-chip">
                    <BranchIcon />
                    {b}
                  </span>
                  {b === 'main' && <span className="badge badge-warn">Korumalı</span>}
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
