import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { TaskCommit } from '../api/types'
import { formatRelative } from '../labels'

// The commits whose message named this task.
//
// Nothing here is entered by hand. The git server reads every commit on
// its way in and matches the repository's own key prefix against the
// message, so writing "DEN-14 tarih filtresi düzeltildi" is the entire
// interaction — the same thing Jira sells as an integration you install,
// configure and keep an eye on.
//
// The section hides itself when there is nothing to show: an empty
// "Commitler" heading on every task would teach people to stop reading
// this part of the page.
export function TaskCommits({
  repo,
  taskId,
  taskKey,
}: {
  repo: string
  taskId: string
  taskKey?: string
}) {
  const [commits, setCommits] = useState<TaskCommit[] | null>(null)

  useEffect(() => {
    let cancelled = false
    api
      .taskCommits(repo, taskId)
      .then((list) => {
        if (!cancelled) setCommits(list)
      })
      // A failure here leaves the section hidden rather than putting an
      // error where work should be: the commit list is context, and the
      // task itself is still perfectly readable without it.
      .catch(() => {
        if (!cancelled) setCommits([])
      })
    return () => {
      cancelled = true
    }
  }, [repo, taskId])

  if (!commits || commits.length === 0) return null

  return (
    <section className="card task-commits">
      <div className="card-head">
        <h2>Commitler</h2>
        <span className="badge badge-neutral">{commits.length}</span>
      </div>
      <ul className="commit-list">
        {commits.map((commit) => (
          <li key={commit.hash}>
            <span className="commit-hash" title={commit.hash}>
              {commit.hash.slice(0, 7)}
            </span>
            <span className="commit-subject">{commit.subject}</span>
            <span className="commit-meta">
              {commit.author}
              {/* A commit with an unparsable author line keeps a zero
                  timestamp rather than borrowing the push time, so there
                  is deliberately nothing to show for it here. */}
              {commit.at && !commit.at.startsWith('0001') && ` · ${formatRelative(commit.at)}`}
            </span>
          </li>
        ))}
      </ul>
      {taskKey && (
        <p className="field-hint">
          Commit mesajına <code>{taskKey}</code> yazan her commit buraya kendiliğinden düşer.
        </p>
      )}
    </section>
  )
}
