import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api } from '../api/client'
import type { Commit, Contributor, DayCount } from '../api/types'
import { ActivityChart } from '../components/ActivityChart'
import { formatDate } from '../labels'

export function RepoInsightsPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const [activity, setActivity] = useState<DayCount[] | null>(null)
  const [contributors, setContributors] = useState<Contributor[] | null>(null)
  const [commits, setCommits] = useState<Commit[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setError(null)
    Promise.all([api.activity(repo, 30), api.listContributors(repo), api.listCommits(repo, 15)])
      .then(([a, c, cs]) => {
        setActivity(a)
        setContributors(c)
        setCommits(cs)
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [repo])

  const totalCommits = activity?.reduce((sum, d) => sum + d.commits, 0) ?? 0

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>İstatistikler</h1>
          <p className="page-subtitle">{repo} deposunun aktivitesi</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="stat-grid">
        <div className="stat-tile">
          <div className="stat-label">Son 30 günde commit</div>
          <div className="stat-value">{totalCommits}</div>
        </div>
        <div className="stat-tile">
          <div className="stat-label">Katkıda bulunan</div>
          <div className="stat-value">{contributors?.length ?? 0}</div>
        </div>
      </div>

      <div className="section-title">
        <h2>Günlük commit aktivitesi</h2>
        <span className="muted" style={{ fontSize: 12 }}>son 30 gün</span>
      </div>
      <div className="card">
        <div className="card-body">
          {activity === null && <p className="empty-state">Yükleniyor...</p>}
          {activity && totalCommits === 0 && (
            <p className="empty-state">Son 30 günde commit yok.</p>
          )}
          {activity && totalCommits > 0 && <ActivityChart days={activity} />}
        </div>
      </div>

      <div className="section-title">
        <h2>Katkıda bulunanlar</h2>
      </div>
      <div className="card">
        {contributors === null && <p className="empty-state">Yükleniyor...</p>}
        {contributors?.length === 0 && <p className="empty-state">Henüz commit yok.</p>}
        {contributors && contributors.length > 0 && (
          <ul className="row-list">
            {contributors.map((c) => (
              <li key={c.email}>
                <div className="row-main">
                  <span className="avatar">{c.name.slice(0, 2)}</span>
                  <span className="row-title">{c.name}</span>
                  <div className="spacer" />
                  <span className="badge badge-neutral">{c.commits} commit</span>
                </div>
                <p className="row-meta">
                  {c.email}
                  <span>·</span>
                  son: {formatDate(c.lastAt)}
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>Son commit'ler</h2>
      </div>
      <div className="card">
        {commits === null && <p className="empty-state">Yükleniyor...</p>}
        {commits?.length === 0 && <p className="empty-state">Henüz commit yok.</p>}
        {commits && commits.length > 0 && (
          <ul className="row-list">
            {commits.map((c) => (
              <li key={c.hash}>
                <div className="row-main">
                  <span className="commit-msg">{firstLine(c.message)}</span>
                  <div className="spacer" />
                  <span className="commit-hash">{c.shortHash}</span>
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

// Commit messages carry their full body; a list row shows the subject only.
function firstLine(message: string): string {
  return message.split('\n')[0]
}
