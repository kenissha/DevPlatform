import type { MergeRequestStatus, TaskStatus } from './api/types'

// Turkish display strings + which badge variant each status wears. Shared
// so the same status never renders as two different labels/colours on two
// different screens.

export const TASK_STATUS_LABELS: Record<TaskStatus, string> = {
  in_progress: 'Yapılıyor',
  awaiting_test: 'Test bekliyor',
  done: 'Bitti',
}

export const TASK_STATUSES: TaskStatus[] = ['in_progress', 'awaiting_test', 'done']

export const TASK_STATUS_BADGE: Record<TaskStatus, string> = {
  in_progress: 'badge-accent',
  awaiting_test: 'badge-warn',
  done: 'badge-success',
}

export const MR_STATUS_LABELS: Record<MergeRequestStatus, string> = {
  open: 'Açık',
  approved: 'Onaylandı',
  rejected: 'Reddedildi',
}

export const MR_STATUS_BADGE: Record<MergeRequestStatus, string> = {
  open: 'badge-accent',
  approved: 'badge-success',
  rejected: 'badge-danger',
}

export function formatDate(iso: string): string {
  return new Date(iso).toLocaleString('tr-TR', {
    day: '2-digit',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}
