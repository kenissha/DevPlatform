import { useEffect, useState } from 'react'
import { api, ApiError, type AccessRegistry, type DisplayNameRegistry, type Person } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { KeyIcon, LockIcon } from '../components/icons'
import { Modal } from '../components/Modal'
import { useRepos } from '../repos/ReposContext'

// AccessPage is the design doc's Faz 3 "proje bazlı yetkilendirme" admin
// screen. A person absent from the access registry is unrestricted (sees
// every repo) — see backend/internal/access's doc comment for why that is
// the default rather than the other way around.
//
// A table, not a stack of cards. This is a roster: the questions it
// answers are comparative ("who is restricted", "who has no display name
// set"), and comparison needs columns that line up. Everything editable
// about one person lives in a dialog opened from their row, so the table
// stays one line per person no matter how many repositories exist.
export function AccessPage() {
  const { user } = useAuth()
  const { repos } = useRepos()
  const [people, setPeople] = useState<Person[] | null>(null)
  const [registry, setRegistry] = useState<AccessRegistry | null>(null)
  const [displayNames, setDisplayNames] = useState<DisplayNameRegistry | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState<Person | null>(null)

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
              : `${people.length} kişi · ` +
                (restrictedCount > 0
                  ? `${restrictedCount} tanesi belirli repolarla sınırlı`
                  : 'hepsi tüm repolara erişiyor')}
          </p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="card table-card">
        {people === null && <p className="empty-state">Yükleniyor...</p>}
        {people?.length === 0 && <p className="empty-state">Henüz kimse giriş yapmadı.</p>}
        {people && people.length > 0 && registry && repos && (
          <table className="data-table">
            <thead>
              <tr>
                <th scope="col">Kişi</th>
                <th scope="col">Rol</th>
                <th scope="col">Repo erişimi</th>
                <th scope="col" className="col-actions">
                  <span className="visually-hidden">İşlemler</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {people.map((person) => {
                const restricted = person.subject in registry
                const allowed = registry[person.subject] ?? repos
                const name = displayNames?.[person.subject]
                const label = name || person.email || person.subject
                return (
                  <tr key={person.subject}>
                    <td>
                      <div className="cell-person">
                        <span className="person-avatar">{initials(label)}</span>
                        <div className="cell-person-text">
                          <span className="cell-primary">{label}</span>
                          {/* The subject id is always shown: two accounts
                              can carry the same address, and without it
                              they render as one person listed twice. */}
                          <span className="cell-secondary">
                            {person.email && person.email !== label ? `${person.email} · ` : ''}
                            <span className="mono">{person.subject}</span>
                          </span>
                        </div>
                      </div>
                    </td>
                    <td>
                      <span className={`badge ${person.role === 'admin' ? 'badge-accent' : 'badge-neutral'}`}>
                        {person.role === 'admin' ? 'Yönetici' : 'Geliştirici'}
                      </span>
                    </td>
                    <td>
                      {restricted ? (
                        <span className="access-summary" title={allowed.join(', ')}>
                          <LockIcon />
                          {allowed.length} repo
                        </span>
                      ) : (
                        <span className="access-summary is-open">Tüm repolar</span>
                      )}
                    </td>
                    <td className="col-actions">
                      <button
                        type="button"
                        className="btn-secondary btn-sm"
                        onClick={() => setEditing(person)}
                      >
                        Düzenle
                      </button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>

      {editing && repos && registry && (
        <PersonModal
          person={editing}
          displayName={displayNames?.[editing.subject]}
          restricted={editing.subject in registry}
          allowed={registry[editing.subject] ?? repos}
          allRepos={repos}
          onClose={() => setEditing(null)}
          onChange={reload}
        />
      )}
    </div>
  )
}

function PersonModal({
  person,
  displayName,
  restricted,
  allowed,
  allRepos,
  onClose,
  onChange,
}: {
  person: Person
  displayName?: string
  restricted: boolean
  allowed: string[]
  allRepos: string[]
  onClose: () => void
  onChange: () => void
}) {
  const [name, setName] = useState(displayName ?? '')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fallback = person.email || person.subject

  async function run(action: () => Promise<unknown>, failure: string) {
    setSaving(true)
    setError(null)
    try {
      await action()
      onChange()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : failure)
    } finally {
      setSaving(false)
    }
  }

  async function saveName() {
    await run(
      () => (name.trim() ? api.setDisplayName(person.subject, name.trim()) : api.clearDisplayName(person.subject)),
      'Kaydedilemedi',
    )
  }

  async function toggleRepo(repo: string, checked: boolean) {
    const next = checked ? [...allowed, repo] : allowed.filter((r) => r !== repo)
    await run(() => api.setAccess(person.subject, next), 'Kaydedilemedi')
  }

  async function revokeKeys() {
    if (
      !window.confirm(
        `${fallback} kişisinin TÜM git anahtarları iptal edilecek ve bütün makinelerinde git erişimi kesilecek. Emin misiniz?`,
      )
    ) {
      return
    }
    await run(() => api.revokeGitToken(person.subject), 'İptal edilemedi')
  }

  const nameDirty = name.trim() !== (displayName ?? '')

  return (
    <Modal
      variant="panel"
      icon={<LockIcon />}
      title={displayName || fallback}
      subtitle={person.subject}
      onClose={onClose}
    >
      <div className="modal-sections">
        <section>
          <label className="field">
            <span className="field-label">Görünen ad</span>
            <span className="person-name-input">
              <input
                type="text"
                value={name}
                placeholder={fallback}
                disabled={saving}
                onChange={(e) => setName(e.target.value)}
              />
              {/* Only offered once there is something to save — an idle
                  Kaydet button reads as work waiting to be done. */}
              {nameDirty && (
                <button type="button" className="btn-primary btn-sm" disabled={saving} onClick={saveName}>
                  Kaydet
                </button>
              )}
            </span>
            <span className="field-hint">Boş bırakılırsa e-posta adresi gösterilir.</span>
          </label>
        </section>

        <section>
          <p className="field-label">Repo erişimi</p>
          <p className="field-hint">
            {restricted
              ? 'Sadece işaretli repoları görebilir.'
              : 'Şu an tüm repolara erişiyor. Bir repo işaretlediğin an sadece işaretlediklerine iner.'}
          </p>
          <div className="repo-checks">
            {allRepos.map((repo) => (
              <label key={repo} className="repo-check">
                <input
                  type="checkbox"
                  checked={allowed.includes(repo)}
                  disabled={saving}
                  onChange={(e) => toggleRepo(repo, e.target.checked)}
                />
                <span>{repo}</span>
              </label>
            ))}
          </div>
          {restricted && (
            <button
              type="button"
              className="link-button"
              disabled={saving}
              onClick={() => run(() => api.clearAccess(person.subject), 'Kaldırılamadı')}
            >
              Kısıtlamayı kaldır — tüm repolara erişsin
            </button>
          )}
        </section>

        <section className="danger-section">
          <div>
            <p className="field-label">Git anahtarları</p>
            <p className="field-hint">
              Bu kişinin bağlı olduğu bütün makinelerde git erişimini keser. Yeniden kurulum
              gerektirir.
            </p>
          </div>
          <button type="button" className="btn-danger btn-sm" disabled={saving} onClick={revokeKeys}>
            <KeyIcon /> Hepsini iptal et
          </button>
        </section>

        {error && <p className="error">{error}</p>}
      </div>
    </Modal>
  )
}

function initials(label: string): string {
  const parts = label.trim().split(/\s+/)
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase()
  return label.slice(0, 2).toUpperCase()
}
