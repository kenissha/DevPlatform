import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { api, ApiError, setAuthToken } from '../api/client'
import type { User } from '../api/types'

const STORAGE_KEY = 'devplatform_token'

type Status = 'loading' | 'authenticated' | 'unauthenticated'

interface AuthState {
  user: User | null
  status: Status
  login: (token: string) => void
  logout: () => void
}

const AuthContext = createContext<AuthState | undefined>(undefined)

// The real login flow (per the design doc) is a hand-off from an existing
// external system that already does the actual AD/LDAP authentication: it
// redirects here with the JWT it minted as a `?token=` query parameter.
// This reads that once on load and removes it from the URL so it doesn't
// linger in browser history / get bookmarked.
function readTokenFromUrl(): string | null {
  const params = new URLSearchParams(window.location.search)
  const token = params.get('token')
  if (!token) return null

  params.delete('token')
  const query = params.toString()
  const newUrl = window.location.pathname + (query ? `?${query}` : '') + window.location.hash
  window.history.replaceState({}, '', newUrl)
  return token
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState<string | null>(null)
  const [user, setUser] = useState<User | null>(null)
  const [status, setStatus] = useState<Status>('loading')

  useEffect(() => {
    const fromUrl = readTokenFromUrl()
    const initial = fromUrl ?? localStorage.getItem(STORAGE_KEY)
    if (fromUrl) localStorage.setItem(STORAGE_KEY, fromUrl)
    if (initial) {
      setToken(initial)
    } else {
      setStatus('unauthenticated')
    }
  }, [])

  useEffect(() => {
    setAuthToken(token)
    if (!token) {
      setUser(null)
      return
    }

    let cancelled = false
    setStatus('loading')
    api
      .me()
      .then((u) => {
        if (cancelled) return
        setUser(u)
        setStatus('authenticated')
      })
      .catch((err) => {
        if (cancelled) return
        // A token that no longer validates (expired, revoked, malformed):
        // drop it instead of getting stuck retrying it forever.
        if (err instanceof ApiError && err.status === 401) {
          localStorage.removeItem(STORAGE_KEY)
          setToken(null)
        }
        setUser(null)
        setStatus('unauthenticated')
      })
    return () => {
      cancelled = true
    }
  }, [token])

  const login = (newToken: string) => {
    localStorage.setItem(STORAGE_KEY, newToken)
    setToken(newToken)
  }

  const logout = () => {
    localStorage.removeItem(STORAGE_KEY)
    setToken(null)
  }

  return <AuthContext.Provider value={{ user, status, login, logout }}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider')
  return ctx
}
