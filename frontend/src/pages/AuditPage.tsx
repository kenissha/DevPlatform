import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import type { AuditEvent } from '../api/types'
import { AUDIT_ACTION_BADGE, AUDIT_ACTION_LABELS, formatDate } from '../labels'

export function AuditPage() {
  const [events, setEvents] = useState<AuditEvent[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api
      .listAudit(200)
      .then(setEvents)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Denetim kaydı</h1>
          <p className="page-subtitle">
            Platformda yapılan her işlem — kim, ne zaman, neyi. Kayıtlar yalnızca eklenir, hiçbir
            satır sonradan değiştirilmez.
          </p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="card">
        {events === null && <p className="empty-state">Yükleniyor...</p>}
        {events?.length === 0 && <p className="empty-state">Henüz kayıtlı işlem yok.</p>}
        {events && events.length > 0 && (
          <ul className="row-list">
            {events.map((e, i) => (
              <li key={`${e.at}-${i}`}>
                <div className="row-main">
                  <span className={`badge ${AUDIT_ACTION_BADGE[e.action] ?? 'badge-neutral'}`}>
                    {AUDIT_ACTION_LABELS[e.action] ?? e.action}
                  </span>
                  <span className="row-title">{e.summary}</span>
                </div>
                <p className="row-meta">
                  <span className="avatar">{e.actor.slice(0, 2)}</span>
                  {e.actor}
                  {e.repo && (
                    <>
                      <span>·</span>
                      <Link to={`/repos/${encodeURIComponent(e.repo)}`}>{e.repo}</Link>
                    </>
                  )}
                  <span>·</span>
                  {formatDate(e.at)}
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
