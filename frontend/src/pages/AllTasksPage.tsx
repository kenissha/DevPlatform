import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../api/client'
import type { Person, Task } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { TaskFilterBar } from '../components/TaskFilterBar'
import { TaskList } from '../components/TaskList'
import { todayKey } from '../labels'
import {
  filterFromParams,
  filterToParams,
  isOverdue,
  matchesFilter,
  sortTasks,
  type TaskFilter,
} from '../tasks/filters'

// Every repository's tasks on one screen. Deliberately a list and not a
// kanban: a board across four repos is four times the columns for the
// same four statuses, and the question this page answers — "what is on
// my plate, everywhere?" — is a list question. The per-repo board is
// still where work gets moved.
//
// GET /api/tasks already narrows to the repositories the caller may see
// (internal/access), so this page needs no permission logic of its own.
export function AllTasksPage() {
  const { user } = useAuth()
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [people, setPeople] = useState<Person[]>([])
  const [error, setError] = useState<string | null>(null)

  const [params, setParams] = useSearchParams()
  const filter = filterFromParams(params)

  function setFilter(next: TaskFilter) {
    setParams(filterToParams(next), { replace: true })
  }

  useEffect(() => {
    api
      .listAllTasks()
      .then(setTasks)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
    api
      .listPeople()
      .then(setPeople)
      .catch(() => setPeople([]))
  }, [])

  const today = todayKey()
  const visible = (tasks ?? []).filter((t) => matchesFilter(t, filter, user?.subject ?? '', today))
  const openCount = tasks?.filter((t) => t.status !== 'done').length ?? 0
  const overdueCount = tasks?.filter((t) => isOverdue(t, today)).length ?? 0
  const repoCount = new Set(tasks?.map((t) => t.repo)).size

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Tüm görevler</h1>
          <p className="page-subtitle">
            {tasks === null
              ? 'Erişebildiğin repolardaki bütün iş takibi'
              : `${repoCount} repoda ${openCount} açık görev` +
                (overdueCount > 0 ? ` · ${overdueCount} geciken` : '')}
          </p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {tasks && (
        <TaskFilterBar
          filter={filter}
          onChange={setFilter}
          people={people}
          matched={visible.length}
          total={tasks.length}
        />
      )}

      {tasks === null && <p className="empty-state">Yükleniyor...</p>}

      {tasks && (
        <TaskList
          tasks={sortTasks(visible)}
          people={people}
          today={today}
          showRepo
          emptyMessage={
            tasks.length === 0
              ? 'Henüz hiçbir repoda görev yok.'
              : 'Filtreye uyan görev yok. Filtreyi temizleyip tekrar bak.'
          }
        />
      )}
    </div>
  )
}
