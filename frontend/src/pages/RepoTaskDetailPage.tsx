import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { AuditEvent, Person, Task, TaskPriority, TaskStatus } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { AuditIcon, TaskIcon } from '../components/icons'
import { TaskComments } from '../components/TaskComments'
import { TaskSubtasks } from '../components/TaskSubtasks'
import {
  TASK_PRIORITIES,
  TASK_PRIORITY_LABELS,
  TASK_STATUS_LABELS,
  TASK_STATUSES,
  dayHeading,
  formatDate,
  formatTime,
  groupByDay,
  todayKey,
} from '../labels'

// A task's own page, at its own URL — so it can be linked to in a chat
// message or a commit. The board's modal is still there for a glance;
// this is where the work actually gets discussed and its history read.
export function RepoTaskDetailPage() {
  const { repo = '', id = '' } = useParams<{ repo: string; id: string }>()
  const { user } = useAuth()
  const navigate = useNavigate()

  const [task, setTask] = useState<Task | null>(null)
  const [history, setHistory] = useState<AuditEvent[] | null>(null)
  const [people, setPeople] = useState<Person[]>([])
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [busy, setBusy] = useState(false)

  const reload = useCallback(() => {
    api
      .getTask(repo, id)
      .then(setTask)
      .catch((err) => setError(err instanceof ApiError ? err.message : 'Görev yüklenemedi'))
    // History failing must not blank the task itself — it is context, not
    // the subject of the page.
    api
      .taskHistory(repo, id)
      .then(setHistory)
      .catch(() => setHistory([]))
  }, [repo, id])

  useEffect(reload, [reload])
  useEffect(() => {
    api.listPeople().then(setPeople).catch(() => setPeople([]))
  }, [])

  async function patch(changes: Parameters<typeof api.updateTask>[2]) {
    setBusy(true)
    setActionError(null)
    try {
      await api.updateTask(repo, id, changes)
      reload()
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : 'Güncellenemedi')
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    setBusy(true)
    setActionError(null)
    try {
      await api.deleteTask(repo, id)
      navigate(`/repos/${encodeURIComponent(repo)}/tasks`)
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : 'Silinemedi')
      setBusy(false)
    }
  }

  function personLabel(subject: string): string {
    return people.find((p) => p.subject === subject)?.email || subject
  }

  if (error) {
    return (
      <div className="page">
        <p>
          <Link to={`/repos/${encodeURIComponent(repo)}/tasks`}>← Görevler</Link>
        </p>
        <p className="error">{error}</p>
      </div>
    )
  }

  if (!task) return <p className="page-message">Yükleniyor...</p>

  const canDelete = user?.role === 'admin' || task.author === user?.subject
  const overdue = Boolean(task.dueDate && task.status !== 'done' && task.dueDate < todayKey())

  return (
    <div className="page task-page">
      <p>
        <Link to={`/repos/${encodeURIComponent(repo)}/tasks`}>← Görevler</Link>
      </p>

      <div className="task-hero">
        <div className="task-hero-main">
          <p className="task-hero-key">
            {task.key && <span className="task-key">{task.key}</span>}
            <span className={`kanban-dot kanban-dot-${task.status}`} aria-hidden="true" />
            {TASK_STATUS_LABELS[task.status]}
          </p>
          {editing ? (
            <TaskEditForm
              repo={repo}
              task={task}
              onCancel={() => setEditing(false)}
              onSaved={() => {
                setEditing(false)
                reload()
              }}
            />
          ) : (
            <>
              <h1>{task.title}</h1>
              {task.description ? (
                <p className="task-desc">{task.description}</p>
              ) : (
                <p className="task-desc task-desc-empty">Açıklama yok.</p>
              )}
            </>
          )}
        </div>
      </div>

      {actionError && <p className="error">{actionError}</p>}

      <div className="task-split">
        <div className="task-main">
          <TaskSubtasks repo={repo} task={task} onChanged={setTask} />

          <TaskComments repo={repo} taskId={id} people={people} />

          <section>
          <div className="section-title">
            <h2>Geçmiş</h2>
            {history && history.length > 0 && (
              <span className="badge badge-neutral">{history.length}</span>
            )}
          </div>
          {history === null && (
            <div className="card">
              <p className="empty-state">Yükleniyor...</p>
            </div>
          )}
          {history?.length === 0 && (
            <div className="card">
              <p className="empty-state">Bu görev için kayıt yok.</p>
            </div>
          )}
          {history &&
            history.length > 0 &&
            groupByDay(history, (e) => e.at).map(([key, items]) => (
              <div key={key} className="day-group">
                <h3 className="day-heading">{dayHeading(items[0].at)}</h3>
                <ol className="timeline">
                  {items.map((e, i) => (
                    <li key={`${e.at}-${i}`}>
                      <span className="timeline-dot tone-neutral">
                        {e.action === 'task.created' ? <TaskIcon /> : <AuditIcon />}
                      </span>
                      <div className="timeline-body">
                        <p className="timeline-text">{e.summary || e.action}</p>
                        <p className="timeline-meta">
                          <strong>{personLabel(e.actor)}</strong>
                        </p>
                      </div>
                      <span className="timeline-time">{formatTime(e.at)}</span>
                    </li>
                  ))}
                </ol>
              </div>
            ))}
          </section>
        </div>

        <aside className="task-rail">
          <div className="card">
            <div className="card-body task-fields">
              <label className="field">
                <span className="field-label">Durum</span>
                <select
                  value={task.status}
                  disabled={busy}
                  onChange={(e) => patch({ status: e.target.value as TaskStatus })}
                >
                  {TASK_STATUSES.map((s) => (
                    <option key={s} value={s}>
                      {TASK_STATUS_LABELS[s]}
                    </option>
                  ))}
                </select>
              </label>

              <label className="field">
                <span className="field-label">Öncelik</span>
                <select
                  value={task.priority}
                  disabled={busy}
                  onChange={(e) => patch({ priority: e.target.value as TaskPriority })}
                >
                  {TASK_PRIORITIES.map((p) => (
                    <option key={p} value={p}>
                      {TASK_PRIORITY_LABELS[p]}
                    </option>
                  ))}
                </select>
              </label>

              <label className="field">
                <span className="field-label">
                  Bitiş tarihi {overdue && <span className="overdue-flag">gecikti</span>}
                </span>
                <input
                  type="date"
                  value={task.dueDate ?? ''}
                  disabled={busy}
                  onChange={(e) => patch({ dueDate: e.target.value })}
                />
              </label>

              <label className="field">
                <span className="field-label">Atanan</span>
                <select
                  value={task.assignedTo}
                  disabled={busy}
                  onChange={(e) => patch({ assignedTo: e.target.value })}
                >
                  <option value="">Atanmamış</option>
                  {people.map((p) => (
                    <option key={p.subject} value={p.subject}>
                      {p.email || p.subject}
                    </option>
                  ))}
                  {task.assignedTo && !people.some((p) => p.subject === task.assignedTo) && (
                    <option value={task.assignedTo}>{task.assignedTo}</option>
                  )}
                </select>
              </label>
            </div>
          </div>

          <div className="task-rail-actions">
            {!editing && (
              <button type="button" className="btn-secondary btn-sm" onClick={() => setEditing(true)}>
                Düzenle
              </button>
            )}
            <div className="spacer" />
            {canDelete &&
              (confirmingDelete ? (
                <div className="confirm-inline">
                  <span>Silinsin mi?</span>
                  <button type="button" className="btn-danger btn-sm" onClick={remove} disabled={busy}>
                    Evet
                  </button>
                  <button
                    type="button"
                    className="btn-secondary btn-sm"
                    onClick={() => setConfirmingDelete(false)}
                    disabled={busy}
                  >
                    Vazgeç
                  </button>
                </div>
              ) : (
                <button
                  type="button"
                  className="link-button danger"
                  onClick={() => setConfirmingDelete(true)}
                >
                  Sil
                </button>
              ))}
          </div>

          <p className="task-rail-meta">
            {personLabel(task.author)} açtı · {formatDate(task.createdAt)}
          </p>
        </aside>
      </div>
    </div>
  )
}

function TaskEditForm({
  repo,
  task,
  onCancel,
  onSaved,
}: {
  repo: string
  task: Task
  onCancel: () => void
  onSaved: () => void
}) {
  const [title, setTitle] = useState(task.title)
  const [description, setDescription] = useState(task.description)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function save(e: FormEvent) {
    e.preventDefault()
    if (!title.trim()) return
    setSaving(true)
    setError(null)
    try {
      // Only what actually changed is sent, so this cannot clobber an
      // edit somebody else made to the other field meanwhile.
      const changes: Partial<{ title: string; description: string }> = {}
      if (title.trim() !== task.title) changes.title = title.trim()
      if (description.trim() !== task.description) changes.description = description.trim()
      if (Object.keys(changes).length > 0) await api.updateTask(repo, task.id, changes)
      onSaved()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Kaydedilemedi')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={save} className="task-edit-form">
      <label className="field">
        <span className="field-label">Başlık</span>
        <input value={title} onChange={(e) => setTitle(e.target.value)} required autoFocus />
      </label>
      <label className="field">
        <span className="field-label">Açıklama</span>
        <textarea rows={6} value={description} onChange={(e) => setDescription(e.target.value)} />
      </label>
      {error && <p className="error">{error}</p>}
      <div className="form-actions">
        <button type="submit" className="btn-primary" disabled={saving || !title.trim()}>
          {saving ? 'Kaydediliyor...' : 'Kaydet'}
        </button>
        <button type="button" className="btn-secondary" onClick={onCancel} disabled={saving}>
          Vazgeç
        </button>
      </div>
    </form>
  )
}
