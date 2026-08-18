import { useState } from 'react'
import { api, ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'

// HesabimPage lets the signed-in person mint their own git credential —
// see docs/superpowers/specs/2026-08-17-per-user-git-access-design.md.
// The raw token is shown exactly once, right after generation; it is
// never stored or re-displayed — only its SHA-256 hash persists on the
// server (see backend/internal/gittoken).
export function HesabimPage() {
  const { user } = useAuth()
  const [token, setToken] = useState<string | null>(null)
  const [generating, setGenerating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  async function generate() {
    setGenerating(true)
    setError(null)
    setCopied(false)
    try {
      const res = await api.generateGitToken()
      setToken(res.token)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Anahtar oluşturulamadı')
    } finally {
      setGenerating(false)
    }
  }

  async function copy() {
    if (!token) return
    await navigator.clipboard.writeText(token)
    setCopied(true)
  }

  const subject = user?.subject ?? ''
  const cloneExample = `git clone http://${subject}:${token ?? '<anahtar>'}@${window.location.host}/git/<repo>.git`

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Hesabım</h1>
          <p className="page-subtitle">Git üzerinden clone/push yapmak için kendi kişisel anahtarınız</p>
        </div>
      </div>

      <div className="card">
        <p>
          Git'e paylaşılan bir şifreyle değil, kendinize ait bir anahtarla bağlanırsınız. Panelde
          hangi repolara erişebiliyorsanız git'te de aynı repolara erişebilirsiniz — ayrı bir izin
          sistemi yok.
        </p>

        <div className="form-actions">
          <button type="button" className="btn-primary" disabled={generating} onClick={generate}>
            {token ? 'Yeni anahtar oluştur (eskisini geçersiz kılar)' : 'Anahtar oluştur'}
          </button>
        </div>

        {error && <p className="error">{error}</p>}

        {token && (
          <div className="field">
            <label>Anahtarınız</label>
            <p className="error">
              Bu anahtar bir daha gösterilmeyecek — şimdi bir yere kaydedin.
            </p>
            <textarea
              readOnly
              rows={2}
              value={token}
              spellCheck={false}
              onFocus={(e) => e.currentTarget.select()}
            />
            <div className="form-actions">
              <button type="button" className="btn-ghost" onClick={copy}>
                {copied ? 'Kopyalandı' : 'Kopyala'}
              </button>
            </div>
            <label>Örnek kullanım</label>
            <textarea readOnly rows={2} value={cloneExample} spellCheck={false} />
          </div>
        )}
      </div>
    </div>
  )
}
