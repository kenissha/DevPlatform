import type { Task, TaskLabel, TaskPriority, TaskStatus } from '../api/types'
import { TASK_LABELS, TASK_PRIORITIES, TASK_PRIORITY_RANK, TASK_STATUSES, todayKey } from '../labels'

// What a person can narrow a board down to. Every field is "" for "no
// opinion" rather than undefined, so the whole thing round-trips through
// a query string without a special case per field.
export interface TaskFilter {
  // Free text, matched against key, title and description.
  q: string
  // A subject, '' for anyone, or the sentinel below for "whoever I am".
  assignee: string
  status: TaskStatus | ''
  priority: TaskPriority | ''
  label: TaskLabel | ''
  overdue: boolean
}

// "Bana atananlar" has to survive being pasted to somebody else. Storing
// the viewer's own subject in the URL would make the link mean "Ahmet's
// tasks" once Ahmet sends it on; this sentinel keeps it meaning "mine",
// whoever opens it.
export const ASSIGNEE_ME = 'me'
export const ASSIGNEE_NOBODY = 'none'

export const EMPTY_FILTER: TaskFilter = {
  q: '',
  assignee: '',
  status: '',
  priority: '',
  label: '',
  overdue: false,
}

export function isEmptyFilter(f: TaskFilter): boolean {
  return !f.q && !f.assignee && !f.status && !f.priority && !f.label && !f.overdue
}

// Read from a URL. Unknown values are dropped rather than kept, so a
// hand-edited or stale link degrades to a wider view instead of an empty
// one — a filter nothing can match looks exactly like "no tasks here".
export function filterFromParams(params: URLSearchParams): TaskFilter {
  const status = params.get('status') ?? ''
  const priority = params.get('priority') ?? ''
  const label = params.get('label') ?? ''
  return {
    q: params.get('q') ?? '',
    assignee: params.get('assignee') ?? '',
    status: (TASK_STATUSES as string[]).includes(status) ? (status as TaskStatus) : '',
    priority: (TASK_PRIORITIES as string[]).includes(priority) ? (priority as TaskPriority) : '',
    label: (TASK_LABELS as string[]).includes(label) ? (label as TaskLabel) : '',
    overdue: params.get('overdue') === '1',
  }
}

// Only non-default values are written, so an unfiltered board has a clean
// URL and "did I leave a filter on?" is answerable by looking at the
// address bar.
export function filterToParams(f: TaskFilter, keep?: URLSearchParams): URLSearchParams {
  const params = new URLSearchParams(keep)
  const set = (key: string, value: string) => {
    if (value) params.set(key, value)
    else params.delete(key)
  }
  set('q', f.q.trim())
  set('assignee', f.assignee)
  set('status', f.status)
  set('priority', f.priority)
  set('label', f.label)
  set('overdue', f.overdue ? '1' : '')
  return params
}

// viewer is the subject of whoever is looking, needed to resolve
// ASSIGNEE_ME. today is passed in rather than read here so a list and its
// "N geciken" summary can never disagree about the date across midnight.
export function matchesFilter(task: Task, f: TaskFilter, viewer: string, today: string): boolean {
  if (f.status && task.status !== f.status) return false
  if (f.priority && task.priority !== f.priority) return false
  if (f.label && !(task.labels ?? []).includes(f.label)) return false
  if (f.overdue && !isOverdue(task, today)) return false

  if (f.assignee === ASSIGNEE_ME) {
    if (task.assignedTo !== viewer) return false
  } else if (f.assignee === ASSIGNEE_NOBODY) {
    if (task.assignedTo) return false
  } else if (f.assignee && task.assignedTo !== f.assignee) {
    return false
  }

  if (f.q) {
    const needle = f.q.trim().toLocaleLowerCase('tr')
    const haystack = `${task.key ?? ''} ${task.title} ${task.description}`.toLocaleLowerCase('tr')
    if (!haystack.includes(needle)) return false
  }
  return true
}

// Done work is never overdue — chasing something already delivered is
// noise. Mirrors Task.Overdue on the backend.
export function isOverdue(task: Task, today = todayKey()): boolean {
  return Boolean(task.dueDate && task.status !== 'done' && task.dueDate < today)
}

// The list's default order, and the one a board column uses inside a
// status: most urgent first, then the thing that has waited longest.
// Finished tasks sink to the bottom of a mixed list — in a list view
// status is a column, not a heading, so without this the done pile buries
// the work.
export function sortTasks(tasks: Task[]): Task[] {
  return [...tasks].sort(
    (a, b) =>
      Number(a.status === 'done') - Number(b.status === 'done') ||
      TASK_PRIORITY_RANK[a.priority] - TASK_PRIORITY_RANK[b.priority] ||
      a.createdAt.localeCompare(b.createdAt),
  )
}
