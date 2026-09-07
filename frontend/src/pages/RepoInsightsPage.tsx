import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api } from '../api/client'
import type { Commit, Contributor, DayCount } from '../api/types'
import { ContributionGraph } from '../components/ContributionGraph'
import { formatDate, formatRelative } from '../labels'

// How many commits the list holds. A repository can have thousands, and a
// page that renders all of them is a page you scroll past rather than
// read; the list is also boxed to a fixed height so it never pushes the
// rest of the screen away. Everything older lives in the repository
// itself, which is where a real archaeology session belongs anyway.
const COMMIT_LIMIT = 30

// The heatmap is drawn over half a year, not the 30 days the headline
// numbers cover: a week-per-column grid needs columns to be a grid at all,
// and five of them read as a stray block rather than as history. The
// summary tiles slice the last 30 days back out of the same response, so
// this stays one request.
const ACTIVITY_DAYS = 182
const SUMMARY_DAYS = 30

export function RepoInsightsPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const [activity, setActivity] = useState<DayCount[] | null>(null)
  const [contributors, setContributors] = useState<Contributor[] | null>(null)
  const [commits, setCommits] = useState<Commit[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setError(null)
    Promise.all([
      api.activity(repo, ACTIVITY_DAYS),
      api.listContributors(repo),
      api.listCommits(repo, COMMIT_LIMIT),
    ])
      .then(([a, c, cs]) => {
        setActivity(a)
        setContributors(c)
        setCommits(cs)
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [repo])

  const recent = activity?.slice(-SUMMARY_DAYS) ?? []
  const totalCommits = recent.reduce((sum, d) => sum + d.commits, 0)
  const busiest = recent.reduce((max, d) => Math.max(max, d.commits), 0)
  const activeDays = recent.filter((d) => d.commits > 0).length
  const yearTotal = activity?.reduce((sum, d) => sum + d.commits, 0) ?? 0

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>İstatistikler</h1>
          <p className="page-subtitle">{repo} deposunun aktivitesi</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="insight-stats">
        <Stat value={totalCommits} label="commit" hint="son 30 gün" />
        <Stat value={activeDays} label="aktif gün" hint="son 30 gün" />
        <Stat value={busiest} label="en yoğun gün" hint="commit sayısı" />
        <Stat value={contributors?.length ?? 0} label="katkıda bulunan" hint="tüm zamanlar" />
      </div>

      <div className="section-title">
        <h2>Commit aktivitesi</h2>
        <span className="muted" style={{ fontSize: 12 }}>
          son 6 ay
        </span>
      </div>
      <div className="card">
        <div className="card-body">
          {activity === null && <p className="empty-state">Yükleniyor...</p>}
          {activity && yearTotal === 0 && <p className="empty-state">Bu aralıkta commit yok.</p>}
          {activity && yearTotal > 0 && <ContributionGraph days={activity} />}
        </div>
      </div>

      <div className="section-title">
        <h2>Katkıda bulunanlar</h2>
        {contributors && contributors.length > 0 && (
          <span className="badge badge-neutral">{contributors.length}</span>
        )}
      </div>
      {contributors === null && (
        <div className="card">
          <p className="empty-state">Yükleniyor...</p>
        </div>
      )}
      {contributors?.length === 0 && (
        <div className="card">
          <p className="empty-state">Henüz commit yok.</p>
        </div>
      )}
      {contributors && contributors.length > 0 && (
        <div className="contributor-grid">
          {contributors.map((c) => (
            <div key={c.email} className="contributor-card">
              <span className="contributor-avatar">{initials(c.name)}</span>
              <p className="contributor-name" title={c.name}>
                {c.name}
              </p>
              <p className="contributor-email" title={c.email}>
                {c.email}
              </p>
              <p className="contributor-count">
                <strong>{c.commits}</strong> commit
              </p>
              <p className="contributor-last">son {formatRelative(c.lastAt)}</p>
            </div>
          ))}
        </div>
      )}

      <div className="section-title">
        <h2>Son commit'ler</h2>
        <span className="muted" style={{ fontSize: 12 }}>
          en yeni {COMMIT_LIMIT}
        </span>
      </div>
      <div className="card">
        {commits === null && <p className="empty-state">Yükleniyor...</p>}
        {commits?.length === 0 && <p className="empty-state">Henüz commit yok.</p>}
        {commits && commits.length > 0 && (
          <ul className="row-list scroll-list">
            {commits.map((c) => (
              <li key={c.hash}>
                <div className="row-main">
                  <span className="row-title">{firstLine(c.message)}</span>
                  <div className="spacer" />
                  <code className="commit-hash">{c.shortHash}</code>
                </div>
                <p className="row-meta">
                  {c.authorName}
                  <span>·</span>
                  {formatDate(c.when)}
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function Stat({ value, label, hint }: { value: number; label: string; hint: string }) {
  return (
    <div className="insight-stat">
      <div className="insight-stat-value">{value}</div>
      <div className="insight-stat-label">{label}</div>
      <div className="insight-stat-hint">{hint}</div>
    </div>
  )
}

// Two letters from the name, taking the first letter of each of the first
// two words when there are two — "Rifat Öztürk" reads as RÖ, not RI.
function initials(name: string): string {
  const parts = name.trim().split(/\s+/)
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase()
  return name.slice(0, 2).toUpperCase()
}

// Commit messages carry their full body; a list row shows the subject only.
function firstLine(message: string): string {
  return message.split('\n')[0]
}
