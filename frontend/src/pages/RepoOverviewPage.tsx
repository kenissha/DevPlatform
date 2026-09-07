import { useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { Commit, MergeRequest, Task } from '../api/types'
import { CopyButton } from '../components/CopyButton'
import { BranchIcon, MergeIcon, RepoIcon, TaskIcon } from '../components/icons'
import { useAuth } from '../auth/AuthContext'
import { formatRelative, MR_STATUS_BADGE, MR_STATUS_LABELS, TASK_STATUS_BADGE, TASK_STATUS_LABELS } from '../labels'
import { cloneURL } from '../repos/clone'
import { useRepos } from '../repos/ReposContext'

const DESCRIPTION_MAX = 200

export function RepoOverviewPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const { user } = useAuth()
  const { descriptions, reload } = useRepos()
  const isAdmin = user?.role === 'admin'

  const [branches, setBranches] = useState<string[] | null>(null)
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [mergeRequests, setMergeRequests] = useState<MergeRequest[] | null>(null)
  const [commits, setCommits] = useState<Commit[] | null>(null)
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

  // Commits are fetched separately rather than joining the Promise.all
  // above: a repository with no commits yet is a normal state, and letting
  // that failure reject the batch would blank out the branches, tasks and
  // review requests along with it.
  useEffect(() => {
    setCommits(null)
    api
      .listCommits(repo, 5)
      .then(setCommits)
      .catch(() => setCommits([]))
  }, [repo])

  const openTasks = tasks?.filter((t) => t.status !== 'done') ?? []
  const openMrs = mergeRequests?.filter((m) => m.status === 'open') ?? []
  const url = cloneURL(repo)

  return (
    <div className="page repo-page">
      <div className="repo-hero">
        <div className="repo-hero-main">
          <h1 className="repo-title">
            <RepoIcon />
            {repo}
          </h1>
          <RepoDescription
            repo={repo}
            description={descriptions[repo]}
            editable={isAdmin}
            onSaved={reload}
          />
        </div>

        <div className="repo-hero-clone">
          <span className="repo-hero-clone-label">Klonlamak için</span>
          <div className="repo-card-clone">
            <code title={url}>{url}</code>
            <CopyButton value={url} label={`${repo} adresini kopyala`} />
          </div>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="repo-stats">
        <Stat
          icon={<BranchIcon />}
          value={branches?.length}
          label="branch"
          to={`/repos/${encodeURIComponent(repo)}/branches`}
        />
        <Stat
          icon={<TaskIcon />}
          value={tasks === null ? undefined : openTasks.length}
          label="açık görev"
          to={`/repos/${encodeURIComponent(repo)}/tasks`}
        />
        <Stat
          icon={<MergeIcon />}
          value={mergeRequests === null ? undefined : openMrs.length}
          label="açık inceleme"
          to={`/repos/${encodeURIComponent(repo)}/merge-requests`}
        />
      </div>

      <div className="repo-columns">
        <Panel
          title="Açık görevler"
          count={openTasks.length}
          to={`/repos/${encodeURIComponent(repo)}/tasks`}
          loading={tasks === null}
          empty={tasks !== null && openTasks.length === 0 ? 'Açık görev yok.' : null}
        >
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
        </Panel>

        <Panel
          title="Açık inceleme istekleri"
          count={openMrs.length}
          to={`/repos/${encodeURIComponent(repo)}/merge-requests`}
          loading={mergeRequests === null}
          empty={mergeRequests !== null && openMrs.length === 0 ? 'Açık inceleme isteği yok.' : null}
        >
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
        </Panel>
      </div>

      {/* Branches are short names, so they read far better as a wrapped
          row of chips than as a five-row vertical list that shows a
          fraction of them. The whole set fits at a glance. */}
      <section className="repo-section">
        <div className="section-title">
          <h2>Branch'ler</h2>
          <span className="badge badge-neutral">{branches?.length ?? 0}</span>
          <div className="spacer" />
          <Link to={`/repos/${encodeURIComponent(repo)}/branches`}>Tümü →</Link>
        </div>
        <div className="card">
          {branches === null && <p className="empty-state">Yükleniyor...</p>}
          {branches?.length === 0 && (
            <p className="empty-state">Henüz branch yok — ilk push'unu yaptığında burada görünecek.</p>
          )}
          {branches && branches.length > 0 && (
            <div className="branch-cloud">
              {branches.map((b) => (
                // Branch names can contain "/" (feature/hakem-raporlari) and
                // the detail route is a splat, so the name goes in raw —
                // encoding it would turn the slash into %2F and stop
                // matching. Same form RepoBranchesPage links with.
                <Link
                  key={b}
                  to={`/repos/${encodeURIComponent(repo)}/branches/${b}`}
                  className="branch-chip branch-chip-link"
                >
                  <BranchIcon />
                  {b}
                  {b === 'main' && <span className="branch-chip-tag">korumalı</span>}
                </Link>
              ))}
            </div>
          )}
        </div>
      </section>

      <section className="repo-section">
        <div className="section-title">
          <h2>Son commit'ler</h2>
          <div className="spacer" />
          <Link to={`/repos/${encodeURIComponent(repo)}/insights`}>İstatistikler →</Link>
        </div>
        <div className="card">
          {commits === null && <p className="empty-state">Yükleniyor...</p>}
          {commits?.length === 0 && <p className="empty-state">Henüz commit yok.</p>}
          {commits && commits.length > 0 && (
            <ul className="row-list">
              {commits.map((c) => (
                <li key={c.hash}>
                  <div className="row-main">
                    <code className="commit-hash">{c.shortHash}</code>
                    <span className="row-title">{c.message.split('\n')[0]}</span>
                    <div className="spacer" />
                    <span className="row-meta-inline">{formatRelative(c.when)}</span>
                  </div>
                  <p className="row-meta">{c.authorName}</p>
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>
    </div>
  )
}

function Stat({
  icon,
  value,
  label,
  to,
}: {
  icon: React.ReactNode
  value: number | undefined
  label: string
  to: string
}) {
  return (
    <Link to={to} className="repo-stat">
      <span className="repo-stat-icon">{icon}</span>
      <span className="repo-stat-value">{value ?? '—'}</span>
      <span className="repo-stat-label">{label}</span>
    </Link>
  )
}

function Panel({
  title,
  count,
  to,
  loading,
  empty,
  children,
}: {
  title: string
  count: number
  to: string
  loading: boolean
  empty: string | null
  children: React.ReactNode
}) {
  return (
    <section className="repo-panel">
      <div className="section-title">
        <h2>{title}</h2>
        <span className="badge badge-neutral">{count}</span>
        <div className="spacer" />
        <Link to={to}>Tümü →</Link>
      </div>
      <div className="card">
        {loading && <p className="empty-state">Yükleniyor...</p>}
        {!loading && empty && <p className="empty-state">{empty}</p>}
        {!loading && !empty && <ul className="row-list">{children}</ul>}
      </div>
    </section>
  )
}

// RepoDescription renders the description and, for an admin, doubles as
// its editor. Editing in place beats a separate settings screen for a
// single line of text — the person is already looking at the thing they
// want to change.
function RepoDescription({
  repo,
  description,
  editable,
  onSaved,
}: {
  repo: string
  description?: string
  editable: boolean
  onSaved: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  function startEditing() {
    setDraft(description ?? '')
    setSaveError(null)
    setEditing(true)
  }

  async function save(e: FormEvent) {
    e.preventDefault()
    setSaving(true)
    setSaveError(null)
    try {
      await api.setRepoDescription(repo, draft.trim())
      setEditing(false)
      onSaved()
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : 'Açıklama kaydedilemedi')
    } finally {
      setSaving(false)
    }
  }

  if (editing) {
    return (
      <form onSubmit={save} className="repo-desc-form">
        <input
          type="text"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="Bu depo ne işe yarıyor?"
          maxLength={DESCRIPTION_MAX}
          aria-label="Repo açıklaması"
          autoFocus
        />
        <button type="submit" className="btn-primary" disabled={saving}>
          {saving ? 'Kaydediliyor...' : 'Kaydet'}
        </button>
        <button type="button" className="btn-secondary" onClick={() => setEditing(false)} disabled={saving}>
          Vazgeç
        </button>
        {saveError && <p className="error">{saveError}</p>}
      </form>
    )
  }

  if (description) {
    return (
      <p className="repo-desc">
        {description}
        {editable && (
          <button type="button" className="link-button" onClick={startEditing}>
            düzenle
          </button>
        )}
      </p>
    )
  }

  // No description. Non-admins get nothing rather than an empty-looking
  // placeholder for something they can't act on.
  if (!editable) return null
  return (
    <p className="repo-desc repo-desc-empty">
      <button type="button" className="link-button" onClick={startEditing}>
        + Açıklama ekle
      </button>
    </p>
  )
}
