import { useEffect, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { BranchIcon, CheckIcon, CopyIcon, PlusIcon, RepoIcon } from '../components/icons'
import { formatRelative } from '../labels'
import { useRepos } from '../repos/ReposContext'

// cloneURL is built from the page's own origin rather than a configured
// hostname: whoever is reading this page reached the platform at that
// origin, so it is by definition an address that works for them — no
// server-side setting to keep in sync, and correct behind IIS's proxy
// where the backend only ever sees an internal loopback address.
function cloneURL(repo: string): string {
  return `${window.location.origin}/git/${encodeURIComponent(repo)}.git`
}

// Summary is the at-a-glance line under a repo's name. Both halves are
// optional because they come from separate requests that fail
// independently — a brand-new repo has no commits to report, and one
// endpoint being slow shouldn't hold the other's number back.
interface Summary {
  branches?: number
  lastCommit?: { when: string; authorName: string }
}

export function ReposPage() {
  const { user } = useAuth()
  const { repos, error, reload } = useRepos()
  const isAdmin = user?.role === 'admin'

  const [summaries, setSummaries] = useState<Record<string, Summary>>({})
  const [showCreate, setShowCreate] = useState(false)
  const [newRepoName, setNewRepoName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  // Branch counts and last-commit times aren't part of GET /api/repos —
  // it returns names only. Rather than widen that endpoint (and make the
  // sidebar, which renders the same list, pay for data it never shows),
  // the cards fill themselves in from the per-repo endpoints that
  // already exist. Failures are swallowed on purpose: a repo whose
  // summary can't be read still renders, just without the detail line.
  useEffect(() => {
    if (!repos) return
    let cancelled = false

    for (const name of repos) {
      api
        .listBranches(name)
        .then((branches) => {
          if (cancelled) return
          setSummaries((prev) => ({ ...prev, [name]: { ...prev[name], branches: branches.length } }))
        })
        .catch(() => {})

      api
        .listCommits(name, 1)
        .then((commits) => {
          if (cancelled || commits.length === 0) return
          setSummaries((prev) => ({
            ...prev,
            [name]: {
              ...prev[name],
              lastCommit: { when: commits[0].when, authorName: commits[0].authorName },
            },
          }))
        })
        .catch(() => {})
    }

    return () => {
      cancelled = true
    }
  }, [repos])

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (!newRepoName.trim()) return
    setCreating(true)
    setCreateError(null)
    try {
      await api.createRepo(newRepoName.trim())
      setNewRepoName('')
      setShowCreate(false)
      reload()
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : 'Repo oluşturulamadı')
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Repolar</h1>
          <p className="page-subtitle">
            {isAdmin
              ? 'Platform üzerinde barındırılan tüm depolar'
              : 'Erişim verilen depolar — birine ihtiyacın varsa yöneticine söyle'}
          </p>
        </div>
        {isAdmin && (
          <button
            type="button"
            className={showCreate ? 'btn-secondary' : 'btn-primary'}
            onClick={() => {
              setShowCreate((open) => !open)
              setCreateError(null)
            }}
          >
            {showCreate ? 'Vazgeç' : (
              <>
                <PlusIcon /> Yeni repo
              </>
            )}
          </button>
        )}
      </div>

      {error && <p className="error">{error}</p>}

      {isAdmin && showCreate && (
        <div className="card create-repo">
          <div className="card-body">
            <form onSubmit={handleCreate} className="inline-form">
              {/* autoFocus is right here and only here: this panel appears
                  in response to a deliberate click, so the caret lands
                  where the person was already headed. */}
              <input
                type="text"
                value={newRepoName}
                onChange={(e) => setNewRepoName(e.target.value)}
                placeholder="yeni-repo-adi"
                pattern="[a-zA-Z0-9_-]+"
                title="Sadece harf, rakam, tire ve alt çizgi"
                aria-label="Yeni repo adı"
                autoFocus
              />
              <button type="submit" className="btn-primary" disabled={creating || !newRepoName.trim()}>
                {creating ? 'Oluşturuluyor...' : 'Oluştur'}
              </button>
            </form>
            <p className="form-hint">
              Boş bir depo oluşturulur. Adında sadece harf, rakam, tire ve alt çizgi olabilir.
            </p>
            {createError && <p className="error">{createError}</p>}
          </div>
        </div>
      )}

      {repos === null && <p className="empty-state">Yükleniyor...</p>}

      {repos?.length === 0 && (
        <div className="card">
          <p className="empty-state">
            {isAdmin
              ? 'Henüz repo yok. Yukarıdaki "Yeni repo" ile ilkini oluşturabilirsin.'
              : 'Henüz hiçbir repoya erişimin yok. Yöneticin "Proje erişimi" sayfasından yetki verebilir.'}
          </p>
        </div>
      )}

      {repos && repos.length > 0 && (
        <div className="repo-grid">
          {repos.map((name) => (
            <RepoCard key={name} name={name} summary={summaries[name]} />
          ))}
        </div>
      )}
    </div>
  )
}

function RepoCard({ name, summary }: { name: string; summary?: Summary }) {
  const url = cloneURL(name)

  return (
    <div className="repo-card">
      <Link to={`/repos/${encodeURIComponent(name)}`} className="repo-card-head">
        <RepoIcon className="repo-card-icon" />
        <span className="repo-card-name">{name}</span>
      </Link>

      <div className="repo-card-meta">
        {summary?.branches !== undefined && (
          <span>
            <BranchIcon /> {summary.branches} branch
          </span>
        )}
        {summary?.lastCommit ? (
          <span title={`${summary.lastCommit.authorName} — ${summary.lastCommit.when}`}>
            {formatRelative(summary.lastCommit.when)}
          </span>
        ) : (
          summary?.branches === 0 && <span>henüz commit yok</span>
        )}
      </div>

      <div className="repo-card-clone">
        <code title={url}>{url}</code>
        <CopyButton value={url} label={`${name} adresini kopyala`} />
      </div>
    </div>
  )
}

function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 1600)
    return () => clearTimeout(timer)
  }, [copied])

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
    } catch {
      // navigator.clipboard is unavailable over plain HTTP on some
      // browsers, and there is nothing useful to say about it — the
      // address is right there on screen to select by hand.
    }
  }

  return (
    <button
      type="button"
      className="icon-button"
      onClick={copy}
      aria-label={label}
      title={copied ? 'Kopyalandı' : 'Kopyala'}
    >
      {copied ? <CheckIcon className="copied" /> : <CopyIcon />}
    </button>
  )
}
