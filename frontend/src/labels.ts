import type {
  AuditAction,
  DeploymentStatus,
  MergeRequestStatus,
  TaskLabel,
  TaskPriority,
  TaskStatus,
} from './api/types'

// Turkish display strings + which badge variant each status wears. Shared
// so the same status never renders as two different labels/colours on two
// different screens.

export const TASK_STATUS_LABELS: Record<TaskStatus, string> = {
  todo: 'Yapılacak',
  in_progress: 'Yapılıyor',
  awaiting_test: 'Test bekliyor',
  done: 'Bitti',
}

export const TASK_STATUSES: TaskStatus[] = ['todo', 'in_progress', 'awaiting_test', 'done']

export const TASK_STATUS_BADGE: Record<TaskStatus, string> = {
  todo: 'badge-neutral',
  in_progress: 'badge-accent',
  awaiting_test: 'badge-warn',
  done: 'badge-success',
}

export const TASK_PRIORITY_LABELS: Record<TaskPriority, string> = {
  low: 'Düşük',
  normal: 'Normal',
  high: 'Yüksek',
  critical: 'Kritik',
}

// Highest first: a picker and a sort both want the urgent end at the top.
export const TASK_PRIORITIES: TaskPriority[] = ['critical', 'high', 'normal', 'low']

// Normal is deliberately absent from the badge map — the default carries
// no colour, so the ones that do mean something stand out. Callers render
// a badge only when there is a class here.
export const TASK_PRIORITY_BADGE: Partial<Record<TaskPriority, string>> = {
  critical: 'badge-danger',
  high: 'badge-warn',
  low: 'badge-neutral',
}

// Sort weight, descending by urgency. Used to order a column so the thing
// that matters is not three cards down.
export const TASK_PRIORITY_RANK: Record<TaskPriority, number> = {
  critical: 0,
  high: 1,
  normal: 2,
  low: 3,
}

// Turkish spelling for the ASCII values the API stores. The two differ on
// purpose: a label ends up in the board's filter query string, and
// "teknik borç" would spend the rest of its life percent-encoded.
export const TASK_LABEL_LABELS: Record<TaskLabel, string> = {
  hata: 'Hata',
  ozellik: 'Özellik',
  iyilestirme: 'İyileştirme',
  'teknik-borc': 'Teknik borç',
  dokuman: 'Doküman',
  arastirma: 'Araştırma',
}

// Picker order, most-used first rather than alphabetical. Mirrors
// KnownLabels on the backend so a set renders the same way round in both.
export const TASK_LABELS: TaskLabel[] = [
  'hata',
  'ozellik',
  'iyilestirme',
  'teknik-borc',
  'dokuman',
  'arastirma',
]

// Every label gets its own hue. Unlike priority — where "normal" is
// deliberately colourless — a label always means something, and telling
// two of them apart at a glance is the whole point of putting them on a
// card.
export const TASK_LABEL_TONE: Record<TaskLabel, string> = {
  hata: 'tag-red',
  ozellik: 'tag-green',
  iyilestirme: 'tag-blue',
  'teknik-borc': 'tag-amber',
  dokuman: 'tag-slate',
  arastirma: 'tag-violet',
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
  'task.deleted': 'Görev silindi',
  'task.commented': 'Göreve yorum yapıldı',
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
  'task.deleted': 'badge-danger',
  'task.commented': 'badge-neutral',
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
  merge_request_decided: 'İnceleme sonucu',
  task_commented: 'Göreve yorum',
  deployment_opened: 'Deploy isteği açıldı',
  deployment_decided: 'Deploy sonucu',
}

export const NOTIFICATION_KIND_BADGE: Record<string, string> = {
  task_assigned: 'badge-accent',
  merge_request_opened: 'badge-accent',
  merge_request_decided: 'badge-neutral',
  task_commented: 'badge-accent',
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

// Groups timestamped records under a day heading, newest day first,
// preserving the order within each day. Both the notification list and
// the audit log are long streams of rows where the only structure a
// reader brings is "when" — without day breaks they read as one
// undifferentiated column.
export function groupByDay<T>(items: T[], at: (item: T) => string): [string, T[]][] {
  const groups: [string, T[]][] = []
  for (const item of items) {
    const key = dayKey(at(item))
    const last = groups[groups.length - 1]
    if (last && last[0] === key) last[1].push(item)
    else groups.push([key, [item]])
  }
  return groups
}

// "Bugün" / "Dün" / "3 Eylül Çarşamba". Relative for the two days people
// actually think of by name; the date itself past that.
export function dayHeading(iso: string): string {
  const d = new Date(iso)
  const today = dayKey(new Date().toISOString())
  const yesterday = dayKey(new Date(Date.now() - 86400000).toISOString())
  const key = dayKey(iso)
  if (key === today) return 'Bugün'
  if (key === yesterday) return 'Dün'
  return d.toLocaleDateString('tr-TR', { day: 'numeric', month: 'long', weekday: 'long' })
}

// Local calendar day, not the UTC one: a record made at 01:00 local time
// belongs under today's heading for the person reading it.
function dayKey(iso: string): string {
  const d = new Date(iso)
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

// Just the clock time — the day is already in the group heading above.
export function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString('tr-TR', { hour: '2-digit', minute: '2-digit' })
}

// Today as "YYYY-MM-DD" in the reader's own calendar — the same shape a
// due date is stored in, so overdue is a plain string comparison and never
// drifts by a timezone.
export function todayKey(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

// A due date rendered for reading: "12 Eylül" — the year only when it is
// not this one, because most deadlines are weeks away and the year is
// noise.
export function formatDueDate(due: string): string {
  const d = new Date(due + 'T00:00:00')
  const sameYear = d.getFullYear() === new Date().getFullYear()
  return d.toLocaleDateString('tr-TR', {
    day: 'numeric',
    month: 'long',
    year: sameYear ? undefined : 'numeric',
  })
}
