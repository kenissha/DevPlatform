import { useState, type FormEvent } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'

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
        <h1>DevPlatform</h1>
        <p className="muted">
          Normalde buraya, kurumsal girişinizi zaten yapmış olan sistemden bir{' '}
          <code>?token=</code> bağlantısıyla yönlendirilirsiniz. Geliştirme/test için, aşağıya
          geçerli bir JWT yapıştırarak da giriş yapabilirsiniz.
        </p>
        <form onSubmit={handleSubmit}>
          <label htmlFor="token">JWT</label>
          <textarea
            id="token"
            value={tokenInput}
            onChange={(e) => setTokenInput(e.target.value)}
            rows={4}
            placeholder="eyJhbGciOi..."
            spellCheck={false}
          />
          <button type="submit" disabled={!tokenInput.trim()}>
            Giriş yap
          </button>
        </form>
      </div>
    </div>
  )
}
