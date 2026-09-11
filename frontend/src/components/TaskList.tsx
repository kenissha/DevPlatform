import { Link } from 'react-router-dom'
import type { Person, Task } from '../api/types'
import {
  TASK_PRIORITY_BADGE,
  TASK_PRIORITY_LABELS,
  TASK_STATUS_LABELS,
  formatDueDate,
  formatRelative,
} from '../labels'
import { isOverdue } from '../tasks/filters'
import { TaskLabelTags } from './TaskLabels'

// The board's other half. A board answers "where is everything?"; a list
// answers "what exactly is on my plate, in order?" — the questions want
// different shapes, so this is a real table rather than a narrower board.
//
// showRepo is on for the cross-repo view, where the repository is the one
// column that stops two tasks from looking identical.
export function TaskList({
  tasks,
  people,
  today,
  showRepo = false,
  emptyMessage,
}: {
  tasks: Task[]
  people: Person[]
  today: string
  showRepo?: boolean
  emptyMessage: string
}) {
  function personLabel(subject: string): string {
    const person = people.find((p) => p.subject === subject)
    return person?.displayName || person?.email || subject
  }

  if (tasks.length === 0) {
    return <p className="empty-state">{emptyMessage}</p>
  }

  return (
    <div className="card table-card">
      <div className="table-scroll">
        <table className="data-table task-table">
          <thead>
            <tr>
              <th>Görev</th>
              {showRepo && <th>Repo</th>}
              <th>Durum</th>
              <th>Öncelik</th>
              <th>Atanan</th>
              <th>Bitiş</th>
              <th>Açıldı</th>
            </tr>
          </thead>
          <tbody>
            {tasks.map((task) => (
              <tr key={`${task.repo}/${task.id}`}>
                <td>
                  {/* The whole cell is the link target — key, title and
                      tags together — so the click area matches what a
                      person reads as "the task". */}
                  <Link
                    className="task-cell"
                    to={`/repos/${encodeURIComponent(task.repo)}/tasks/${task.id}`}
                  >
                    <span className="task-cell-top">
                      {task.key && <span className="task-key">{task.key}</span>}
                      <span className="task-cell-title">{task.title}</span>
                    </span>
                    <TaskLabelTags labels={task.labels} />
                  </Link>
                </td>
                {showRepo && (
                  <td>
                    <Link to={`/repos/${encodeURIComponent(task.repo)}/tasks`} className="repo-cell">
                      {task.repo}
                    </Link>
                  </td>
                )}
                <td>
                  <span className="status-cell">
                    <span className={`kanban-dot kanban-dot-${task.status}`} aria-hidden="true" />
                    {TASK_STATUS_LABELS[task.status]}
                  </span>
                </td>
                <td>
                  {TASK_PRIORITY_BADGE[task.priority] ? (
                    <span className={`badge ${TASK_PRIORITY_BADGE[task.priority]}`}>
                      {TASK_PRIORITY_LABELS[task.priority]}
                    </span>
                  ) : (
                    <span className="text-muted">{TASK_PRIORITY_LABELS[task.priority]}</span>
                  )}
                </td>
                <td>{task.assignedTo ? personLabel(task.assignedTo) : <span className="text-muted">—</span>}</td>
                <td>
                  {task.dueDate ? (
                    <span className={isOverdue(task, today) ? 'due-chip is-overdue' : 'due-chip'}>
                      {formatDueDate(task.dueDate)}
                    </span>
                  ) : (
                    <span className="text-muted">—</span>
                  )}
                </td>
                <td className="text-muted">{formatRelative(task.createdAt)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
