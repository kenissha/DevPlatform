import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api } from '../api/client'
import { BranchIcon } from '../components/icons'

export function RepoBranchesPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const [branches, setBranches] = useState<string[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api
      .listBranches(repo)
      .then(setBranches)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [repo])

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Branch'ler</h1>
          <p className="page-subtitle">{repo} deposundaki dallar</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="card">
        {branches === null && <p className="empty-state">Yükleniyor...</p>}
        {branches?.length === 0 && (
          <p className="empty-state">Henüz branch yok — ilk push'unuzda burada görünecek.</p>
        )}
        {branches && branches.length > 0 && (
          <ul className="row-list">
            {branches.map((b) => (
              <li key={b}>
                <div className="row-main">
                  <span className="branch-chip">
                    <BranchIcon />
                    {b}
                  </span>
                  {b === 'main' && (
                    <span className="badge badge-warn">Korumalı — doğrudan push kapalı</span>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
