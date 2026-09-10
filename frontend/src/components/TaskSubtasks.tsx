import { useState, type FormEvent } from 'react'
import { api, ApiError } from '../api/client'
import type { Task } from '../api/types'

// A checklist on the task, not a set of child tasks — see
// backend/internal/taskboard/subtasks.go for why.
//
// Every call returns the whole updated task, so the parent just swaps its
// state in; there is no separate list to keep in step.
export function TaskSubtasks({
  repo,
  task,
  onChanged,
}: {
  repo: string
  task: Task
  onChanged: (task: Task) => void
}) {
  const [title, setTitle] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)

  const subtasks = task.subtasks ?? []
  const done = subtasks.filter((s) => s.done).length

  async function run(action: () => Promise<Task>, failure: string) {
    setBusy(true)
    setError(null)
    try {
      onChanged(await action())
    } catch (err) {
      setError(err instanceof ApiError ? err.message : failure)
    } finally {
      setBusy(false)
    }
  }

  async function add(e: FormEvent) {
    e.preventDefault()
    if (!title.trim()) return
    await run(() => api.addSubtask(repo, task.id, title.trim()), 'Eklenemedi')
    setTitle('')
  }

  return (
    <section className="subtasks">
      <div className="section-title">
        <h2>Alt görevler</h2>
        {subtasks.length > 0 && (
          <span className="badge badge-neutral">
            {done}/{subtasks.length}
          </span>
        )}
        <div className="spacer" />
        {!adding && (
          <button type="button" className="link-button" onClick={() => setAdding(true)}>
            + Madde ekle
          </button>
        )}
      </div>

      <div className="card">
        {subtasks.length === 0 && !adding && (
          <p className="empty-state">
            Bu işi parçalara bölmek istersen buraya madde ekleyebilirsin.
          </p>
        )}

        {subtasks.length > 0 && (
          <>
            {/* The bar is the point of a checklist: how far along, at a
                glance, without counting ticks. */}
            <div className="subtask-progress" aria-hidden="true">
              <span style={{ width: `${(done / subtasks.length) * 100}%` }} />
            </div>
            <ul className="subtask-list">
              {subtasks.map((s) => (
                <li key={s.id} className={s.done ? 'is-done' : undefined}>
                  <label className="subtask-item">
                    <input
                      type="checkbox"
                      checked={s.done}
                      disabled={busy}
                      onChange={(e) =>
                        run(() => api.setSubtaskDone(repo, task.id, s.id, e.target.checked), 'Güncellenemedi')
                      }
                    />
                    <span>{s.title}</span>
                  </label>
                  <button
                    type="button"
                    className="link-button danger"
                    disabled={busy}
                    onClick={() => run(() => api.removeSubtask(repo, task.id, s.id), 'Silinemedi')}
                  >
                    Sil
                  </button>
                </li>
              ))}
            </ul>
          </>
        )}

        {adding && (
          <form onSubmit={add} className="subtask-form">
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Ne yapılması gerekiyor?"
              disabled={busy}
              autoFocus
            />
            <button type="submit" className="btn-primary btn-sm" disabled={busy || !title.trim()}>
              Ekle
            </button>
            <button
              type="button"
              className="btn-secondary btn-sm"
              onClick={() => {
                setAdding(false)
                setTitle('')
              }}
            >
              Kapat
            </button>
          </form>
        )}

        {error && (
          <p className="error" style={{ padding: '0 16px 12px' }}>
            {error}
          </p>
        )}
      </div>
    </section>
  )
}
