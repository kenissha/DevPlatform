import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api } from '../api/client'
import type { Repo } from '../api/types'

// The repo list is shared state: the sidebar renders it on every page, and
// ReposPage's create form has to refresh it. Fetching it once here beats
// each consumer fetching its own copy and drifting.
interface ReposState {
  // Names only. Most consumers — the sidebar, the access matrix, the
  // deploy-target picker — want a plain list of names, and several pass it
  // straight down as a string[] prop.
  repos: string[] | null
  // repo → description, for the two screens that show one. Derived from
  // the same response as `repos`, so there is one request and one source
  // of truth; a repo with no description simply isn't a key here.
  descriptions: Record<string, string>
  error: string | null
  reload: () => void
}

const ReposContext = createContext<ReposState | undefined>(undefined)

export function ReposProvider({ children }: { children: ReactNode }) {
  const [list, setList] = useState<Repo[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const reload = useCallback(() => {
    api
      .listRepos()
      .then((r) => {
        setList(r)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  useEffect(reload, [reload])

  const repos = useMemo(() => list?.map((r) => r.name) ?? null, [list])
  const descriptions = useMemo(() => {
    const map: Record<string, string> = {}
    for (const r of list ?? []) {
      if (r.description) map[r.name] = r.description
    }
    return map
  }, [list])

  return (
    <ReposContext.Provider value={{ repos, descriptions, error, reload }}>{children}</ReposContext.Provider>
  )
}

export function useRepos(): ReposState {
  const ctx = useContext(ReposContext)
  if (!ctx) throw new Error('useRepos must be used within a ReposProvider')
  return ctx
}
