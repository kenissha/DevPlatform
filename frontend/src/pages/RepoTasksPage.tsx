import { useEffect, useState, type DragEvent, type FormEvent } from 'react'
import { useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { Person, Task, TaskStatus } from '../api/types'
import { TASK_STATUS_BADGE, TASK_STATUS_LABELS, TASK_STATUSES } from '../labels'

export function RepoTasksPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [people, setPeople] = useState<Person[]>([])
  const [error, setError] = useState<string | null>(null)
  const [dragOverStatus, setDragOverStatus] = useState<TaskStatus | null>(null)

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

  // Optimistic: the board should react the instant a card is dropped
  // rather than waiting a round-trip. If the API call fails, reload()
  // pulls the task's real (unchanged) column back from the server.
  async function setStatus(task: Task, status: TaskStatus) {
    if (task.status === status) return
    setTasks((prev) => (prev ? prev.map((t) => (t.id === task.id ? { ...t, status } : t)) : prev))
    try {
      await api.updateTask(repo, task.id, { status })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Görev güncellenemedi')
      reload()
    }
  }

  function handleDrop(e: DragEvent<HTMLDivElement>, status: TaskStatus) {
    e.preventDefault()
    setDragOverStatus(null)
    const taskId = e.dataTransfer.getData('text/plain')
    const task = tasks?.find((t) => t.id === taskId)
    if (task) setStatus(task, status)
  }

  function personLabel(subject: string): string {
    const person = people.find((p) => p.subject === subject)
    return person?.email || subject
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

      {tasks === null && <p className="empty-state">Yükleniyor...</p>}
      {tasks && (
        <div className="kanban-board">
          {TASK_STATUSES.map((status) => {
            const columnTasks = tasks.filter((t) => t.status === status)
            return (
              <div
                key={status}
                className={dragOverStatus === status ? 'kanban-column drag-over' : 'kanban-column'}
                onDragOver={(e) => {
                  e.preventDefault()
                  setDragOverStatus(status)
                }}
                onDragLeave={() => setDragOverStatus((s) => (s === status ? null : s))}
                onDrop={(e) => handleDrop(e, status)}
              >
                <div className="kanban-column-header">
                  <span className={`badge ${TASK_STATUS_BADGE[status]}`}>{TASK_STATUS_LABELS[status]}</span>
                  <span className="muted">{columnTasks.length}</span>
                </div>
                <div className="kanban-column-body">
                  {columnTasks.map((task) => (
                    <div
                      key={task.id}
                      className="kanban-card"
                      draggable
                      onDragStart={(e) => e.dataTransfer.setData('text/plain', task.id)}
                    >
                      {task.urgent && <span className="badge badge-danger">Acil</span>}
                      <p className="kanban-card-title">{task.title}</p>
                      <p className="kanban-card-meta">
                        {task.assignedTo ? personLabel(task.assignedTo) : 'Atanmamış'}
                      </p>
                    </div>
                  ))}
                  {columnTasks.length === 0 && <p className="empty-state">Görev yok.</p>}
                </div>
              </div>
            )
          })}
        </div>
      )}

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
