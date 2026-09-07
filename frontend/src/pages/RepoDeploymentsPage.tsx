import { useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { DeployTarget, DeploymentRequest } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { BranchIcon, DeployIcon, PlusIcon } from '../components/icons'
import { Modal } from '../components/Modal'
import { DEPLOYMENT_STATUS_BADGE, DEPLOYMENT_STATUS_LABELS, formatDate, formatRelative } from '../labels'

// Deploying is a two-step act on purpose: somebody opens a request, an
// admin approves it, and only the approval actually builds and swaps the
// site (see backend/internal/deployment). This page has to make both
// halves obvious — what each environment currently is, and what opening a
// request would do to it.
export function RepoDeploymentsPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'

  const [deployments, setDeployments] = useState<DeploymentRequest[] | null>(null)
  const [environments, setEnvironments] = useState<string[]>([])
  const [targets, setTargets] = useState<DeployTarget[]>([])
  const [branches, setBranches] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)
  const [openEnvironment, setOpenEnvironment] = useState<string | null>(null)
  const [actingId, setActingId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  function reload() {
    Promise.all([api.listDeployments(repo), api.listDeployTargetEnvironments(repo), api.listBranches(repo)])
      .then(([d, envs, b]) => {
        setDeployments(d)
        setEnvironments(envs)
        setBranches(b)
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(reload, [repo])

  // Full target details (recipe, IIS site) are admin-only — the
  // environment list above is what everyone else gets. Fetched separately
  // and failure-tolerant so a developer's page is unaffected by an
  // endpoint they aren't allowed to call.
  useEffect(() => {
    if (!isAdmin) return
    api
      .listDeployTargets()
      .then((all) => setTargets(all.filter((t) => t.repo === repo)))
      .catch(() => setTargets([]))
  }, [repo, isAdmin])

  async function decide(id: string, action: 'approve' | 'reject') {
    setActingId(id)
    setActionError(null)
    try {
      if (action === 'approve') await api.approveDeployment(repo, id)
      else await api.rejectDeployment(repo, id)
      reload()
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : 'İşlem başarısız')
    } finally {
      setActingId(null)
    }
  }

  const pending = deployments?.filter((d) => d.status === 'pending') ?? []

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Deploy</h1>
          <p className="page-subtitle">
            {environments.length === 0
              ? `${repo} için henüz ortam tanımlı değil`
              : `${repo} · ${environments.length} ortam` +
                (pending.length > 0 ? ` · ${pending.length} onay bekliyor` : '')}
          </p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}
      {actionError && <p className="error">{actionError}</p>}

      {environments.length === 0 ? (
        <NoTarget repo={repo} isAdmin={isAdmin} />
      ) : (
        <div className="env-grid">
          {environments.map((env) => (
            <EnvironmentCard
              key={env}
              environment={env}
              target={targets.find((t) => t.environment === env)}
              last={deployments?.find((d) => d.environment === env) ?? null}
              onDeploy={() => setOpenEnvironment(env)}
            />
          ))}
        </div>
      )}

      {pending.length > 0 && (
        <>
          <div className="section-title">
            <h2>Onay bekleyenler</h2>
            <span className="badge badge-warn">{pending.length}</span>
          </div>
          <div className="card">
            <ul className="row-list">
              {pending.map((d) => (
                <li key={d.id}>
                  <DeploymentRow deployment={d} />
                  {isAdmin ? (
                    <div className="deploy-decide">
                      <button
                        type="button"
                        className="btn-primary btn-sm"
                        onClick={() => decide(d.id, 'approve')}
                        disabled={actingId === d.id}
                      >
                        {actingId === d.id ? 'Çalışıyor...' : 'Onayla ve deploy et'}
                      </button>
                      <button
                        type="button"
                        className="btn-danger btn-sm"
                        onClick={() => decide(d.id, 'reject')}
                        disabled={actingId === d.id}
                      >
                        Reddet
                      </button>
                      <span className="deploy-decide-hint">
                        Onaylayınca build çalışır ve site yeni sürüme çevrilir.
                      </span>
                    </div>
                  ) : (
                    <p className="row-meta">Bir yöneticinin onayı bekleniyor.</p>
                  )}
                </li>
              ))}
            </ul>
          </div>
        </>
      )}

      <div className="section-title">
        <h2>Geçmiş</h2>
        {deployments && deployments.length > 0 && (
          <span className="badge badge-neutral">{deployments.length}</span>
        )}
      </div>
      <div className="card">
        {deployments === null && <p className="empty-state">Yükleniyor...</p>}
        {deployments?.length === 0 && <p className="empty-state">Henüz deploy isteği yok.</p>}
        {deployments && deployments.length > 0 && (
          <ul className="row-list scroll-list">
            {deployments.map((d) => (
              <li key={d.id}>
                <DeploymentRow deployment={d} />
              </li>
            ))}
          </ul>
        )}
      </div>

      {openEnvironment && (
        <NewDeploymentModal
          repo={repo}
          environment={openEnvironment}
          target={targets.find((t) => t.environment === openEnvironment)}
          branches={branches}
          onClose={() => setOpenEnvironment(null)}
          onCreated={() => {
            setOpenEnvironment(null)
            reload()
          }}
        />
      )}
    </div>
  )
}

// One card per environment: what it is wired to, where it stands, and the
// button that starts a release into it. The environment is the thing
// people think in ("production'a çıkalım"), so it is the thing the page
// is built out of rather than a dropdown inside a form.
function EnvironmentCard({
  environment,
  target,
  last,
  onDeploy,
}: {
  environment: string
  target?: DeployTarget
  last: DeploymentRequest | null
  onDeploy: () => void
}) {
  return (
    <div className="env-card">
      <div className="env-card-head">
        <DeployIcon className="env-card-icon" />
        <span className="env-card-name">{environment}</span>
        {last && (
          <span className={`badge ${DEPLOYMENT_STATUS_BADGE[last.status]}`}>
            {DEPLOYMENT_STATUS_LABELS[last.status]}
          </span>
        )}
      </div>

      {/* Only admins get target details from the API; for everyone else
          this stays absent rather than showing an empty row. */}
      {target && (
        <dl className="env-card-facts">
          <div>
            <dt>Build</dt>
            <dd>{target.recipe}</dd>
          </div>
          <div>
            <dt>IIS site'ı</dt>
            <dd title={target.siteName}>{target.siteName}</dd>
          </div>
          <div>
            <dt>Saklanan sürüm</dt>
            <dd>{target.keepVersions}</dd>
          </div>
        </dl>
      )}

      <p className="env-card-last">
        {last ? (
          <>
            son: {last.kind === 'rollback' ? 'geri alma' : last.sourceBranch} ·{' '}
            {formatRelative(last.createdAt)}
          </>
        ) : (
          'Henüz buraya deploy edilmedi.'
        )}
      </p>

      <button type="button" className="btn-primary" onClick={onDeploy}>
        <PlusIcon /> Deploy isteği aç
      </button>
    </div>
  )
}

function DeploymentRow({ deployment: d }: { deployment: DeploymentRequest }) {
  return (
    <>
      <div className="row-main">
        <span className="row-title">{d.environment}</span>
        {d.kind === 'rollback' ? (
          <span className="badge badge-accent">Geri alma</span>
        ) : (
          <span className="branch-chip">
            <BranchIcon />
            {d.sourceBranch}
          </span>
        )}
        <div className="spacer" />
        <span className={`badge ${DEPLOYMENT_STATUS_BADGE[d.status]}`}>
          {DEPLOYMENT_STATUS_LABELS[d.status]}
        </span>
      </div>
      <p className="row-meta">
        {d.author} {d.kind === 'rollback' ? 'geri döndü' : 'açtı'}
        <span>·</span>
        {formatDate(d.createdAt)}
        {d.decidedAt && d.kind !== 'rollback' && (
          <>
            <span>·</span>
            karar: {formatDate(d.decidedAt)}
          </>
        )}
      </p>
      {d.status === 'deployed' && d.releaseDir && (
        <p className="row-meta">
          <code>{d.releaseDir}</code>
        </p>
      )}
      {d.status === 'failed' && d.failureReason && <p className="error">{d.failureReason}</p>}
    </>
  )
}

// NoTarget is the state this page spends most of its life in for a new
// repo, and it used to be a dead end: it named the page an admin had to
// visit but didn't take them there, so setting one up meant leaving,
// finding the screen, picking this repo out of a dropdown, and coming
// back. The link carries the repo with it.
function NoTarget({ repo, isAdmin }: { repo: string; isAdmin: boolean }) {
  return (
    <div className="card">
      <div className="card-body empty-cta">
        <DeployIcon className="empty-cta-icon" />
        <p className="empty-cta-title">Bu repo için deploy hedefi yok</p>
        <p className="empty-cta-sub">
          Deploy hedefi, bir ortamın (örn. <code>production</code>) hangi IIS site'ına, hangi build
          tarifiyle çıkacağını söyler. Tanımlanmadan deploy isteği açılamaz.
        </p>
        {isAdmin ? (
          <Link to={`/deploy-targets?repo=${encodeURIComponent(repo)}`} className="btn-primary">
            {repo} için hedef tanımla →
          </Link>
        ) : (
          <p className="empty-cta-sub">Bir yöneticinin tanımlaması gerekiyor.</p>
        )}
      </div>
    </div>
  )
}

function NewDeploymentModal({
  repo,
  environment,
  target,
  branches,
  onClose,
  onCreated,
}: {
  repo: string
  environment: string
  target?: DeployTarget
  branches: string[]
  onClose: () => void
  onCreated: () => void
}) {
  const [sourceBranch, setSourceBranch] = useState(branches.includes('main') ? 'main' : branches[0] || '')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!sourceBranch) return
    setCreating(true)
    setCreateError(null)
    try {
      await api.createDeployment(repo, environment, sourceBranch)
      onCreated()
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : 'Deploy isteği oluşturulamadı')
    } finally {
      setCreating(false)
    }
  }

  return (
    <Modal title={`${environment} ortamına deploy`} onClose={onClose}>
      <form onSubmit={submit} className="modal-form">
        {/* Spelling out the destination inside the dialog: this is the
            last screen before something reaches a live site, and "which
            site was production again" is not a question to answer from
            memory. */}
        {target && (
          <dl className="env-card-facts">
            <div>
              <dt>Hedef site</dt>
              <dd>{target.siteName}</dd>
            </div>
            <div>
              <dt>Build</dt>
              <dd>{target.recipe}</dd>
            </div>
          </dl>
        )}

        <label className="field">
          <span className="field-label">Hangi branch yayınlansın?</span>
          <select value={sourceBranch} onChange={(e) => setSourceBranch(e.target.value)} autoFocus>
            {branches.map((b) => (
              <option key={b} value={b}>
                {b}
              </option>
            ))}
          </select>
          <span className="field-hint">
            Bu branch'in son hâli build edilip yeni bir sürüm klasörüne yazılır.
          </span>
        </label>

        {createError && <p className="error">{createError}</p>}

        <div className="modal-actions">
          <button type="button" className="btn-secondary" onClick={onClose} disabled={creating}>
            Vazgeç
          </button>
          <button type="submit" className="btn-primary" disabled={creating || !sourceBranch}>
            {creating ? 'Açılıyor...' : 'İstek aç'}
          </button>
        </div>
        <p className="field-hint">
          İstek açmak hiçbir şeyi yayına almaz — bir yönetici onaylayınca build çalışır ve site o
          sürüme çevrilir.
        </p>
      </form>
    </Modal>
  )
}
