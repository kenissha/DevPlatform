import { useEffect, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { CopyButton } from '../components/CopyButton'
import { ImportRepoModal } from '../components/ImportRepoModal'
import { BranchIcon, PlusIcon, RepoIcon } from '../components/icons'
import { formatRelative } from '../labels'
import { cloneURL } from '../repos/clone'
import { useRepos } from '../repos/ReposContext'

// Mirrors repodesc.MaxLength on the backend, which rejects anything longer
// with a 400. Enforcing it on the input too means the limit is felt as a
// stopped keystroke rather than a failed submit.
const DESCRIPTION_MAX = 200

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
  const { repos, descriptions, error, reload } = useRepos()
  const isAdmin = user?.role === 'admin'

  const [summaries, setSummaries] = useState<Record<string, Summary>>({})
  const [showCreate, setShowCreate] = useState(false)
  const [importing, setImporting] = useState(false)
  const [newRepoName, setNewRepoName] = useState('')
  const [newRepoDesc, setNewRepoDesc] = useState('')
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
      await api.createRepo(newRepoName.trim(), newRepoDesc.trim())
      setNewRepoName('')
      setNewRepoDesc('')
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
          <div className="page-header-actions">
            {/* Not a primary button: importing is the rarer of the two, and
                two primaries side by side make neither one the answer. */}
            <button type="button" className="btn-secondary" onClick={() => setImporting(true)}>
              İçe aktar
            </button>
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
          </div>
        )}
      </div>

      {error && <p className="error">{error}</p>}

      {isAdmin && showCreate && (
        <div className="card create-repo">
          <form onSubmit={handleCreate} className="card-body create-repo-form">
            <label className="field">
              <span className="field-label">Repo adı</span>
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
                autoFocus
              />
              <span className="field-hint">Sadece harf, rakam, tire ve alt çizgi.</span>
            </label>

            <label className="field">
              <span className="field-label">
                Açıklama <span className="field-optional">— isteğe bağlı</span>
              </span>
              <input
                type="text"
                value={newRepoDesc}
                onChange={(e) => setNewRepoDesc(e.target.value)}
                placeholder="Bu depo ne işe yarıyor?"
                maxLength={DESCRIPTION_MAX}
              />
              <span className="field-hint">
                Listede ve reponun sayfasında görünür. Sonradan da değiştirebilirsin.
              </span>
            </label>

            <div className="create-repo-actions">
              <button type="submit" className="btn-primary" disabled={creating || !newRepoName.trim()}>
                {creating ? 'Oluşturuluyor...' : 'Oluştur'}
              </button>
              {createError && <p className="error">{createError}</p>}
            </div>
          </form>
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
            <RepoCard
              key={name}
              name={name}
              description={descriptions[name]}
              summary={summaries[name]}
            />
          ))}
        </div>
      )}

      {importing && (
        <ImportRepoModal
          onClose={() => setImporting(false)}
          // Refreshes the list behind the modal, so the imported repo is
          // already there when the person closes it.
          onImported={reload}
        />
      )}
    </div>
  )
}

function RepoCard({
  name,
  description,
  summary,
}: {
  name: string
  description?: string
  summary?: Summary
}) {
  const url = cloneURL(name)

  return (
    <div className="repo-card">
      <Link to={`/repos/${encodeURIComponent(name)}`} className="repo-card-head">
        <RepoIcon className="repo-card-icon" />
        <span className="repo-card-name">{name}</span>
      </Link>

      {description && <p className="repo-card-desc">{description}</p>}

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
