import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import type { AuditAction, AuditEvent } from '../api/types'
import { AuditIcon, DeployIcon, MergeIcon, RepoIcon, TaskIcon } from '../components/icons'
import { AUDIT_ACTION_LABELS, dayHeading, formatTime, groupByDay } from '../labels'

// Grouped into families rather than one filter per action: nobody comes
// here asking "show me deployment.rolled_back", they ask "what happened
// with deploys". The keys are prefixes of the action strings the backend
// writes (see internal/audit).
const FILTERS: { key: string; label: string; match: (a: AuditAction) => boolean }[] = [
  { key: 'all', label: 'Hepsi', match: () => true },
  { key: 'task', label: 'Görevler', match: (a) => a.startsWith('task.') },
  { key: 'mr', label: 'İncelemeler', match: (a) => a.startsWith('merge_request.') },
  { key: 'deploy', label: 'Deploy', match: (a) => a.startsWith('deployment.') },
  { key: 'repo', label: 'Repolar', match: (a) => a.startsWith('repo.') },
]

const ACTION_ICON: Record<string, React.ReactNode> = {
  'repo.created': <RepoIcon />,
  'task.created': <TaskIcon />,
  'task.updated': <TaskIcon />,
  'task.deleted': <TaskIcon />,
  'merge_request.opened': <MergeIcon />,
  'merge_request.approved': <MergeIcon />,
  'merge_request.rejected': <MergeIcon />,
  'deployment.opened': <DeployIcon />,
  'deployment.deployed': <DeployIcon />,
  'deployment.failed': <DeployIcon />,
  'deployment.rejected': <DeployIcon />,
  'deployment.rolled_back': <DeployIcon />,
}

// Colour carries outcome, not category: green happened, red was refused
// or broke, amber is waiting on somebody. Scanning a long log for "what
// went wrong" should not require reading it.
const ACTION_TONE: Record<string, string> = {
  'repo.created': 'tone-neutral',
  'task.created': 'tone-neutral',
  'task.updated': 'tone-neutral',
  'task.deleted': 'tone-danger',
  'merge_request.opened': 'tone-accent',
  'merge_request.approved': 'tone-success',
  'merge_request.rejected': 'tone-danger',
  'deployment.opened': 'tone-warn',
  'deployment.deployed': 'tone-success',
  'deployment.failed': 'tone-danger',
  'deployment.rejected': 'tone-danger',
  'deployment.rolled_back': 'tone-warn',
}

export function AuditPage() {
  const [events, setEvents] = useState<AuditEvent[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState('all')

  useEffect(() => {
    api
      .listAudit(200)
      .then(setEvents)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  const active = FILTERS.find((f) => f.key === filter) ?? FILTERS[0]
  const shown = useMemo(
    () => (events ?? []).filter((e) => active.match(e.action)),
    [events, active],
  )
  const groups = groupByDay(shown, (e) => e.at)

  return (
    <div className="page feed-page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Denetim kaydı</h1>
          <p className="page-subtitle">
            Kim, ne zaman, neyi. Kayıtlar yalnızca eklenir — hiçbir satır sonradan değiştirilmez.
          </p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="filter-bar" role="tablist" aria-label="Kayıt türü">
        {FILTERS.map((f) => {
          const count = (events ?? []).filter((e) => f.match(e.action)).length
          return (
            <button
              key={f.key}
              type="button"
              role="tab"
              aria-selected={f.key === filter}
              className={f.key === filter ? 'filter-chip active' : 'filter-chip'}
              onClick={() => setFilter(f.key)}
            >
              {f.label}
              {events && <span className="filter-count">{count}</span>}
            </button>
          )
        })}
      </div>

      {events === null && (
        <div className="card">
          <p className="empty-state">Yükleniyor...</p>
        </div>
      )}
      {events && shown.length === 0 && (
        <div className="card">
          <p className="empty-state">
            {events.length === 0 ? 'Henüz kayıtlı işlem yok.' : 'Bu türde kayıt yok.'}
          </p>
        </div>
      )}

      {groups.map(([key, items]) => (
        <section key={key} className="day-group">
          <h2 className="day-heading">{dayHeading(items[0].at)}</h2>
          {/* A timeline, not a table: these are events in sequence, and
              the rail makes "one after another" the thing the eye follows
              instead of a boundary between rows. */}
          <ol className="timeline">
            {items.map((e, i) => (
              <li key={`${e.at}-${i}`}>
                <span className={`timeline-dot ${ACTION_TONE[e.action] ?? 'tone-neutral'}`}>
                  {ACTION_ICON[e.action] ?? <AuditIcon />}
                </span>
                <div className="timeline-body">
                  <p className="timeline-text">{e.summary || AUDIT_ACTION_LABELS[e.action] || e.action}</p>
                  <p className="timeline-meta">
                    <strong>{e.actor}</strong>
                    {e.repo && (
                      <>
                        <span>·</span>
                        <Link to={`/repos/${encodeURIComponent(e.repo)}`}>{e.repo}</Link>
                      </>
                    )}
                  </p>
                </div>
                <span className="timeline-time">{formatTime(e.at)}</span>
              </li>
            ))}
          </ol>
        </section>
      ))}
    </div>
  )
}
