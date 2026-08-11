import { useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { MergeRequest, Task, TaskStatus } from '../api/types'

const MR_STATUS_LABELS: Record<MergeRequest['status'], string> = {
  open: 'Açık',
  approved: 'Onaylandı',
  rejected: 'Reddedildi',
}

const TASK_STATUS_LABELS: Record<TaskStatus, string> = {
  in_progress: 'Yapılıyor',
  awaiting_test: 'Test bekliyor',
  done: 'Bitti',
}

const TASK_STATUSES: TaskStatus[] = ['in_progress', 'awaiting_test', 'done']

export function RepoDetailPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const [branches, setBranches] = useState<string[] | null>(null)
  const [mergeRequests, setMergeRequests] = useState<MergeRequest[] | null>(null)
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [title, setTitle] = useState('')
  const [sourceBranch, setSourceBranch] = useState('')
  const [targetBranch, setTargetBranch] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const [taskTitle, setTaskTitle] = useState('')
  const [taskDescription, setTaskDescription] = useState('')
  const [taskAssignee, setTaskAssignee] = useState('')
  const [creatingTask, setCreatingTask] = useState(false)
  const [createTaskError, setCreateTaskError] = useState<string | null>(null)

  function reload() {
    Promise.all([api.listBranches(repo), api.listMergeRequests(repo), api.listTasks(repo)])
      .then(([b, mrs, ts]) => {
        setBranches(b)
        setMergeRequests(mrs)
        setTasks(ts)
        if (!sourceBranch && b.length > 0) setSourceBranch(b[0])
        if (!targetBranch && b.includes('main')) setTargetBranch('main')
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  // Deliberately keyed only on `repo`, not sourceBranch/targetBranch: reload
  // just seeds those two as defaults once branches first load, it must not
  // re-fetch every time the picker selection changes.
  // eslint-disable-next-line react-hooks/exhaustive-deps
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

  async function handleCreateTask(e: FormEvent) {
    e.preventDefault()
    if (!taskTitle.trim()) return
    setCreatingTask(true)
    setCreateTaskError(null)
    try {
      await api.createTask(repo, taskTitle.trim(), taskDescription.trim(), taskAssignee.trim())
      setTaskTitle('')
      setTaskDescription('')
      setTaskAssignee('')
      reload()
    } catch (err) {
      setCreateTaskError(err instanceof ApiError ? err.message : 'Görev oluşturulamadı')
    } finally {
      setCreatingTask(false)
    }
  }

  async function handleTaskStatusChange(task: Task, status: TaskStatus) {
    try {
      await api.updateTask(repo, task.id, { status })
      reload()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Görev güncellenemedi')
    }
  }

  async function handleTaskUrgentToggle(task: Task) {
    try {
      await api.updateTask(repo, task.id, { urgent: !task.urgent })
      reload()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Görev güncellenemedi')
    }
  }

  return (
    <div className="page">
      <p>
        <Link to="/">← Repolar</Link>
      </p>
      <h1>{repo}</h1>

      {error && <p className="error">{error}</p>}

      <section>
        <h2>Branch'ler</h2>
        {branches === null && <p className="muted">Yükleniyor...</p>}
        {branches !== null && branches.length === 0 && <p className="muted">Henüz branch yok.</p>}
        {branches !== null && branches.length > 0 && (
          <ul className="branch-list">
            {branches.map((b) => (
              <li key={b}>
                <code>{b}</code>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section>
        <h2>Görevler</h2>
        {tasks === null && <p className="muted">Yükleniyor...</p>}
        {tasks !== null && tasks.length === 0 && <p className="muted">Henüz görev yok.</p>}
        {tasks !== null && tasks.length > 0 && (
          <ul className="task-list">
            {tasks.map((task) => (
              <li key={task.id} className={task.urgent ? 'task-urgent' : undefined}>
                <div className="task-row">
                  <span className="task-title">
                    {task.urgent && <span className="urgent-flag">ACİL</span>}
                    {task.title}
                  </span>
                  <select
                    value={task.status}
                    onChange={(e) => handleTaskStatusChange(task, e.target.value as TaskStatus)}
                  >
                    {TASK_STATUSES.map((s) => (
                      <option key={s} value={s}>
                        {TASK_STATUS_LABELS[s]}
                      </option>
                    ))}
                  </select>
                  <button type="button" className="link-button" onClick={() => handleTaskUrgentToggle(task)}>
                    {task.urgent ? 'Acili kaldır' : 'Acil işaretle'}
                  </button>
                </div>
                {task.description && <p className="muted task-description">{task.description}</p>}
                <p className="muted task-meta">
                  {task.assignedTo ? `${task.assignedTo} üzerinde` : 'Atanmamış'} · {task.author} tarafından açıldı
                </p>
              </li>
            ))}
          </ul>
        )}

        <form onSubmit={handleCreateTask} className="mr-form">
          <h3>Yeni görev</h3>
          <label htmlFor="task-title">Başlık</label>
          <input id="task-title" value={taskTitle} onChange={(e) => setTaskTitle(e.target.value)} required />

          <label htmlFor="task-description">Açıklama</label>
          <textarea
            id="task-description"
            value={taskDescription}
            onChange={(e) => setTaskDescription(e.target.value)}
            rows={2}
          />

          <label htmlFor="task-assignee">Atanan (opsiyonel)</label>
          <input id="task-assignee" value={taskAssignee} onChange={(e) => setTaskAssignee(e.target.value)} />

          <button type="submit" disabled={creatingTask || !taskTitle.trim()}>
            {creatingTask ? 'Oluşturuluyor...' : 'Görev oluştur'}
          </button>
          {createTaskError && <p className="error">{createTaskError}</p>}
        </form>
      </section>

      <section>
        <h2>Merge İstekleri</h2>
        {mergeRequests === null && <p className="muted">Yükleniyor...</p>}
        {mergeRequests !== null && mergeRequests.length === 0 && (
          <p className="muted">Henüz merge isteği yok.</p>
        )}
        {mergeRequests !== null && mergeRequests.length > 0 && (
          <ul className="mr-list">
            {mergeRequests.map((mr) => (
              <li key={mr.id}>
                <Link to={`/repos/${encodeURIComponent(repo)}/merge-requests/${mr.id}`}>{mr.title}</Link>
                <span className={`status-badge status-${mr.status}`}>{MR_STATUS_LABELS[mr.status]}</span>
                <span className="muted">
                  {mr.sourceBranch} → {mr.targetBranch}
                </span>
              </li>
            ))}
          </ul>
        )}

        {branches !== null && branches.length >= 2 && (
          <form onSubmit={handleCreate} className="mr-form">
            <h3>Yeni merge isteği</h3>
            <label htmlFor="mr-title">Başlık</label>
            <input id="mr-title" value={title} onChange={(e) => setTitle(e.target.value)} required />

            <div className="branch-pickers">
              <div>
                <label htmlFor="mr-source">Kaynak branch</label>
                <select id="mr-source" value={sourceBranch} onChange={(e) => setSourceBranch(e.target.value)}>
                  {branches.map((b) => (
                    <option key={b} value={b}>
                      {b}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label htmlFor="mr-target">Hedef branch</label>
                <select id="mr-target" value={targetBranch} onChange={(e) => setTargetBranch(e.target.value)}>
                  {branches.map((b) => (
                    <option key={b} value={b}>
                      {b}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <button type="submit" disabled={creating || sourceBranch === targetBranch}>
              {creating ? 'Oluşturuluyor...' : 'Merge isteği oluştur'}
            </button>
            {sourceBranch === targetBranch && (
              <p className="error">Kaynak ve hedef branch aynı olamaz.</p>
            )}
            {createError && <p className="error">{createError}</p>}
          </form>
        )}
      </section>
    </div>
  )
}
