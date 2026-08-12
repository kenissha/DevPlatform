import { createContext, useContext } from 'react'
import type { Notification } from '../api/types'

// Split from NotificationsContext.tsx solely so that file can export only
// the NotificationsProvider component — a file that exports a component
// plus a hook trips oxlint's react/only-export-components (fine for Fast
// Refresh correctness, since the hook can't hot-reload anyway, but the
// rule doesn't know that), which ReposContext.tsx and AuthContext.tsx both
// already carry. Keeping this one clean rather than adding a third
// instance.
export interface NotificationsState {
  notifications: Notification[] | null
  unreadCount: number
  error: string | null
  reload: () => void
  markRead: (id: string) => void
}

export const NotificationsContext = createContext<NotificationsState | undefined>(undefined)

export function useNotifications(): NotificationsState {
  const ctx = useContext(NotificationsContext)
  if (!ctx) throw new Error('useNotifications must be used within a NotificationsProvider')
  return ctx
}
