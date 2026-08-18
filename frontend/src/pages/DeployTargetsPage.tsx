import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError, type DeployRecipe, type DeployTarget } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { DeployIcon } from '../components/icons'
import { useRepos } from '../repos/ReposContext'

// DeployTargetsPage is the admin-only screen for managing which
// (repo, environment) pair deploys to which IIS site — see
// docs/superpowers/specs/2026-08-18-deploy-target-management-design.md.
// siteName is always chosen from GET /api/allowed-sites, never typed:
// that list is ops-managed and the one thing this page can never write
// to, which is the feature's actual security boundary.
export function DeployTargetsPage() {
  const { user } = useAuth()
  const { repos } = useRepos()
  const [targets, setTargets] = useState<DeployTarget[] | null>(null)
  const [allowedSites, setAllowedSites] = useState<string[] | null>(null)
  const [error, setError] = useState<string | null>(null)

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
      </div>

      {error && <p className="error">{error}</p>}

      <div className="card">
        {targets === null && <p className="empty-state">Yükleniyor...</p>}
        {targets?.length === 0 && <p className="empty-state">Henüz deploy hedefi yok.</p>}
        {targets && targets.length > 0 && (
          <ul className="row-list">
            {targets.map((t) => (
              <li key={`${t.repo}/${t.environment}`}>
                <div className="row-main">
                  <DeployIcon className="muted" />
                  <span className="row-title">
                    {t.repo} → {t.environment}
                  </span>
                  <span className="spacer" />
                  <span className="badge badge-neutral">{t.recipe}</span>
                  <span className="badge badge-neutral">{t.siteName}</span>
                  <button
                    type="button"
                    className="btn-ghost"
                    onClick={async () => {
                      try {
                        await api.deleteDeployTarget(t.repo, t.environment)
                        reload()
                      } catch (err) {
                        setError(err instanceof ApiError ? err.message : 'Silinemedi')
                      }
                    }}
                  >
                    Sil
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>Yeni deploy hedefi</h2>
      </div>
      <div className="card">
        <div className="card-body">
          <NewTargetForm repos={repos ?? []} allowedSites={allowedSites ?? []} onCreated={reload} />
        </div>
      </div>
    </div>
  )
}

function NewTargetForm({
  repos,
  allowedSites,
  onCreated,
}: {
  repos: string[]
  allowedSites: string[]
  onCreated: () => void
}) {
  const [repo, setRepo] = useState('')
  const [environment, setEnvironment] = useState('')
  const [recipe, setRecipe] = useState<DeployRecipe>('dotnet')
  const [siteName, setSiteName] = useState('')
  const [secretsTarget, setSecretsTarget] = useState('')
  const [keepVersions, setKeepVersions] = useState(5)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

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
      setEnvironment('')
      setSecretsTarget('')
      onCreated()
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Kaydedilemedi')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="stacked-form">
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

      <div className="field">
        <label htmlFor="target-recipe">Recipe</label>
        <select id="target-recipe" value={recipe} onChange={(e) => setRecipe(e.target.value as DeployRecipe)}>
          <option value="dotnet">dotnet</option>
          <option value="npm">npm</option>
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

      <div className="form-actions">
        <button type="submit" className="btn-primary" disabled={saving || !repo || !environment.trim() || !siteName}>
          {saving ? 'Kaydediliyor...' : 'Kaydet'}
        </button>
      </div>
      {formError && <p className="error">{formError}</p>}
    </form>
  )
}
