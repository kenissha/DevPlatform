import { useEffect, useState, type FormEvent } from 'react'
import { useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { Person, Task, TaskStatus } from '../api/types'
import { TASK_STATUSES, TASK_STATUS_LABELS } from '../labels'
import { formatDate } from '../labels'

export function RepoTasksPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [people, setPeople] = useState<Person[]>([])
  const [error, setError] = useState<string | null>(null)

  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [assignee, setAssignee] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  function reload() {
    api
      .listTasks(repo)
      .then(setTasks)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  useEffect(reload, [repo])

  // The assignee picker lists people the platform has actually seen, so a
  // task can't be assigned to a misspelled name that then never shows up
  // on anyone's dashboard.
  useEffect(() => {
    api.listPeople().then(setPeople).catch(() => setPeople([]))
  }, [])

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (!title.trim()) return
    setCreating(true)
    setCreateError(null)
    try {
      await api.createTask(repo, title.trim(), description.trim(), assignee.trim())
      setTitle('')
      setDescription('')
      setAssignee('')
      reload()
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : 'Görev oluşturulamadı')
    } finally {
      setCreating(false)
    }
  }

  async function patch(
    task: Task,
    changes: Partial<{ status: TaskStatus; urgent: boolean; assignedTo: string }>,
  ) {
    try {
      await api.updateTask(repo, task.id, changes)
      reload()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Görev güncellenemedi')
    }
  }

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Görevler</h1>
          <p className="page-subtitle">{repo} üzerindeki iş takibi</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="card">
        {tasks === null && <p className="empty-state">Yükleniyor...</p>}
        {tasks?.length === 0 && <p className="empty-state">Henüz görev yok.</p>}
        {tasks && tasks.length > 0 && (
          <ul className="row-list">
            {tasks.map((task) => (
              <li key={task.id} className={task.urgent ? 'urgent' : undefined}>
                <div className="task-row">
                  {task.urgent && <span className="badge badge-danger">Acil</span>}
                  <span className="row-title">{task.title}</span>
                  <div className="spacer" />
                  <select
                    value={task.status}
                    aria-label="Durum"
                    onChange={(e) => patch(task, { status: e.target.value as TaskStatus })}
                  >
                    {TASK_STATUSES.map((s) => (
                      <option key={s} value={s}>
                        {TASK_STATUS_LABELS[s]}
                      </option>
                    ))}
                  </select>
                  <button
                    type="button"
                    className="btn-secondary btn-sm"
                    onClick={() => patch(task, { urgent: !task.urgent })}
                  >
                    {task.urgent ? 'Acili kaldır' : 'Acil işaretle'}
                  </button>
                </div>
                {task.description && <p className="task-desc">{task.description}</p>}
                <p className="row-meta">
                  <select
                    className="inline-select"
                    aria-label="Atanan"
                    value={task.assignedTo}
                    onChange={(e) => patch(task, { assignedTo: e.target.value })}
                  >
                    <option value="">Atanmamış</option>
                    {people.map((p) => (
                      <option key={p.subject} value={p.subject}>
                        {p.subject}
                      </option>
                    ))}
                    {/* A task assigned before that person was known still
                        shows its current value rather than snapping to
                        "Atanmamış". */}
                    {task.assignedTo && !people.some((p) => p.subject === task.assignedTo) && (
                      <option value={task.assignedTo}>{task.assignedTo}</option>
                    )}
                  </select>
                  <span>·</span>
                  {task.author} açtı
                  <span>·</span>
                  {formatDate(task.createdAt)}
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>Yeni görev</h2>
      </div>
      <div className="card">
        <div className="card-body">
          <form onSubmit={handleCreate}>
            <div className="field">
              <label htmlFor="task-title">Başlık</label>
              <input id="task-title" value={title} onChange={(e) => setTitle(e.target.value)} required />
            </div>
            <div className="field">
              <label htmlFor="task-description">Açıklama</label>
              <textarea
                id="task-description"
                rows={3}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </div>
            <div className="field">
              <label htmlFor="task-assignee">Atanan</label>
              <select id="task-assignee" value={assignee} onChange={(e) => setAssignee(e.target.value)}>
                <option value="">Atanmamış</option>
                {people.map((p) => (
                  <option key={p.subject} value={p.subject}>
                    {p.subject}
                    {p.email ? ` (${p.email})` : ''}
                  </option>
                ))}
              </select>
            </div>
            <div className="form-actions">
              <button type="submit" className="btn-primary" disabled={creating || !title.trim()}>
                {creating ? 'Oluşturuluyor...' : 'Görev oluştur'}
              </button>
              {createError && <p className="error">{createError}</p>}
            </div>
          </form>
        </div>
      </div>
    </div>
  )
}
