import { useEffect, useState, type FormEvent } from 'react'
import { useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { DeploymentRequest } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { BranchIcon } from '../components/icons'
import { DEPLOYMENT_STATUS_BADGE, DEPLOYMENT_STATUS_LABELS, formatDate } from '../labels'

export function RepoDeploymentsPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const { user } = useAuth()
  const [deployments, setDeployments] = useState<DeploymentRequest[] | null>(null)
  const [environments, setEnvironments] = useState<string[]>([])
  const [branches, setBranches] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)

  const [environment, setEnvironment] = useState('')
  const [sourceBranch, setSourceBranch] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [actingId, setActingId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  function reload() {
    Promise.all([api.listDeployments(repo), api.listDeployTargetEnvironments(repo), api.listBranches(repo)])
      .then(([d, envs, b]) => {
        setDeployments(d)
        setEnvironments(envs)
        setBranches(b)
        setEnvironment((cur) => cur || envs[0] || '')
        setSourceBranch((cur) => cur || (b.includes('main') ? 'main' : b[0] || ''))
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(reload, [repo])

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (!environment || !sourceBranch) return
    setCreating(true)
    setCreateError(null)
    try {
      await api.createDeployment(repo, environment, sourceBranch)
      reload()
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : 'Deploy isteği oluşturulamadı')
    } finally {
      setCreating(false)
    }
  }

  async function handleApprove(id: string) {
    setActingId(id)
    setActionError(null)
    try {
      await api.approveDeployment(repo, id)
      reload()
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : 'Onaylanamadı')
    } finally {
      setActingId(null)
    }
  }

  async function handleReject(id: string) {
    setActingId(id)
    setActionError(null)
    try {
      await api.rejectDeployment(repo, id)
      reload()
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : 'Reddedilemedi')
    } finally {
      setActingId(null)
    }
  }

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Deploy istekleri</h1>
          <p className="page-subtitle">{repo} için build + versiyonlu yayınlama</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}
      {actionError && <p className="error">{actionError}</p>}

      <div className="card">
        {deployments === null && <p className="empty-state">Yükleniyor...</p>}
        {deployments?.length === 0 && <p className="empty-state">Henüz deploy isteği yok.</p>}
        {deployments && deployments.length > 0 && (
          <ul className="row-list">
            {deployments.map((d) => (
              <li key={d.id}>
                <div className="row-main">
                  <span className="row-title">{d.environment}</span>
                  {d.kind === 'rollback' ? (
                    <span className="badge badge-accent">Rollback</span>
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
                {d.status === 'failed' && d.failureReason && (
                  <p className="error" style={{ marginTop: 4 }}>
                    {d.failureReason}
                  </p>
                )}
                {d.status === 'pending' && user?.role === 'admin' && (
                  <div className="form-actions" style={{ marginTop: 8 }}>
                    <button
                      type="button"
                      className="btn-primary btn-sm"
                      onClick={() => handleApprove(d.id)}
                      disabled={actingId === d.id}
                    >
                      Onayla ve deploy et
                    </button>
                    <button
                      type="button"
                      className="btn-danger btn-sm"
                      onClick={() => handleReject(d.id)}
                      disabled={actingId === d.id}
                    >
                      Reddet
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>Yeni deploy isteği</h2>
      </div>
      <div className="card">
        <div className="card-body">
          {environments.length === 0 ? (
            <p className="empty-state">
              Bu repo için henüz deploy hedefi tanımlanmamış. Bir yönetici "Deploy
              Hedefleri" sayfasından bu repo için bir hedef eklemeden deploy isteği
              açılamaz.
            </p>
          ) : (
            <form onSubmit={handleCreate}>
              <div className="field field-row">
                <div>
                  <label htmlFor="deploy-environment">Ortam</label>
                  <select
                    id="deploy-environment"
                    value={environment}
                    onChange={(e) => setEnvironment(e.target.value)}
                  >
                    {environments.map((env) => (
                      <option key={env} value={env}>
                        {env}
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label htmlFor="deploy-branch">Branch</label>
                  <select
                    id="deploy-branch"
                    value={sourceBranch}
                    onChange={(e) => setSourceBranch(e.target.value)}
                  >
                    {branches.map((b) => (
                      <option key={b} value={b}>
                        {b}
                      </option>
                    ))}
                  </select>
                </div>
              </div>
              <div className="form-actions">
                <button
                  type="submit"
                  className="btn-primary"
                  disabled={creating || !environment || !sourceBranch}
                >
                  {creating ? 'Oluşturuluyor...' : 'Deploy isteği oluştur'}
                </button>
                {createError && <p className="error">{createError}</p>}
              </div>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}
