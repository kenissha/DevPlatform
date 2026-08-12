import type { AuditAction, MergeRequestStatus, TaskStatus } from './api/types'

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

export const AUDIT_ACTION_LABELS: Record<AuditAction, string> = {
  'repo.created': 'Repo oluşturuldu',
  'task.created': 'Görev açıldı',
  'task.updated': 'Görev güncellendi',
  'merge_request.opened': 'Merge isteği açıldı',
  'merge_request.approved': 'Merge onaylandı',
  'merge_request.rejected': 'Merge reddedildi',
}

export const AUDIT_ACTION_BADGE: Record<AuditAction, string> = {
  'repo.created': 'badge-neutral',
  'task.created': 'badge-neutral',
  'task.updated': 'badge-neutral',
  'merge_request.opened': 'badge-accent',
  'merge_request.approved': 'badge-success',
  'merge_request.rejected': 'badge-danger',
}

// Keyed by string, not a union like AUDIT_ACTION_LABELS: Notification.kind
// is deliberately a bare string on the backend (see api/types.ts), so an
// unrecognised kind falls back to the raw value rather than a type error.
// Covers the two kinds the backend currently produces (backend/internal/
// taskboard, backend/internal/mergerequest).
export const NOTIFICATION_KIND_LABELS: Record<string, string> = {
  task_assigned: 'Görev atandı',
  merge_request_opened: 'Merge isteği açıldı',
}

export const NOTIFICATION_KIND_BADGE: Record<string, string> = {
  task_assigned: 'badge-accent',
  merge_request_opened: 'badge-accent',
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
