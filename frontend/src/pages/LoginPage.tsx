import { useState, type FormEvent } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { LogoMark } from '../components/icons'

export function LoginPage() {
  const { status, login } = useAuth()
  const [tokenInput, setTokenInput] = useState('')

  if (status === 'authenticated') {
    return <Navigate to="/" replace />
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (tokenInput.trim()) {
      login(tokenInput.trim())
    }
  }

  return (
    <div className="login-page">
      <div className="login-card">
        <div className="login-brand">
          <LogoMark className="brand-mark" />
          <span>DevPlatform</span>
        </div>
        <p className="login-note">
          Kurumsal girişinizi zaten yaptığınız sistemden bu panele yönlendirilirsiniz; oturumunuz
          oradan devredilir.
        </p>

        <div className="login-divider">Geliştirme girişi</div>

        <form onSubmit={handleSubmit}>
          <div className="field">
            <label htmlFor="token">JWT</label>
            <textarea
              id="token"
              value={tokenInput}
              onChange={(e) => setTokenInput(e.target.value)}
              rows={4}
              placeholder="eyJhbGciOi..."
              spellCheck={false}
            />
          </div>
          <div className="form-actions">
            <button type="submit" className="btn-primary" disabled={!tokenInput.trim()}>
              Giriş yap
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
