import type { TaskLabel } from '../api/types'
import { TASK_LABELS, TASK_LABEL_LABELS, TASK_LABEL_TONE } from '../labels'

// Read-only tags, for a card or a task header. Renders nothing at all
// when there are none — an empty row still costs vertical space on a
// board where every card's height matters.
export function TaskLabelTags({ labels }: { labels?: TaskLabel[] }) {
  if (!labels || labels.length === 0) return null
  return (
    <div className="tag-row">
      {labels.map((label) => (
        <span key={label} className={`tag ${TASK_LABEL_TONE[label]}`}>
          {TASK_LABEL_LABELS[label]}
        </span>
      ))}
    </div>
  )
}

// The picker. Toggle buttons rather than a multi-select: the whole
// vocabulary is six items, so showing all of them costs one line and
// removes the "what can I even pick?" step. Every option stays visible
// whether or not it is chosen — a picker that hides what you didn't
// choose is a list you have to open to read.
export function TaskLabelPicker({
  value,
  onChange,
  disabled,
}: {
  value: TaskLabel[]
  onChange: (labels: TaskLabel[]) => void
  disabled?: boolean
}) {
  function toggle(label: TaskLabel) {
    // Rebuilt from TASK_LABELS rather than pushed onto the end, so the
    // order never depends on the order somebody clicked — the same set
    // renders identically everywhere, backend included.
    const next = new Set(value)
    if (next.has(label)) next.delete(label)
    else next.add(label)
    onChange(TASK_LABELS.filter((l) => next.has(l)))
  }

  return (
    <div className="tag-row">
      {TASK_LABELS.map((label) => {
        const on = value.includes(label)
        return (
          <button
            key={label}
            type="button"
            className={`tag tag-pick ${TASK_LABEL_TONE[label]}`}
            aria-pressed={on}
            disabled={disabled}
            onClick={() => toggle(label)}
          >
            {TASK_LABEL_LABELS[label]}
          </button>
        )
      })}
    </div>
  )
}
