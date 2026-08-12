import { Link } from 'react-router-dom'
import { NOTIFICATION_KIND_BADGE, NOTIFICATION_KIND_LABELS, formatDate } from '../labels'
import { useNotifications } from '../notifications/useNotifications'

export function NotificationsPage() {
  const { notifications, error, markRead } = useNotifications()

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Bildirimler</h1>
          <p className="page-subtitle">
            Size atanan görevler ve onay bekleyen merge istekleri gibi olaylar burada listelenir.
          </p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="card">
        {notifications === null && <p className="empty-state">Yükleniyor...</p>}
        {notifications?.length === 0 && <p className="empty-state">Henüz bildirim yok.</p>}
        {notifications && notifications.length > 0 && (
          <ul className="row-list">
            {notifications.map((n) => (
              <li key={n.id} className={!n.read ? 'unread' : undefined}>
                <div
                  className="row-main"
                  onClick={() => !n.read && markRead(n.id)}
                  style={!n.read ? { cursor: 'pointer' } : undefined}
                >
                  <span className={`badge ${NOTIFICATION_KIND_BADGE[n.kind] ?? 'badge-neutral'}`}>
                    {NOTIFICATION_KIND_LABELS[n.kind] ?? n.kind}
                  </span>
                  {n.link ? (
                    <Link to={n.link} className="row-title">
                      {n.message}
                    </Link>
                  ) : (
                    <span className="row-title">{n.message}</span>
                  )}
                </div>
                <p className="row-meta">{formatDate(n.createdAt)}</p>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
