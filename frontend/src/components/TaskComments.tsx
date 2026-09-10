import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError } from '../api/client'
import type { Person, TaskComment } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { formatRelative } from '../labels'

// The conversation about a piece of work, kept with the work.
//
// Separate from the audit timeline beside it on purpose: the log records
// what the system did, this records what people said. Mixing them would
// bury a question under twenty status changes.
export function TaskComments({
  repo,
  taskId,
  people,
}: {
  repo: string
  taskId: string
  people: Person[]
}) {
  const { user } = useAuth()
  const [comments, setComments] = useState<TaskComment[] | null>(null)
  const [body, setBody] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)

  useEffect(() => {
    api
      .taskComments(repo, taskId)
      .then(setComments)
      .catch((err) => setError(err instanceof ApiError ? err.message : 'Yorumlar yüklenemedi'))
  }, [repo, taskId])

  function personLabel(subject: string): string {
    return people.find((p) => p.subject === subject)?.email || subject
  }

  async function run(action: () => Promise<unknown>, failure: string) {
    setBusy(true)
    setError(null)
    try {
      await action()
      setComments(await api.taskComments(repo, taskId))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : failure)
    } finally {
      setBusy(false)
    }
  }

  async function post(e: FormEvent) {
    e.preventDefault()
    if (!body.trim()) return
    await run(() => api.addTaskComment(repo, taskId, body.trim()), 'Yorum eklenemedi')
    setBody('')
  }

  return (
    <section className="comments">
      <div className="section-title">
        <h2>Yorumlar</h2>
        {comments && comments.length > 0 && (
          <span className="badge badge-neutral">{comments.length}</span>
        )}
      </div>

      {comments === null && (
        <div className="card">
          <p className="empty-state">Yükleniyor...</p>
        </div>
      )}

      {comments?.length === 0 && (
        <div className="card">
          <p className="empty-state">Henüz yorum yok. İlk sözü sen söyle.</p>
        </div>
      )}

      {comments && comments.length > 0 && (
        <ul className="comment-list">
          {comments.map((c) => (
            <li key={c.id} className="comment">
              <span className="comment-avatar">{initials(personLabel(c.author))}</span>
              <div className="comment-body">
                <p className="comment-head">
                  <strong>{personLabel(c.author)}</strong>
                  <span className="comment-when">{formatRelative(c.at)}</span>
                  {/* An edited comment says so — otherwise the record can
                      be rewritten without anyone noticing. */}
                  {c.editedAt && <span className="comment-edited">düzenlendi</span>}
                  <span className="spacer" />
                  {c.author === user?.subject && editingId !== c.id && (
                    <button type="button" className="link-button" onClick={() => setEditingId(c.id)}>
                      Düzenle
                    </button>
                  )}
                  {(c.author === user?.subject || user?.role === 'admin') && (
                    <button
                      type="button"
                      className="link-button danger"
                      disabled={busy}
                      onClick={() => {
                        if (!confirm('Bu yorum silinsin mi?')) return
                        run(() => api.deleteTaskComment(repo, taskId, c.id), 'Silinemedi')
                      }}
                    >
                      Sil
                    </button>
                  )}
                </p>
                {editingId === c.id ? (
                  <CommentEditor
                    initial={c.body}
                    busy={busy}
                    onCancel={() => setEditingId(null)}
                    onSave={async (next) => {
                      await run(
                        () => api.editTaskComment(repo, taskId, c.id, next),
                        'Kaydedilemedi',
                      )
                      setEditingId(null)
                    }}
                  />
                ) : (
                  <p className="comment-text">{c.body}</p>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      {error && <p className="error">{error}</p>}

      <form onSubmit={post} className="comment-form">
        <textarea
          rows={3}
          value={body}
          onChange={(e) => setBody(e.target.value)}
          placeholder="Bu iş hakkında bir şey yaz..."
          disabled={busy}
        />
        <div className="form-actions">
          <button type="submit" className="btn-primary btn-sm" disabled={busy || !body.trim()}>
            {busy ? 'Gönderiliyor...' : 'Yorum yap'}
          </button>
          <span className="field-hint">Atanan kişiye ve görevi açana bildirim gider.</span>
        </div>
      </form>
    </section>
  )
}

function CommentEditor({
  initial,
  busy,
  onCancel,
  onSave,
}: {
  initial: string
  busy: boolean
  onCancel: () => void
  onSave: (body: string) => void
}) {
  const [draft, setDraft] = useState(initial)

  return (
    <div className="comment-form">
      <textarea
        rows={3}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        disabled={busy}
        autoFocus
      />
      <div className="form-actions">
        <button
          type="button"
          className="btn-primary btn-sm"
          disabled={busy || !draft.trim()}
          onClick={() => onSave(draft.trim())}
        >
          Kaydet
        </button>
        <button type="button" className="btn-secondary btn-sm" onClick={onCancel} disabled={busy}>
          Vazgeç
        </button>
      </div>
    </div>
  )
}

function initials(label: string): string {
  const parts = label.trim().split(/\s+/)
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase()
  return label.slice(0, 2).toUpperCase()
}
