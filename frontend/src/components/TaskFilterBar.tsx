import type { Person, TaskLabel, TaskPriority, TaskStatus } from '../api/types'
import {
  TASK_LABELS,
  TASK_LABEL_LABELS,
  TASK_PRIORITIES,
  TASK_PRIORITY_LABELS,
  TASK_STATUSES,
  TASK_STATUS_LABELS,
} from '../labels'
import {
  ASSIGNEE_ME,
  ASSIGNEE_NOBODY,
  EMPTY_FILTER,
  isEmptyFilter,
  type TaskFilter,
} from '../tasks/filters'

// One row of controls, shared by the repo board and the cross-repo list so
// a filter means the same thing on both.
//
// showStatus is off on the board: there, status IS the columns, and a
// status filter would empty three of them to no purpose.
export function TaskFilterBar({
  filter,
  onChange,
  people,
  showStatus = true,
  matched,
  total,
}: {
  filter: TaskFilter
  onChange: (next: TaskFilter) => void
  people: Person[]
  showStatus?: boolean
  matched: number
  total: number
}) {
  const active = !isEmptyFilter(filter)

  return (
    <div className="filter-bar">
      <input
        type="search"
        className="filter-search"
        placeholder="Ara — anahtar, başlık, açıklama"
        value={filter.q}
        onChange={(e) => onChange({ ...filter, q: e.target.value })}
      />

      <select
        value={filter.assignee}
        onChange={(e) => onChange({ ...filter, assignee: e.target.value })}
        aria-label="Atanan"
      >
        <option value="">Herkes</option>
        <option value={ASSIGNEE_ME}>Bana atananlar</option>
        <option value={ASSIGNEE_NOBODY}>Atanmamış</option>
        {people.map((p) => (
          <option key={p.subject} value={p.subject}>
            {p.displayName || p.email || p.subject}
          </option>
        ))}
      </select>

      {showStatus && (
        <select
          value={filter.status}
          onChange={(e) => onChange({ ...filter, status: e.target.value as TaskStatus | '' })}
          aria-label="Durum"
        >
          <option value="">Her durum</option>
          {TASK_STATUSES.map((s) => (
            <option key={s} value={s}>
              {TASK_STATUS_LABELS[s]}
            </option>
          ))}
        </select>
      )}

      <select
        value={filter.priority}
        onChange={(e) => onChange({ ...filter, priority: e.target.value as TaskPriority | '' })}
        aria-label="Öncelik"
      >
        <option value="">Her öncelik</option>
        {TASK_PRIORITIES.map((p) => (
          <option key={p} value={p}>
            {TASK_PRIORITY_LABELS[p]}
          </option>
        ))}
      </select>

      <select
        value={filter.label}
        onChange={(e) => onChange({ ...filter, label: e.target.value as TaskLabel | '' })}
        aria-label="Etiket"
      >
        <option value="">Her etiket</option>
        {TASK_LABELS.map((l) => (
          <option key={l} value={l}>
            {TASK_LABEL_LABELS[l]}
          </option>
        ))}
      </select>

      <button
        type="button"
        className={filter.overdue ? 'filter-toggle is-on' : 'filter-toggle'}
        aria-pressed={filter.overdue}
        onClick={() => onChange({ ...filter, overdue: !filter.overdue })}
      >
        Gecikenler
      </button>

      {/* The count is only shown while something is filtering. Unfiltered,
          "14 / 14" is a number that answers no question. */}
      {active && (
        <span className="filter-count">
          {matched} / {total}
        </span>
      )}
      {active && (
        <button type="button" className="link-button" onClick={() => onChange(EMPTY_FILTER)}>
          Temizle
        </button>
      )}
    </div>
  )
}
