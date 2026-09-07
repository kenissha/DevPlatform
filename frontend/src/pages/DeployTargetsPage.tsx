import { useEffect, useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, ApiError, type DeployRecipe, type DeployTarget, type ReleaseInfo } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { DeployIcon, PlusIcon } from '../components/icons'
import { Modal } from '../components/Modal'
import { formatDate } from '../labels'
import { useRepos } from '../repos/ReposContext'

// Release directory names are Go's `t.UTC().Format("20060102T150405.000000000")`
// (see backend/internal/deploy/versionstore.go's releaseName) — parsed
// back into a real date here since the raw name alone doesn't tell an
// admin when a release actually happened without doing the arithmetic by
// hand. A name that's had a collision-retry suffix appended (e.g. "-1")
// falls back to showing it unchanged rather than guessing.
function formatReleaseName(name: string): string {
  const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})\.(\d{3})/.exec(name)
  if (!m) return name
  const [, y, mo, d, h, mi, s, ms] = m
  return formatDate(`${y}-${mo}-${d}T${h}:${mi}:${s}.${ms}Z`)
}

// DeployTargetsPage is the admin-only screen for managing which
// (repo, environment) pair deploys to which IIS site — see
// docs/superpowers/specs/2026-08-18-deploy-target-management-design.md.
// siteName is always chosen from GET /api/allowed-sites, never typed:
// that list is ops-managed and the one thing this page can never write
// to, which is the feature's actual security boundary.
export function DeployTargetsPage() {
  const { user } = useAuth()
  const { repos } = useRepos()
  // ?repo= is how a repo's Deploy page hands its own name over when it
  // has no target yet. Without it, setting one up meant arriving here,
  // finding that repo in the dropdown, and remembering which one you came
  // from — three steps that carry no decision.
  const [searchParams] = useSearchParams()
  const presetRepo = searchParams.get('repo') ?? ''
  const [targets, setTargets] = useState<DeployTarget[] | null>(null)
  const [allowedSites, setAllowedSites] = useState<string[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [editingTarget, setEditingTarget] = useState<DeployTarget | null>(null)
  // One flag for both jobs: the form is the same either way, and
  // editingTarget decides which of the two it is showing.
  const [formOpen, setFormOpen] = useState(false)

  function reload() {
    Promise.all([api.listDeployTargets(), api.listAllowedSites()])
      .then(([t, sites]) => {
        setTargets(t)
        setAllowedSites(sites)
        setError(null)
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : 'Deploy hedefleri yüklenemedi'))
  }

  useEffect(reload, [])

  // Arriving from a repo's Deploy page means the intent is already
  // decided; opening the form is one click nobody needs to spend.
  useEffect(() => {
    if (presetRepo) setFormOpen(true)
  }, [presetRepo])

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
          <h1>Deploy hedefleri</h1>
          <p className="page-subtitle">Hangi repo hangi ortama, hangi IIS site'ına deploy olur</p>
        </div>
        <button
          type="button"
          className="btn-primary"
          onClick={() => {
            setEditingTarget(null)
            setFormOpen(true)
          }}
        >
          <PlusIcon /> Yeni hedef
        </button>
      </div>

      {error && <p className="error">{error}</p>}

      {targets === null && (
        <div className="card">
          <p className="empty-state">Yükleniyor...</p>
        </div>
      )}
      {targets?.length === 0 && (
        <div className="card">
          <p className="empty-state">
            Henüz deploy hedefi yok. Aşağıdan bir repo ve ortam seçerek ilkini tanımlayabilirsin.
          </p>
        </div>
      )}

      {/* Grouped by repository, because that is the unit a person holds in
          their head ("bu projenin ortamları"), not the flat (repo,
          environment) pairs the store keys on. */}
      {targets &&
        targets.length > 0 &&
        groupByRepo(targets).map(([repoName, repoTargets]) => (
          <section key={repoName} className="target-group">
            <div className="section-title">
              <h2>{repoName}</h2>
              <span className="badge badge-neutral">{repoTargets.length} ortam</span>
              <div className="spacer" />
              <Link to={`/repos/${encodeURIComponent(repoName)}/deployments`}>Deploy sayfası →</Link>
            </div>
            <div className="card">
          <ul className="row-list">
            {repoTargets.map((t) => (
              <li key={`${t.repo}/${t.environment}`}>
                <div className="target-head">
                  <DeployIcon className="target-icon" />
                  <span className="target-env">{t.environment}</span>
                  <div className="spacer" />
                  <button type="button" className="btn-secondary btn-sm" onClick={() => {
                      setEditingTarget(t)
                      setFormOpen(true)
                    }}>
                    Düzenle
                  </button>
                  <button
                    type="button"
                    className="link-button danger"
                    onClick={async () => {
                      if (
                        !confirm(
                          `${t.repo} → ${t.environment} hedefi silinsin mi? Bu ortama artık deploy isteği açılamaz.`,
                        )
                      ) {
                        return
                      }
                      try {
                        await api.deleteDeployTarget(t.repo, t.environment)
                        if (editingTarget?.repo === t.repo && editingTarget?.environment === t.environment) {
                          setEditingTarget(null)
                        }
                        reload()
                      } catch (err) {
                        setError(err instanceof ApiError ? err.message : 'Silinemedi')
                      }
                    }}
                  >
                    Sil
                  </button>
                </div>

                {/* Labelled values, not a row of same-coloured badges: the
                    old layout put the build recipe, an IIS site name and a
                    version count side by side in identical pills, so which
                    one was which had to be inferred from the content. */}
                <dl className="env-card-facts target-facts">
                  <div>
                    <dt>Build</dt>
                    <dd>{t.recipe}</dd>
                  </div>
                  <div>
                    <dt>IIS site'ı</dt>
                    <dd title={t.siteName}>{t.siteName}</dd>
                  </div>
                  <div>
                    <dt>Saklanan sürüm</dt>
                    <dd>{t.keepVersions}</dd>
                  </div>
                  <div>
                    <dt>Secrets dosyası</dt>
                    <dd title={t.secretsTarget}>{t.secretsTarget || '—'}</dd>
                  </div>
                </dl>

                <div className="target-panels">
                  <ReleasesPanel target={t} />
                  <SecretsPanel target={t} />
                </div>
              </li>
            ))}
          </ul>
            </div>
          </section>
        ))}

      {formOpen && (
        <Modal
          variant="panel"
          icon={<DeployIcon />}
          title={editingTarget ? 'Deploy hedefini düzenle' : 'Yeni deploy hedefi'}
          subtitle={
            editingTarget
              ? `${editingTarget.repo} → ${editingTarget.environment}`
              : 'Bir ortamın hangi IIS site\'ına, hangi build tarifiyle çıkacağını tanımlar.'
          }
          onClose={() => {
            setFormOpen(false)
            setEditingTarget(null)
          }}
        >
          <TargetForm
            repos={repos ?? []}
            allowedSites={allowedSites ?? []}
            editingTarget={editingTarget}
            presetRepo={presetRepo}
            onDone={() => {
              setFormOpen(false)
              setEditingTarget(null)
              reload()
            }}
            onCancel={() => {
              setFormOpen(false)
              setEditingTarget(null)
            }}
          />
        </Modal>
      )}
    </div>
  )
}

// groupByRepo keeps the store's ordering within each repository and
// orders the repositories themselves alphabetically, so the page does not
// reshuffle between visits.
function groupByRepo(targets: DeployTarget[]): [string, DeployTarget[]][] {
  const byRepo = new Map<string, DeployTarget[]>()
  for (const t of targets) {
    const list = byRepo.get(t.repo)
    if (list) list.push(t)
    else byRepo.set(t.repo, [t])
  }
  return [...byRepo.entries()].sort(([a], [b]) => a.localeCompare(b, 'tr'))
}

// ReleasesPanel is a per-target, collapsed-by-default section listing the
// releases still on disk for (target.repo, target.environment), with a
// "Bu versiyona dön" (rollback) action on every one except whichever is
// already active. keepVersions is the practical depth this list ever
// reaches — older releases are pruned on every new deploy, so rollback
// can only ever reach as far back as target.keepVersions allows.
function ReleasesPanel({ target }: { target: DeployTarget }) {
  const [open, setOpen] = useState(false)
  const [releases, setReleases] = useState<ReleaseInfo[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [rollingBackTo, setRollingBackTo] = useState<string | null>(null)

  function load() {
    setError(null)
    api
      .listReleases(target.repo, target.environment)
      .then(setReleases)
      .catch((err) => setError(err instanceof ApiError ? err.message : 'Versiyonlar yüklenemedi'))
  }

  useEffect(() => {
    if (open) load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  async function handleRollback(name: string) {
    if (
      !confirm(
        `"${formatReleaseName(name)}" versiyonuna dönülsün mü? Bu, ${target.siteName} sitesini canlıda hemen değiştirir.`,
      )
    ) {
      return
    }
    setRollingBackTo(name)
    setError(null)
    try {
      await api.rollback(target.repo, target.environment, name)
      load()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Rollback başarısız')
    } finally {
      setRollingBackTo(null)
    }
  }

  return (
    <div className="row-sub">
      <button type="button" className="btn-ghost btn-sm" onClick={() => setOpen((v) => !v)}>
        {open ? 'Versiyonları gizle' : 'Versiyonlar'}
      </button>
      {open && (
        <div className="release-list">
          {error && <p className="error">{error}</p>}
          {releases === null && !error && <p className="empty-state">Yükleniyor...</p>}
          {releases?.length === 0 && <p className="empty-state">Henüz hiç deploy edilmemiş.</p>}
          {releases && releases.length > 0 && (
            <ul className="row-list">
              {releases.map((r) => (
                <li key={r.name}>
                  <div className="row-main">
                    <span className="row-title">{formatReleaseName(r.name)}</span>
                    {r.active && <span className="badge badge-success">Aktif</span>}
                    <span className="spacer" />
                    {!r.active && (
                      <button
                        type="button"
                        className="btn-ghost btn-sm"
                        disabled={rollingBackTo !== null}
                        onClick={() => handleRollback(r.name)}
                      >
                        {rollingBackTo === r.name ? 'Dönülüyor...' : 'Bu versiyona dön'}
                      </button>
                    )}
                  </div>
                  <p className="row-meta">
                    <code>{r.name}</code>
                  </p>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}

// SecretsPanel is a per-target, collapsed-by-default section for
// uploading the plaintext content that gets encrypted server-side and
// injected into every future release for this target (see
// backend/internal/secretsvault). Deliberately write-only, like GitHub
// Actions' own environment secrets: the textarea always starts empty —
// there is no API to read a saved value back — and clears again after a
// successful save so the plaintext never lingers on screen.
function SecretsPanel({ target }: { target: DeployTarget }) {
  const [open, setOpen] = useState(false)
  const [content, setContent] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  async function save() {
    if (!content.trim()) return
    setSaving(true)
    setError(null)
    setSaved(false)
    try {
      await api.setSecrets(target.repo, target.environment, content)
      setContent('')
      setSaved(true)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Kaydedilemedi')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="row-sub">
      <button
        type="button"
        className="btn-ghost btn-sm"
        onClick={() => {
          setOpen((v) => !v)
          setSaved(false)
        }}
      >
        Secrets
      </button>
      {open && (
        <div className="release-list">
          <p className="empty-state">
            {target.secretsTarget
              ? `Kaydedilen içerik, her deploy'da release içine "${target.secretsTarget}" olarak yazılır.`
              : 'Bu hedefte "Secrets hedefi" alanı boş — önce yukarıdaki formda hangi dosya adına yazılacağını belirt.'}
            {' '}Güvenlik nedeniyle daha önce kaydedilmiş bir değer burada asla gösterilmez, sadece üzerine yazabilirsin.
          </p>
          <textarea
            rows={6}
            value={content}
            onChange={(e) => setContent(e.target.value)}
            placeholder={'OAS_USERNAME=...\nOAS_PASSWORD=...'}
            disabled={saving}
          />
          <div className="form-actions">
            <button type="button" className="btn-primary btn-sm" disabled={saving || !content.trim()} onClick={save}>
              {saving ? 'Kaydediliyor...' : 'Kaydet'}
            </button>
          </div>
          {saved && <p className="row-meta">Kaydedildi.</p>}
          {error && <p className="error">{error}</p>}
        </div>
      )}
    </div>
  )
}

function TargetForm({
  repos,
  allowedSites,
  editingTarget,
  presetRepo,
  onDone,
  onCancel,
}: {
  repos: string[]
  allowedSites: string[]
  editingTarget?: DeployTarget | null
  presetRepo?: string
  onDone: () => void
  onCancel: () => void
}) {
  const [repo, setRepo] = useState('')
  const [environment, setEnvironment] = useState('')
  const [recipe, setRecipe] = useState<DeployRecipe>('dotnet')
  const [siteName, setSiteName] = useState('')
  const [secretsTarget, setSecretsTarget] = useState('')
  const [keepVersions, setKeepVersions] = useState(5)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  useEffect(() => {
    if (editingTarget) {
      setRepo(editingTarget.repo)
      setEnvironment(editingTarget.environment)
      setRecipe(editingTarget.recipe)
      setSiteName(editingTarget.siteName)
      setSecretsTarget(editingTarget.secretsTarget ?? '')
      setKeepVersions(editingTarget.keepVersions)
    } else {
      setRepo(presetRepo ?? '')
      setEnvironment('')
      setRecipe('dotnet')
      setSiteName('')
      setSecretsTarget('')
      setKeepVersions(5)
    }
    setFormError(null)
  }, [editingTarget, presetRepo])

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!repo || !environment.trim() || !siteName) return
    setSaving(true)
    setFormError(null)
    try {
      await api.setDeployTarget(repo, environment.trim(), {
        recipe,
        siteName,
        secretsTarget: secretsTarget.trim() || undefined,
        keepVersions,
      })
      onDone()
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Kaydedilemedi')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="modal-form">
      {editingTarget ? (
        <div className="field">
          <label>Repo → Ortam</label>
          <p>
            {editingTarget.repo} → {editingTarget.environment}
          </p>
        </div>
      ) : (
        <>
          <div className="field">
            <label htmlFor="target-repo">Repo</label>
            <select id="target-repo" value={repo} onChange={(e) => setRepo(e.target.value)}>
              <option value="">Seçin...</option>
              {repos.map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
            </select>
          </div>

          <div className="field">
            <label htmlFor="target-environment">Ortam</label>
            <input
              id="target-environment"
              type="text"
              value={environment}
              onChange={(e) => setEnvironment(e.target.value)}
              placeholder="production"
            />
          </div>
        </>
      )}

      <div className="field">
        <label htmlFor="target-recipe">Recipe</label>
        <select id="target-recipe" value={recipe} onChange={(e) => setRecipe(e.target.value as DeployRecipe)}>
          <option value="dotnet">dotnet</option>
          <option value="npm">npm</option>
          <option value="go">go</option>
        </select>
      </div>

      <div className="field">
        <label htmlFor="target-site">IIS site'ı</label>
        <select id="target-site" value={siteName} onChange={(e) => setSiteName(e.target.value)}>
          <option value="">Seçin...</option>
          {allowedSites.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
        {allowedSites.length === 0 && (
          <p className="empty-state">Onaylı IIS site'ı yok — sunucuda DEVPLATFORM_ALLOWED_SITES_FILE ayarlanmalı.</p>
        )}
      </div>

      <div className="field">
        <label htmlFor="target-secrets">Secrets hedefi (opsiyonel)</label>
        <input
          id="target-secrets"
          type="text"
          value={secretsTarget}
          onChange={(e) => setSecretsTarget(e.target.value)}
          placeholder="appsettings.Production.json"
        />
      </div>

      <div className="field">
        <label htmlFor="target-keep">Saklanacak sürüm sayısı</label>
        <input
          id="target-keep"
          type="number"
          min={1}
          value={keepVersions}
          onChange={(e) => setKeepVersions(Number(e.target.value) || 1)}
        />
      </div>

      {formError && <p className="error">{formError}</p>}

      <div className="modal-actions">
        <button type="button" className="btn-secondary" onClick={onCancel} disabled={saving}>
          Vazgeç
        </button>
        <button type="submit" className="btn-primary" disabled={saving || !repo || !environment.trim() || !siteName}>
          {saving ? 'Kaydediliyor...' : editingTarget ? 'Güncelle' : 'Kaydet'}
        </button>
      </div>
    </form>
  )
}
