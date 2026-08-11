import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api } from '../api/client'

// The repo list is shared state: the sidebar renders it on every page, and
// ReposPage's create form has to refresh it. Fetching it once here beats
// each consumer fetching its own copy and drifting.
interface ReposState {
  repos: string[] | null
  error: string | null
  reload: () => void
}

const ReposContext = createContext<ReposState | undefined>(undefined)

export function ReposProvider({ children }: { children: ReactNode }) {
  const [repos, setRepos] = useState<string[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const reload = useCallback(() => {
    api
      .listRepos()
      .then((r) => {
        setRepos(r)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  useEffect(reload, [reload])

  return <ReposContext.Provider value={{ repos, error, reload }}>{children}</ReposContext.Provider>
}

export function useRepos(): ReposState {
  const ctx = useContext(ReposContext)
  if (!ctx) throw new Error('useRepos must be used within a ReposProvider')
  return ctx
}
