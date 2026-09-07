import { useEffect, useRef, useState, type DragEvent, type FormEvent } from 'react'
import { useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import type { Person, Task, TaskStatus } from '../api/types'
import { PlusIcon } from '../components/icons'
import { Modal } from '../components/Modal'
import { TASK_STATUS_LABELS, TASK_STATUSES, formatDate, formatRelative } from '../labels'

export function RepoTasksPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const { user } = useAuth()
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [people, setPeople] = useState<Person[]>([])
  const [error, setError] = useState<string | null>(null)
  const [dragOverStatus, setDragOverStatus] = useState<TaskStatus | null>(null)
  const [openTask, setOpenTask] = useState<Task | null>(null)
  const [creatingOpen, setCreatingOpen] = useState(false)
  const draggingRef = useRef(false)

  function reload() {
    api
      .listTasks(repo)
      .then(setTasks)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  useEffect(reload, [repo])

  // After a background reload() (e.g. once the detail panel makes a
  // change), openTask would otherwise hold a stale copy.
  useEffect(() => {
    if (!openTask || !tasks) return
    const fresh = tasks.find((t) => t.id === openTask.id)
    setOpenTask(fresh ?? null)
  }, [tasks, openTask?.id])

  // The assignee picker lists people the platform has actually seen, so a
  // task can't be assigned to a misspelled name that then never shows up
  // on anyone's dashboard.
  useEffect(() => {
    api.listPeople().then(setPeople).catch(() => setPeople([]))
  }, [])

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

  const openCount = tasks?.filter((t) => t.status !== 'done').length ?? 0
  const urgentCount = tasks?.filter((t) => t.urgent && t.status !== 'done').length ?? 0

  return (
    <div className="page board-page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Görevler</h1>
          <p className="page-subtitle">
            {tasks === null
              ? `${repo} üzerindeki iş takibi`
              : `${openCount} açık görev` + (urgentCount > 0 ? ` · ${urgentCount} acil` : '')}
          </p>
        </div>
        <button type="button" className="btn-primary" onClick={() => setCreatingOpen(true)}>
          <PlusIcon /> Yeni görev
        </button>
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
                  <span className={`kanban-dot kanban-dot-${status}`} aria-hidden="true" />
                  <span className="kanban-column-title">{TASK_STATUS_LABELS[status]}</span>
                  <span className="kanban-column-count">{columnTasks.length}</span>
                </div>
                <div className="kanban-column-body">
                  {columnTasks.map((task) => (
                    <div
                      key={task.id}
                      className={task.urgent ? 'kanban-card kanban-card-urgent' : 'kanban-card'}
                      draggable
                      role="button"
                      tabIndex={0}
                      onDragStart={(e) => {
                        draggingRef.current = true
                        e.dataTransfer.setData('text/plain', task.id)
                      }}
                      onDragEnd={() => {
                        draggingRef.current = false
                      }}
                      onClick={() => {
                        if (!draggingRef.current) setOpenTask(task)
                      }}
                      onKeyDown={(e) => {
                        if ((e.key === 'Enter' || e.key === ' ') && !draggingRef.current) {
                          e.preventDefault()
                          setOpenTask(task)
                        }
                      }}
                    >
                      {task.urgent && <span className="badge badge-danger">Acil</span>}
                      <p className="kanban-card-title">{task.title}</p>
                      <div className="kanban-card-foot">
                        <Avatar label={task.assignedTo ? personLabel(task.assignedTo) : ''} />
                        <span className="kanban-card-meta">
                          {task.assignedTo ? personLabel(task.assignedTo) : 'Atanmamış'}
                        </span>
                        <span className="kanban-card-age">{formatRelative(task.createdAt)}</span>
                      </div>
                    </div>
                  ))}
                  {columnTasks.length === 0 && (
                    <p className="kanban-empty">
                      {status === 'todo' ? 'Buraya sürükle ya da yeni görev aç.' : 'Buraya sürükle.'}
                    </p>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      )}

      {creatingOpen && (
        <NewTaskModal
          repo={repo}
          people={people}
          onClose={() => setCreatingOpen(false)}
          onCreated={() => {
            setCreatingOpen(false)
            reload()
          }}
        />
      )}

      {openTask && (
        <TaskDetailPanel
          task={openTask}
          people={people}
          repo={repo}
          canDelete={user?.role === 'admin' || openTask.author === user?.subject}
          onClose={() => setOpenTask(null)}
          onChanged={reload}
          onDeleted={() => {
            setOpenTask(null)
            reload()
          }}
        />
      )}
    </div>
  )
}

// Avatar carries the assignee at a glance, so scanning a column doesn't
// mean reading four email addresses. An unassigned task gets a hollow
// circle rather than a letter — visibly a gap, not a person.
function Avatar({ label }: { label: string }) {
  if (!label) return <span className="kanban-avatar kanban-avatar-empty" aria-hidden="true" />
  return (
    <span className="kanban-avatar" aria-hidden="true">
      {label.charAt(0).toUpperCase()}
    </span>
  )
}

function NewTaskModal({
  repo,
  people,
  onClose,
  onCreated,
}: {
  repo: string
  people: Person[]
  onClose: () => void
  onCreated: () => void
}) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [assignee, setAssignee] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (!title.trim()) return
    setCreating(true)
    setCreateError(null)
    try {
      await api.createTask(repo, title.trim(), description.trim(), assignee.trim())
      onCreated()
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : 'Görev oluşturulamadı')
    } finally {
      setCreating(false)
    }
  }

  return (
    <Modal title="Yeni görev" onClose={onClose}>
      <form onSubmit={handleCreate} className="modal-form">
        <label className="field">
          <span className="field-label">Başlık</span>
          <input value={title} onChange={(e) => setTitle(e.target.value)} required autoFocus />
        </label>

        <label className="field">
          <span className="field-label">
            Açıklama <span className="field-optional">— isteğe bağlı</span>
          </span>
          <textarea rows={4} value={description} onChange={(e) => setDescription(e.target.value)} />
        </label>

        <label className="field">
          <span className="field-label">Atanan</span>
          <select value={assignee} onChange={(e) => setAssignee(e.target.value)}>
            <option value="">Atanmamış</option>
            {people.map((p) => (
              <option key={p.subject} value={p.subject}>
                {p.email || p.subject}
              </option>
            ))}
          </select>
        </label>

        {createError && <p className="error">{createError}</p>}

        <div className="modal-actions">
          <button type="button" className="btn-secondary" onClick={onClose} disabled={creating}>
            Vazgeç
          </button>
          <button type="submit" className="btn-primary" disabled={creating || !title.trim()}>
            {creating ? 'Oluşturuluyor...' : 'Görev oluştur'}
          </button>
        </div>
        <p className="field-hint">Görev "Yapılacak" sütununda açılır; başlayınca sürükleyip taşırsın.</p>
      </form>
    </Modal>
  )
}

function TaskDetailPanel({
  task,
  people,
  repo,
  canDelete,
  onClose,
  onChanged,
  onDeleted,
}: {
  task: Task
  people: Person[]
  repo: string
  canDelete: boolean
  onClose: () => void
  onChanged: () => void
  onDeleted: () => void
}) {
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [busy, setBusy] = useState(false)

  async function patch(changes: Partial<{ status: TaskStatus; urgent: boolean; assignedTo: string }>) {
    setError(null)
    try {
      await api.updateTask(repo, task.id, changes)
      onChanged()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Görev güncellenemedi')
    }
  }

  async function handleDelete() {
    setBusy(true)
    setError(null)
    try {
      await api.deleteTask(repo, task.id)
      onDeleted()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Görev silinemedi')
      setBusy(false)
    }
  }

  if (editing) {
    return (
      <Modal title="Görevi düzenle" onClose={() => setEditing(false)}>
        <TaskEditForm
          repo={repo}
          task={task}
          onCancel={() => setEditing(false)}
          onSaved={() => {
            setEditing(false)
            onChanged()
          }}
        />
      </Modal>
    )
  }

  return (
    <Modal title={task.title} onClose={onClose}>
      {error && <p className="error">{error}</p>}

      {task.description ? (
        <p className="task-desc">{task.description}</p>
      ) : (
        <p className="task-desc task-desc-empty">Açıklama yok.</p>
      )}

      {/* Status is the most-changed field here, so it is a row of targets
          rather than a dropdown: the current column reads without opening
          anything, and moving is one click. */}
      <div className="field">
        <span className="field-label">Durum</span>
        <div className="status-picker">
          {TASK_STATUSES.map((s) => (
            <button
              key={s}
              type="button"
              className={s === task.status ? 'status-option active' : 'status-option'}
              aria-pressed={s === task.status}
              onClick={() => patch({ status: s })}
            >
              <span className={`kanban-dot kanban-dot-${s}`} aria-hidden="true" />
              {TASK_STATUS_LABELS[s]}
            </button>
          ))}
        </div>
      </div>

      <label className="field">
        <span className="field-label">Atanan</span>
        <select value={task.assignedTo} onChange={(e) => patch({ assignedTo: e.target.value })}>
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

      <div className="task-actions">
        <button type="button" className="btn-secondary" onClick={() => setEditing(true)}>
          Düzenle
        </button>
        <button
          type="button"
          className={task.urgent ? 'btn-danger' : 'btn-secondary'}
          onClick={() => patch({ urgent: !task.urgent })}
        >
          {task.urgent ? 'Acili kaldır' : 'Acil işaretle'}
        </button>
        <div className="spacer" />
        {canDelete &&
          (confirmingDelete ? (
            /* Confirmed inline rather than through window.confirm: the task
               stays on screen behind the question, and the buttons name what
               they do instead of asking about "this item". */
            <div className="confirm-inline">
              <span>Kalıcı olarak silinsin mi?</span>
              <button type="button" className="btn-danger btn-sm" onClick={handleDelete} disabled={busy}>
                {busy ? 'Siliniyor...' : 'Evet, sil'}
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
            <button type="button" className="link-button danger" onClick={() => setConfirmingDelete(true)}>
              Sil
            </button>
          ))}
      </div>

      <p className="row-meta">
        {task.author} açtı · {formatDate(task.createdAt)}
      </p>
    </Modal>
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
      // Only what actually changed is sent. An omitted field is left alone
      // server-side, so this cannot clobber an edit somebody else made to
      // the other field while this form sat open.
      const changes: Partial<{ title: string; description: string }> = {}
      if (title.trim() !== task.title) changes.title = title.trim()
      if (description.trim() !== task.description) changes.description = description.trim()
      if (Object.keys(changes).length > 0) {
        await api.updateTask(repo, task.id, changes)
      }
      onSaved()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Görev kaydedilemedi')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={save} className="modal-form">
      <label className="field">
        <span className="field-label">Başlık</span>
        <input value={title} onChange={(e) => setTitle(e.target.value)} required autoFocus />
      </label>

      <label className="field">
        <span className="field-label">Açıklama</span>
        <textarea rows={5} value={description} onChange={(e) => setDescription(e.target.value)} />
      </label>

      {error && <p className="error">{error}</p>}

      <div className="modal-actions">
        <button type="button" className="btn-secondary" onClick={onCancel} disabled={saving}>
          Vazgeç
        </button>
        <button type="submit" className="btn-primary" disabled={saving || !title.trim()}>
          {saving ? 'Kaydediliyor...' : 'Kaydet'}
        </button>
      </div>
    </form>
  )
}
