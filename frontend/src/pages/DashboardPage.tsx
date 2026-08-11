import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import type { MergeRequest, Task } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { BranchIcon } from '../components/icons'
import { useRepos } from '../repos/ReposContext'
import { MR_STATUS_BADGE, MR_STATUS_LABELS, TASK_STATUS_BADGE, TASK_STATUS_LABELS } from '../labels'

// The platform's landing page: what is waiting on me, and who is working
// on what across every repository — the "kimin ne üzerinde çalıştığını
// görebilmek" goal, answered without opening each repo in turn.
export function DashboardPage() {
  const { user } = useAuth()
  const { repos } = useRepos()
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [mergeRequests, setMergeRequests] = useState<MergeRequest[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    Promise.all([api.listAllTasks(), api.listAllMergeRequests('open')])
      .then(([t, m]) => {
        setTasks(t)
        setMergeRequests(m)
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  const openTasks = tasks?.filter((t) => t.status !== 'done') ?? []
  const myTasks = openTasks.filter((t) => t.assignedTo === user?.subject)
  const urgent = openTasks.filter((t) => t.urgent)

  // Group open work by assignee so the board reads as "who has what".
  const byPerson = new Map<string, Task[]>()
  for (const t of openTasks) {
    const key = t.assignedTo || ''
    byPerson.set(key, [...(byPerson.get(key) ?? []), t])
  }
  const people = [...byPerson.entries()].sort((a, b) => {
    if (a[0] === '') return 1
    if (b[0] === '') return -1
    return b[1].length - a[1].length
  })

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Panel</h1>
          <p className="page-subtitle">Tüm projelerdeki güncel durum</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="stat-grid">
        <StatTile label="Repo" value={repos?.length ?? 0} />
        <StatTile label="Açık görev" value={openTasks.length} />
        <StatTile label="Bana atanan" value={myTasks.length} />
        <StatTile label="Bekleyen merge" value={mergeRequests?.length ?? 0} />
        <StatTile label="Acil" value={urgent.length} />
      </div>

      <div className="section-title">
        <h2>Bana atanan görevler</h2>
        <span className="badge badge-neutral">{myTasks.length}</span>
      </div>
      <div className="card">
        {tasks === null && <p className="empty-state">Yükleniyor...</p>}
        {tasks !== null && myTasks.length === 0 && (
          <p className="empty-state">Üzerinize atanmış açık görev yok.</p>
        )}
        {myTasks.length > 0 && (
          <ul className="row-list">
            {myTasks.map((task) => (
              <TaskRow key={task.id} task={task} />
            ))}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>İnceleme bekleyenler</h2>
        <span className="badge badge-neutral">{mergeRequests?.length ?? 0}</span>
      </div>
      <div className="card">
        {mergeRequests === null && <p className="empty-state">Yükleniyor...</p>}
        {mergeRequests?.length === 0 && <p className="empty-state">Bekleyen merge isteği yok.</p>}
        {mergeRequests && mergeRequests.length > 0 && (
          <ul className="row-list">
            {mergeRequests.map((mr) => (
              <li key={`${mr.repo}-${mr.id}`}>
                <div className="row-main">
                  <Link
                    to={`/repos/${encodeURIComponent(mr.repo)}/merge-requests/${mr.id}`}
                    className="row-title"
                  >
                    {mr.title}
                  </Link>
                  <div className="spacer" />
                  <span className={`badge ${MR_STATUS_BADGE[mr.status]}`}>{MR_STATUS_LABELS[mr.status]}</span>
                </div>
                <p className="row-meta">
                  <Link to={`/repos/${encodeURIComponent(mr.repo)}`}>{mr.repo}</Link>
                  <span>·</span>
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
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>Kim ne üzerinde çalışıyor</h2>
      </div>
      <div className="card">
        {tasks === null && <p className="empty-state">Yükleniyor...</p>}
        {tasks !== null && people.length === 0 && <p className="empty-state">Açık görev yok.</p>}
        {people.map(([person, personTasks]) => (
          <div key={person || 'unassigned'} className="person-block">
            <div className="person-head">
              <span className="avatar">{(person || '?').slice(0, 2)}</span>
              <strong>{person || 'Atanmamış'}</strong>
              <span className="badge badge-neutral">{personTasks.length}</span>
            </div>
            <ul className="row-list">
              {personTasks.map((task) => (
                <TaskRow key={task.id} task={task} showAssignee={false} />
              ))}
            </ul>
          </div>
        ))}
      </div>
    </div>
  )
}

function StatTile({ label, value }: { label: string; value: number }) {
  return (
    <div className="stat-tile">
      <div className="stat-label">{label}</div>
      <div className="stat-value">{value}</div>
    </div>
  )
}

function TaskRow({ task, showAssignee = true }: { task: Task; showAssignee?: boolean }) {
  return (
    <li className={task.urgent ? 'urgent' : undefined}>
      <div className="row-main">
        {task.urgent && <span className="badge badge-danger">Acil</span>}
        <span className="row-title">{task.title}</span>
        <div className="spacer" />
        <span className={`badge ${TASK_STATUS_BADGE[task.status]}`}>{TASK_STATUS_LABELS[task.status]}</span>
      </div>
      <p className="row-meta">
        <Link to={`/repos/${encodeURIComponent(task.repo)}/tasks`}>{task.repo}</Link>
        {showAssignee && (
          <>
            <span>·</span>
            {task.assignedTo ? `${task.assignedTo} üzerinde` : 'Atanmamış'}
          </>
        )}
      </p>
    </li>
  )
}
