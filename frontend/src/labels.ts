import type { AuditAction, DeploymentStatus, MergeRequestStatus, TaskStatus } from './api/types'

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
  'merge_request.opened': 'İnceleme isteği açıldı',
  'merge_request.approved': 'İnceleme isteği onaylandı',
  'merge_request.rejected': 'İnceleme isteği reddedildi',
  'deployment.opened': 'Deploy isteği açıldı',
  'deployment.deployed': 'Deploy edildi',
  'deployment.failed': 'Deploy başarısız',
  'deployment.rejected': 'Deploy reddedildi',
}

export const AUDIT_ACTION_BADGE: Record<AuditAction, string> = {
  'repo.created': 'badge-neutral',
  'task.created': 'badge-neutral',
  'task.updated': 'badge-neutral',
  'merge_request.opened': 'badge-accent',
  'merge_request.approved': 'badge-success',
  'merge_request.rejected': 'badge-danger',
  'deployment.opened': 'badge-accent',
  'deployment.deployed': 'badge-success',
  'deployment.failed': 'badge-danger',
  'deployment.rejected': 'badge-danger',
}

export const DEPLOYMENT_STATUS_LABELS: Record<DeploymentStatus, string> = {
  pending: 'Onay bekliyor',
  deployed: 'Deploy edildi',
  failed: 'Başarısız',
  rejected: 'Reddedildi',
}

export const DEPLOYMENT_STATUS_BADGE: Record<DeploymentStatus, string> = {
  pending: 'badge-accent',
  deployed: 'badge-success',
  failed: 'badge-danger',
  rejected: 'badge-danger',
}

// Keyed by string, not a union like AUDIT_ACTION_LABELS: Notification.kind
// is deliberately a bare string on the backend (see api/types.ts), so an
// unrecognised kind falls back to the raw value rather than a type error.
// Covers the two kinds the backend currently produces (backend/internal/
// taskboard, backend/internal/mergerequest).
export const NOTIFICATION_KIND_LABELS: Record<string, string> = {
  task_assigned: 'Görev atandı',
  merge_request_opened: 'İnceleme isteği açıldı',
  deployment_opened: 'Deploy isteği açıldı',
  deployment_decided: 'Deploy sonucu',
}

export const NOTIFICATION_KIND_BADGE: Record<string, string> = {
  task_assigned: 'badge-accent',
  merge_request_opened: 'badge-accent',
  deployment_opened: 'badge-accent',
  deployment_decided: 'badge-neutral',
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

// "3 saat önce" — what an activity feed wants, where the gap since an
// event matters more than the wall-clock time it happened at. Falls
// back to formatDate past a week, when "23 gün önce" stops being easier
// to read than the date itself.
export function formatRelative(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''

  const seconds = Math.round((Date.now() - then) / 1000)
  // A clock skew between server and browser can put a just-recorded
  // event slightly in the future; "az önce" reads better than "-2 saniye".
  if (seconds < 60) return 'az önce'

  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes} dakika önce`

  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours} saat önce`

  const days = Math.round(hours / 24)
  if (days === 1) return 'dün'
  if (days < 7) return `${days} gün önce`

  return formatDate(iso)
}

// The time-of-day greeting the dashboard opens with. Turkish splits the
// day differently from English: "iyi günler" covers midday through late
// afternoon, and "günaydın" is strictly morning.
export function greeting(now = new Date()): string {
  const hour = now.getHours()
  if (hour < 6) return 'İyi geceler'
  if (hour < 12) return 'Günaydın'
  if (hour < 18) return 'İyi günler'
  return 'İyi akşamlar'
}

export function formatDayHeading(now = new Date()): string {
  return now.toLocaleDateString('tr-TR', { day: 'numeric', month: 'long', weekday: 'long' })
}
