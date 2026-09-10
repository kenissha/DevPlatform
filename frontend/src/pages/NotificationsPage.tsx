import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import { BellIcon, CheckIcon, DeployIcon, MergeIcon, TaskIcon } from '../components/icons'
import { NOTIFICATION_KIND_LABELS, dayHeading, formatTime, groupByDay } from '../labels'
import { useNotifications } from '../notifications/useNotifications'

// One glyph per notification kind, so an inbox is scanned by shape before
// it is read. Unknown kinds fall back to the bell — the backend can ship a
// new kind without this file changing, the same way
// NOTIFICATION_KIND_LABELS falls back to the raw string.
const KIND_ICON: Record<string, React.ReactNode> = {
  task_assigned: <TaskIcon />,
  merge_request_opened: <MergeIcon />,
  merge_request_decided: <MergeIcon />,
  deployment_opened: <DeployIcon />,
  deployment_decided: <DeployIcon />,
  task_commented: <TaskIcon />,
}

const KIND_TONE: Record<string, string> = {
  task_assigned: 'tone-accent',
  merge_request_opened: 'tone-accent',
  merge_request_decided: 'tone-neutral',
  deployment_opened: 'tone-warn',
  deployment_decided: 'tone-neutral',
  task_commented: 'tone-accent',
}

export function NotificationsPage() {
  const { notifications, error, markRead, reload } = useNotifications()
  const [clearing, setClearing] = useState(false)

  const unread = notifications?.filter((n) => !n.read) ?? []
  const read = notifications?.filter((n) => n.read) ?? []
  const groups = groupByDay(notifications ?? [], (n) => n.createdAt)

  return (
    <div className="page feed-page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Bildirimler</h1>
          <p className="page-subtitle">
            {notifications === null
              ? 'Size atanan işler ve karar bekleyen istekler'
              : unread.length > 0
                ? `${unread.length} okunmamış`
                : 'Hepsi okundu'}
          </p>
        </div>
        <div className="notice-actions">
          {unread.length > 0 && (
            <button
              type="button"
              className="btn-secondary"
              onClick={() => unread.forEach((n) => markRead(n.id))}
            >
              <CheckIcon /> Tümünü okundu işaretle
            </button>
          )}
          {/* Only the read ones, and only in bulk: clearing something you
              have not looked at is how a request quietly goes unanswered.
              The server enforces the same rule. */}
          {read.length > 0 && (
            <button
              type="button"
              className="link-button"
              disabled={clearing}
              onClick={async () => {
                if (!confirm(`Okunmuş ${read.length} bildirim silinsin mi?`)) return
                setClearing(true)
                try {
                  await api.clearReadNotifications()
                  reload()
                } finally {
                  setClearing(false)
                }
              }}
            >
              Okunanları temizle
            </button>
          )}
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {notifications === null && (
        <div className="card">
          <p className="empty-state">Yükleniyor...</p>
        </div>
      )}
      {notifications?.length === 0 && (
        <div className="card">
          <div className="card-body empty-cta">
            <BellIcon className="empty-cta-icon" />
            <p className="empty-cta-title">Bildirim yok</p>
            <p className="empty-cta-sub">
              Sana bir görev atandığında, incelemen için bir istek açıldığında ya da açtığın bir
              istek karara bağlandığında burada görürsün.
            </p>
          </div>
        </div>
      )}

      {groups.map(([key, items]) => (
        <section key={key} className="day-group">
          <h2 className="day-heading">{dayHeading(items[0].createdAt)}</h2>
          <div className="card">
            <ul className="notice-list">
              {items.map((n) => {
                const body = (
                  <>
                    <span className={`feed-icon ${KIND_TONE[n.kind] ?? 'tone-neutral'}`}>
                      {KIND_ICON[n.kind] ?? <BellIcon />}
                    </span>
                    <div className="notice-body">
                      <p className="notice-text">{n.message}</p>
                      <p className="notice-meta">
                        {NOTIFICATION_KIND_LABELS[n.kind] ?? n.kind}
                        <span>·</span>
                        {formatTime(n.createdAt)}
                      </p>
                    </div>
                    {!n.read && <span className="notice-dot" aria-label="Okunmadı" />}
                  </>
                )
                return (
                  <li key={n.id} className={n.read ? undefined : 'is-unread'}>
                    {n.link ? (
                      <Link to={n.link} className="notice-row" onClick={() => !n.read && markRead(n.id)}>
                        {body}
                      </Link>
                    ) : (
                      <div
                        className="notice-row"
                        role={n.read ? undefined : 'button'}
                        tabIndex={n.read ? undefined : 0}
                        onClick={() => !n.read && markRead(n.id)}
                        onKeyDown={(e) => {
                          if (!n.read && (e.key === 'Enter' || e.key === ' ')) {
                            e.preventDefault()
                            markRead(n.id)
                          }
                        }}
                      >
                        {body}
                      </div>
                    )}
                  </li>
                )
              })}
            </ul>
          </div>
        </section>
      ))}
    </div>
  )
}
