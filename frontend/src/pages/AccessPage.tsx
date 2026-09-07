import { useEffect, useState } from 'react'
import { api, ApiError, type AccessRegistry, type DisplayNameRegistry, type Person } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { KeyIcon, LockIcon } from '../components/icons'
import { useRepos } from '../repos/ReposContext'

// AccessPage is the design doc's Faz 3 "proje bazlı yetkilendirme" admin
// screen. A person absent from the access registry is unrestricted (sees
// every repo) — see backend/internal/access's doc comment for why that is
// the default rather than the other way around.
//
// One card per person, not two lists. The page used to iterate the same
// people twice — once for access, once for display names — so changing
// both for one colleague meant working in two places, and two people who
// happened to share an email address were indistinguishable in either.
export function AccessPage() {
  const { user } = useAuth()
  const { repos } = useRepos()
  const [people, setPeople] = useState<Person[] | null>(null)
  const [registry, setRegistry] = useState<AccessRegistry | null>(null)
  const [displayNames, setDisplayNames] = useState<DisplayNameRegistry | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [openSubject, setOpenSubject] = useState<string | null>(null)

  function reload() {
    Promise.all([api.listPeople(), api.listAccess(), api.listDisplayNames()])
      .then(([p, r, d]) => {
        setPeople(p)
        setRegistry(r)
        setDisplayNames(d)
        setError(null)
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : 'Erişim bilgisi yüklenemedi'))
  }

  useEffect(reload, [])

  if (user?.role !== 'admin') {
    return (
      <div className="page">
        <p className="error">Bu sayfa sadece yöneticiler içindir.</p>
      </div>
    )
  }

  const restrictedCount = people && registry ? people.filter((p) => p.subject in registry).length : 0

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Kişiler</h1>
          <p className="page-subtitle">
            {people === null
              ? 'Erişim ve görünen adlar'
              : `${people.length} kişi` +
                (restrictedCount > 0 ? ` · ${restrictedCount} tanesi kısıtlı` : ' · hepsi tüm repolara erişiyor')}
          </p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {people === null && (
        <div className="card">
          <p className="empty-state">Yükleniyor...</p>
        </div>
      )}
      {people?.length === 0 && (
        <div className="card">
          <p className="empty-state">Henüz kimse giriş yapmadı.</p>
        </div>
      )}

      {people && people.length > 0 && registry && repos && (
        <div className="person-list">
          {people.map((person) => (
            <PersonCard
              key={person.subject}
              person={person}
              displayName={displayNames?.[person.subject]}
              restricted={person.subject in registry}
              allowed={registry[person.subject] ?? repos}
              allRepos={repos}
              isOpen={openSubject === person.subject}
              onToggle={() => setOpenSubject(openSubject === person.subject ? null : person.subject)}
              onChange={reload}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function PersonCard({
  person,
  displayName,
  restricted,
  allowed,
  allRepos,
  isOpen,
  onToggle,
  onChange,
}: {
  person: Person
  displayName?: string
  restricted: boolean
  allowed: string[]
  allRepos: string[]
  isOpen: boolean
  onToggle: () => void
  onChange: () => void
}) {
  const label = displayName || person.email || person.subject

  return (
    <div className="person-card">
      <div className="person-head">
        <span className="person-avatar">{initials(label)}</span>
        <div className="person-identity">
          <p className="person-name">{label}</p>
          {/* The subject id is shown, not just the email: two accounts can
              carry the same address, and without the id they render as one
              person twice. */}
          <p className="person-sub">
            {person.email && person.email !== label ? `${person.email} · ` : ''}
            <span className="mono">{person.subject}</span>
          </p>
        </div>
        <span className={`badge ${restricted ? 'badge-accent' : 'badge-neutral'}`}>
          {restricted ? `${allowed.length} repo` : 'Tüm repolar'}
        </span>
      </div>

      <DisplayNameField
        subject={person.subject}
        fallback={person.email || person.subject}
        current={displayName}
        onChange={onChange}
      />

      <div className="person-actions">
        <button type="button" className="btn-secondary btn-sm" onClick={onToggle}>
          <LockIcon /> {isOpen ? 'Erişimi kapat' : 'Erişimi düzenle'}
        </button>
        <div className="spacer" />
        <RevokeKeysButton subject={person.subject} />
      </div>

      {isOpen && (
        <AccessEditor
          subject={person.subject}
          allRepos={allRepos}
          allowed={allowed}
          restricted={restricted}
          onChange={onChange}
        />
      )}
    </div>
  )
}

function DisplayNameField({
  subject,
  fallback,
  current,
  onChange,
}: {
  subject: string
  fallback: string
  current: string | undefined
  onChange: () => void
}) {
  const [name, setName] = useState(current ?? '')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  // Reset when the row's data is refreshed from the server, or an edit
  // made elsewhere would leave a stale value sitting in this input.
  useEffect(() => {
    setName(current ?? '')
  }, [current])

  const dirty = name.trim() !== (current ?? '')

  async function save() {
    setSaving(true)
    setSaveError(null)
    try {
      if (name.trim()) await api.setDisplayName(subject, name.trim())
      else await api.clearDisplayName(subject)
      onChange()
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : 'Kaydedilemedi')
    } finally {
      setSaving(false)
    }
  }

  return (
    <label className="person-name-field">
      <span className="field-label">Görünen ad</span>
      <span className="person-name-input">
        <input
          type="text"
          value={name}
          placeholder={fallback}
          disabled={saving}
          onChange={(e) => setName(e.target.value)}
        />
        {/* The button appears only once there is something to save —
            a row of idle Kaydet buttons reads as work waiting to be done. */}
        {dirty && (
          <button type="button" className="btn-primary btn-sm" disabled={saving} onClick={save}>
            {saving ? '...' : 'Kaydet'}
          </button>
        )}
      </span>
      {saveError && <span className="error">{saveError}</span>}
    </label>
  )
}

function RevokeKeysButton({ subject }: { subject: string }) {
  const [revoking, setRevoking] = useState(false)
  const [message, setMessage] = useState<string | null>(null)

  async function revoke() {
    if (
      !window.confirm(
        `${subject} kişisinin TÜM git anahtarları iptal edilecek ve bütün makinelerinde git erişimi kesilecek. Emin misiniz?`,
      )
    ) {
      return
    }
    setRevoking(true)
    setMessage(null)
    try {
      await api.revokeGitToken(subject)
      setMessage('İptal edildi')
    } catch (err) {
      setMessage(err instanceof ApiError ? err.message : 'İptal edilemedi')
    } finally {
      setRevoking(false)
    }
  }

  return (
    <>
      {message && <span className="row-meta-inline">{message}</span>}
      <button type="button" className="link-button danger" disabled={revoking} onClick={revoke}>
        <KeyIcon /> Git anahtarlarını iptal et
      </button>
    </>
  )
}

function AccessEditor({
  subject,
  allRepos,
  allowed,
  restricted,
  onChange,
}: {
  subject: string
  allRepos: string[]
  allowed: string[]
  restricted: boolean
  onChange: () => void
}) {
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  async function toggle(repo: string, checked: boolean) {
    const next = checked ? [...allowed, repo] : allowed.filter((r) => r !== repo)
    setSaving(true)
    setSaveError(null)
    try {
      await api.setAccess(subject, next)
      onChange()
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : 'Kaydedilemedi')
    } finally {
      setSaving(false)
    }
  }

  async function clear() {
    setSaving(true)
    setSaveError(null)
    try {
      await api.clearAccess(subject)
      onChange()
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : 'Kaldırılamadı')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="access-editor">
      <p className="field-hint">
        {restricted
          ? 'Sadece işaretli repoları görebilir.'
          : 'Şu an tüm repolara erişiyor. Bir repo işaretlediğin an sadece işaretlediklerine iner.'}
      </p>
      <div className="checkbox-grid">
        {allRepos.map((repo) => (
          <label key={repo} className="checkbox-item">
            <input
              type="checkbox"
              checked={allowed.includes(repo)}
              disabled={saving}
              onChange={(e) => toggle(repo, e.target.checked)}
            />
            {repo}
          </label>
        ))}
      </div>
      {restricted && (
        <button type="button" className="link-button" disabled={saving} onClick={clear}>
          Kısıtlamayı kaldır — tüm repolara erişsin
        </button>
      )}
      {saveError && <p className="error">{saveError}</p>}
    </div>
  )
}

function initials(label: string): string {
  const parts = label.trim().split(/\s+/)
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase()
  return label.slice(0, 2).toUpperCase()
}
