import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import type {
  AuditAction,
  AuditEvent,
  Contributions,
  DeploymentRequest,
  MergeRequest,
  Person,
  Task,
} from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { ContributionGraph } from '../components/ContributionGraph'
import {
  AuditIcon,
  CheckIcon,
  DeployIcon,
  MergeIcon,
  PlusIcon,
  RepoIcon,
  TaskIcon,
} from '../components/icons'
import { useRepos } from '../repos/ReposContext'
import { formatDayHeading, formatRelative, greeting } from '../labels'

// The platform's landing page, written as a personal dashboard rather
// than a set of tables: what is waiting on *me* first, then what the
// team has been doing — the design doc's "kimin ne üzerinde çalıştığını
// görebilmek" goal, answered without opening each repo in turn.
//
// Every panel here reads from an endpoint that already existed; the
// dashboard adds no server-side aggregation of its own. The one thing
// it does need is names: audit events and task assignments carry a
// subject id, so /api/users is fetched purely to turn "7" into "Rifat
// Öztürk" (see userResponse in backend/internal/server).
export function DashboardPage() {
  const { user } = useAuth()
  const { repos } = useRepos()
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [mergeRequests, setMergeRequests] = useState<MergeRequest[] | null>(null)
  const [deployments, setDeployments] = useState<DeploymentRequest[] | null>(null)
  const [events, setEvents] = useState<AuditEvent[] | null>(null)
  const [people, setPeople] = useState<Person[] | null>(null)
  const [contributions, setContributions] = useState<Contributions | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    Promise.all([
      api.listAllTasks(),
      api.listAllMergeRequests('open'),
      api.listAllDeployments('pending'),
      api.listAudit(40),
      api.listPeople(),
    ])
      .then(([t, m, d, a, p]) => {
        setTasks(t)
        setMergeRequests(m)
        setDeployments(d)
        setEvents(a)
        setPeople(p)
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  // Fetched separately from the batch above: it walks a year of git
  // history across every repo, so it's the slowest call on the page —
  // letting it resolve on its own keeps the rest of the dashboard from
  // waiting on it. A failure here leaves the graph out rather than
  // failing the whole page.
  useEffect(() => {
    api
      .myContributions()
      .then(setContributions)
      .catch(() => setContributions({ days: [], total: 0 }))
  }, [])

  // subject -> readable name. Falls back to the subject itself so an
  // actor who somehow isn't in the registry still renders as something,
  // rather than vanishing into an empty string.
  const nameOf = useMemo(() => {
    const bySubject = new Map((people ?? []).map((p) => [p.subject, p.displayName]))
    return (subject: string) => bySubject.get(subject) || subject || 'Bilinmiyor'
  }, [people])

  const openTasks = useMemo(() => tasks?.filter((t) => t.status !== 'done') ?? [], [tasks])
  const myTasks = openTasks.filter((t) => t.assignedTo === user?.subject)
  const myUrgent = myTasks.filter((t) => t.urgent)

  // Open work per person, busiest first, with unassigned pinned last —
  // it's a bucket, not a colleague.
  const workload = useMemo(() => {
    const byPerson = new Map<string, Task[]>()
    for (const t of openTasks) {
      const key = t.assignedTo || ''
      byPerson.set(key, [...(byPerson.get(key) ?? []), t])
    }
    return [...byPerson.entries()].sort((a, b) => {
      if (a[0] === '') return 1
      if (b[0] === '') return -1
      return b[1].length - a[1].length
    })
  }, [openTasks])

  const busiest = workload.length > 0 ? Math.max(...workload.map(([, list]) => list.length)) : 0

  const openTasksPerRepo = useMemo(() => {
    const counts = new Map<string, number>()
    for (const t of openTasks) counts.set(t.repo, (counts.get(t.repo) ?? 0) + 1)
    return counts
  }, [openTasks])

  const loading = tasks === null
  const shownName = user?.displayName || user?.email || ''
  // Just the first word: "Günaydın, Rifat" reads like a person talking,
  // "Günaydın, Rifat Öztürk" reads like a form letter.
  const firstName = shownName.split(/[\s@]/)[0]

  return (
    <div className="page dashboard">
      <header className="hero">
        <div>
          <h1>
            {greeting()}
            {firstName && <>, {firstName}</>}
          </h1>
          <p className="hero-sub">
            {formatDayHeading()}
            {!loading && <> · {summarise(myTasks.length, mergeRequests?.length ?? 0)}</>}
          </p>
        </div>
      </header>

      {error && <p className="error">{error}</p>}

      {myUrgent.length > 0 && (
        <div className="alert-strip">
          <strong>{myUrgent.length} acil görev</strong> üzerinize atanmış.
        </div>
      )}

      <div className="focus-grid">
        <FocusCard
          tone="accent"
          icon={<TaskIcon />}
          label="Bana atanan görev"
          count={myTasks.length}
          loading={loading}
          empty="Üzerinize atanmış açık görev yok."
          items={myTasks.map((t) => ({
            key: t.id,
            to: `/repos/${encodeURIComponent(t.repo)}/tasks`,
            title: t.title,
            meta: t.repo,
            badge: t.urgent ? { text: 'Acil', className: 'badge-danger' } : undefined,
          }))}
        />
        <FocusCard
          tone="warn"
          icon={<MergeIcon />}
          label="İnceleme bekleyen"
          count={mergeRequests?.length ?? 0}
          loading={mergeRequests === null}
          empty="Bekleyen inceleme isteği yok."
          items={(mergeRequests ?? []).map((mr) => ({
            key: `${mr.repo}-${mr.id}`,
            to: `/repos/${encodeURIComponent(mr.repo)}/merge-requests/${mr.id}`,
            title: mr.title,
            meta: `${mr.repo} · ${nameOf(mr.author)}`,
          }))}
        />
        <FocusCard
          tone="success"
          icon={<DeployIcon />}
          label="Onay bekleyen deploy"
          count={deployments?.length ?? 0}
          loading={deployments === null}
          empty="Bekleyen deploy isteği yok."
          items={(deployments ?? []).map((d) => ({
            key: `${d.repo}-${d.id}`,
            to: `/repos/${encodeURIComponent(d.repo)}/deployments`,
            title: `${d.repo} → ${d.environment}`,
            meta: nameOf(d.author),
          }))}
        />
      </div>

      <div className="section-title">
        <h2>Katkıların</h2>
        {contributions && contributions.days.length > 0 && (
          <span className="muted" style={{ fontSize: 13 }}>
            son bir yılda {contributions.total} commit
          </span>
        )}
      </div>
      <div className="card">
        <div className="card-body">
          {contributions === null && <p className="empty-state">Yükleniyor...</p>}
          {contributions && contributions.days.length === 0 && (
            <p className="empty-state">Katkı geçmişi okunamadı.</p>
          )}
          {contributions && contributions.days.length > 0 && (
            <>
              <ContributionGraph days={contributions.days} />
              {contributions.total === 0 && (
                <p className="muted" style={{ fontSize: 12, marginTop: 10 }}>
                  Henüz commit görünmüyor. Commit'ler git yazar e-postasıyla eşleştiriliyor — yerel{' '}
                  <code>git config user.email</code> ayarın panel hesabındaki e-postayla aynı değilse burada
                  görünmezler.
                </p>
              )}
            </>
          )}
        </div>
      </div>

      <div className="dash-columns">
        <section>
          <div className="section-title">
            <h2>Son hareketler</h2>
          </div>
          <div className="card">
            {events === null && <p className="empty-state">Yükleniyor...</p>}
            {events?.length === 0 && <p className="empty-state">Henüz bir hareket yok.</p>}
            {events && events.length > 0 && (
              <ul className="feed">
                {events.slice(0, 15).map((e, i) => (
                  <li key={`${e.at}-${i}`}>
                    <span className={`feed-icon ${ACTION_TONE[e.action] ?? 'tone-neutral'}`}>
                      {ACTION_ICON[e.action] ?? <AuditIcon />}
                    </span>
                    <div className="feed-body">
                      <p className="feed-text">{e.summary || e.action}</p>
                      <p className="feed-meta">
                        <strong>{nameOf(e.actor)}</strong>
                        {e.repo && (
                          <>
                            <span>·</span>
                            <Link to={`/repos/${encodeURIComponent(e.repo)}`}>{e.repo}</Link>
                          </>
                        )}
                        <span>·</span>
                        {formatRelative(e.at)}
                      </p>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
          {events && events.length > 0 && (
            <p className="muted" style={{ fontSize: 13 }}>
              <Link to="/audit">Tüm denetim kaydı →</Link>
            </p>
          )}
        </section>

        <aside className="dash-rail">
          <div className="section-title">
            <h2>Ekip</h2>
          </div>
          <div className="card">
            {loading && <p className="empty-state">Yükleniyor...</p>}
            {!loading && workload.length === 0 && <p className="empty-state">Açık görev yok.</p>}
            {workload.map(([subject, list]) => (
              <div key={subject || 'unassigned'} className="workload-row">
                <span className="avatar">{(subject ? nameOf(subject) : '?').slice(0, 2)}</span>
                <div className="workload-body">
                  <div className="workload-head">
                    <span className="workload-name">{subject ? nameOf(subject) : 'Atanmamış'}</span>
                    <span className="muted mono">{list.length}</span>
                  </div>
                  <div className="workload-bar">
                    <span
                      className={subject ? undefined : 'unassigned'}
                      style={{ width: `${busiest ? (list.length / busiest) * 100 : 0}%` }}
                    />
                  </div>
                </div>
              </div>
            ))}
          </div>

          <div className="section-title">
            <h2>Repolar</h2>
          </div>
          <div className="card">
            {repos === null && <p className="empty-state">Yükleniyor...</p>}
            {repos?.length === 0 && <p className="empty-state">Henüz repo yok.</p>}
            {repos && repos.length > 0 && (
              <ul className="row-list">
                {repos.map((name) => (
                  <li key={name}>
                    <div className="row-main">
                      <Link to={`/repos/${encodeURIComponent(name)}`} className="row-title repo-link">
                        <RepoIcon />
                        {name}
                      </Link>
                      <div className="spacer" />
                      {openTasksPerRepo.get(name) ? (
                        <span className="badge badge-neutral">{openTasksPerRepo.get(name)} görev</span>
                      ) : null}
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </aside>
      </div>
    </div>
  )
}

function summarise(myTaskCount: number, reviewCount: number): string {
  const parts: string[] = []
  if (myTaskCount > 0) parts.push(`${myTaskCount} açık görevin`)
  if (reviewCount > 0) parts.push(`${reviewCount} bekleyen inceleme`)
  if (parts.length === 0) return 'bekleyen bir işin yok'
  return `${parts.join(' ve ')} var`
}

// Per-action icon and colour for the activity feed. Keyed by the same
// AuditAction union labels.ts uses, so a new action added there is a
// compile-time prompt to give it a glyph here too — but the feed falls
// back to a neutral icon rather than breaking if one is missed.
const ACTION_ICON: Partial<Record<AuditAction, ReactNode>> = {
  'repo.created': <RepoIcon />,
  'task.created': <PlusIcon />,
  'task.updated': <TaskIcon />,
  'merge_request.opened': <MergeIcon />,
  'merge_request.approved': <CheckIcon />,
  'merge_request.rejected': <MergeIcon />,
  'deployment.opened': <DeployIcon />,
  'deployment.deployed': <CheckIcon />,
  'deployment.failed': <DeployIcon />,
  'deployment.rejected': <DeployIcon />,
}

const ACTION_TONE: Partial<Record<AuditAction, string>> = {
  'repo.created': 'tone-accent',
  'task.created': 'tone-accent',
  'task.updated': 'tone-neutral',
  'merge_request.opened': 'tone-accent',
  'merge_request.approved': 'tone-success',
  'merge_request.rejected': 'tone-danger',
  'deployment.opened': 'tone-accent',
  'deployment.deployed': 'tone-success',
  'deployment.failed': 'tone-danger',
  'deployment.rejected': 'tone-danger',
}

type FocusItem = {
  key: string
  to: string
  title: string
  meta: string
  badge?: { text: string; className: string }
}

// One "what's waiting on you" card: a headline count plus the first few
// actual items, so the number is a way into the work rather than a
// statistic to admire. Deliberately shows at most three and counts the
// rest — a dashboard card that grows without bound stops being a summary.
function FocusCard({
  tone,
  icon,
  label,
  count,
  items,
  empty,
  loading,
}: {
  tone: 'accent' | 'warn' | 'success'
  icon: ReactNode
  label: string
  count: number
  items: FocusItem[]
  empty: string
  loading: boolean
}) {
  const shown = items.slice(0, 3)
  const rest = items.length - shown.length

  return (
    <div className={`focus-card tone-${tone}`}>
      <div className="focus-head">
        <span className="focus-icon">{icon}</span>
        <span className="focus-label">{label}</span>
        <span className="focus-count">{loading ? '—' : count}</span>
      </div>
      {loading && <p className="focus-empty">Yükleniyor...</p>}
      {!loading && shown.length === 0 && <p className="focus-empty">{empty}</p>}
      {shown.length > 0 && (
        <ul className="focus-list">
          {shown.map((item) => (
            <li key={item.key}>
              <Link to={item.to}>
                <span className="focus-item-title">{item.title}</span>
                {item.badge && <span className={`badge ${item.badge.className}`}>{item.badge.text}</span>}
              </Link>
              <span className="focus-item-meta">{item.meta}</span>
            </li>
          ))}
        </ul>
      )}
      {rest > 0 && <p className="focus-more">+{rest} tane daha</p>}
    </div>
  )
}
