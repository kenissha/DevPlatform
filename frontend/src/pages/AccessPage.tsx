import { useEffect, useState } from 'react'
import { api, ApiError, type AccessRegistry, type DisplayNameRegistry, type Person } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { LockIcon } from '../components/icons'
import { useRepos } from '../repos/ReposContext'

// AccessPage is the design doc's Faz 3 "proje bazlı yetkilendirme" admin
// screen: for each known person, which repos they're restricted to. A
// person absent from the access registry is unrestricted (sees every
// repo) — see backend/internal/access's doc comment for why that's the
// default rather than the other way around.
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

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Proje erişimi</h1>
          <p className="page-subtitle">Kişilerin hangi repolara erişebileceğini sınırlayın</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="card">
        {people === null && <p className="empty-state">Yükleniyor...</p>}
        {people?.length === 0 && <p className="empty-state">Henüz kimse giriş yapmadı.</p>}
        {people && people.length > 0 && registry && repos && (
          <ul className="row-list">
            {people.map((person) => {
              const restricted = person.subject in registry
              const allowed = registry[person.subject] ?? repos
              const isOpen = openSubject === person.subject
              return (
                <li key={person.subject}>
                  <div className="row-main">
                    <LockIcon className="muted" />
                    <span className="row-title">{person.email || person.subject}</span>
                    <span className="spacer" />
                    <span className={`badge ${restricted ? 'badge-accent' : 'badge-neutral'}`}>
                      {restricted ? `${allowed.length} repo` : 'Tüm repolar'}
                    </span>
                    <GitTokenRevokeButton subject={person.subject} />
                    <button type="button" className="btn-ghost" onClick={() => setOpenSubject(isOpen ? null : person.subject)}>
                      {isOpen ? 'Kapat' : 'Düzenle'}
                    </button>
                  </div>
                  {isOpen && (
                    <AccessEditor
                      subject={person.subject}
                      allRepos={repos}
                      allowed={allowed}
                      restricted={restricted}
                      onChange={reload}
                    />
                  )}
                </li>
              )
            })}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>Görünen adlar</h2>
        <p className="page-subtitle">Girişte kullanılan e-posta yerine panelde gösterilecek ad-soyad</p>
      </div>
      <div className="card">
        {people === null && <p className="empty-state">Yükleniyor...</p>}
        {people && people.length > 0 && displayNames && (
          <ul className="row-list">
            {people.map((person) => (
              <DisplayNameRow
                key={person.subject}
                subject={person.subject}
                fallback={person.email || person.subject}
                current={displayNames[person.subject]}
                onChange={reload}
              />
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function GitTokenRevokeButton({ subject }: { subject: string }) {
  const [revoking, setRevoking] = useState(false)
  const [message, setMessage] = useState<string | null>(null)

  async function revoke() {
    if (!window.confirm('Bu kişinin git anahtarını iptal etmek istediğinizden emin misiniz?')) {
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
      <button type="button" className="btn-ghost" disabled={revoking} onClick={revoke}>
        Git anahtarını iptal et
      </button>
      {message && <span className="muted">{message}</span>}
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
      {restricted && (
        <button type="button" className="btn-ghost" disabled={saving} onClick={clear}>
          Kısıtlamayı kaldır (tüm repolara erişsin)
        </button>
      )}
      {saveError && <p className="error">{saveError}</p>}
    </div>
  )
}

function DisplayNameRow({
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

  async function save() {
    setSaving(true)
    setSaveError(null)
    try {
      if (name.trim()) {
        await api.setDisplayName(subject, name.trim())
      } else {
        await api.clearDisplayName(subject)
      }
      onChange()
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : 'Kaydedilemedi')
    } finally {
      setSaving(false)
    }
  }

  return (
    <li>
      <div className="row-main">
        <span className="row-title">{fallback}</span>
        <span className="spacer" />
        <input
          type="text"
          value={name}
          placeholder={fallback}
          disabled={saving}
          onChange={(e) => setName(e.target.value)}
          className="inline-input"
        />
        <button type="button" className="btn-ghost" disabled={saving} onClick={save}>
          Kaydet
        </button>
        {saveError && <span className="error">{saveError}</span>}
      </div>
    </li>
  )
}
