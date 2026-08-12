import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { api } from '../api/client'
import type { Notification } from '../api/types'
import { NotificationsContext } from './useNotifications'

// The unread count is shared state: the sidebar renders it on every page,
// so it's fetched once here (same reasoning as ReposContext) rather than
// each consumer polling its own copy. No WebSocket/SSE — a 2-person team
// doesn't need push for this, a 30s poll is plenty.
const POLL_INTERVAL_MS = 30_000

export function NotificationsProvider({ children }: { children: ReactNode }) {
  const [notifications, setNotifications] = useState<Notification[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const reload = useCallback(() => {
    api
      .listNotifications()
      .then((n) => {
        setNotifications(n)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  useEffect(reload, [reload])

  useEffect(() => {
    const id = setInterval(reload, POLL_INTERVAL_MS)
    return () => clearInterval(id)
  }, [reload])

  // Flips the item's read state immediately so the row and the sidebar
  // badge update without waiting on the round-trip, then confirms with the
  // backend; a failed request reverts the flip and surfaces the error the
  // same way every other mutation on this codebase does.
  const markRead = useCallback((id: string) => {
    setNotifications((prev) => prev?.map((n) => (n.id === id ? { ...n, read: true } : n)) ?? prev)
    api.markNotificationRead(id).catch((err) => {
      setNotifications((prev) => prev?.map((n) => (n.id === id ? { ...n, read: false } : n)) ?? prev)
      setError(err instanceof Error ? err.message : String(err))
    })
  }, [])

  const unreadCount = notifications?.filter((n) => !n.read).length ?? 0

  return (
    <NotificationsContext.Provider value={{ notifications, unreadCount, error, reload, markRead }}>
      {children}
    </NotificationsContext.Provider>
  )
}
