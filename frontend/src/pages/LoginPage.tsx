import { useState, type FormEvent } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { signDevToken } from '../auth/devToken'
import { LogoMark } from '../components/icons'

export function LoginPage() {
  const { status, login } = useAuth()
  const [tokenInput, setTokenInput] = useState('')
  const [devLoggingIn, setDevLoggingIn] = useState(false)

  if (status === 'authenticated') {
    return <Navigate to="/" replace />
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (tokenInput.trim()) {
      login(tokenInput.trim())
    }
  }

  // One click, no JWT to hand-craft: signs a token client-side (see
  // devToken.ts) with the same default secret the local backend falls
  // back to, and logs straight in as an Admin — the SSO devir'i
  // taklit eden ?token= flow's manual-paste step, automated away.
  async function handleDevLogin() {
    setDevLoggingIn(true)
    try {
      const token = await signDevToken('local-dev', 'dev@localhost', 'admin')
      login(token)
    } finally {
      setDevLoggingIn(false)
    }
  }

  return (
    <div className="login-page">
      <div className="login-card">
        <div className="login-brand">
          <LogoMark className="brand-mark" />
          <span>STK Atölye</span>
        </div>
        <p className="login-note">
          Kurumsal girişinizi zaten yaptığınız sistemden bu panele yönlendirilirsiniz; oturumunuz
          oradan devredilir.
        </p>

        {/* Ham JWT yapıştırma kutusu: gerçek giriş yolu değil, sadece
            DevPlatform'un kendisini geliştirirken elle token denemek için bir
            kısayol — production build'de (gerçek kullanıcıların gördüğü
            sürümde) hiç render edilmiyor, sadece `npm run dev` ile yerel
            geliştirmede görünür. */}
        {import.meta.env.DEV && (
          <>
            <div className="login-divider">Geliştirme girişi</div>

            <div className="form-actions">
              <button type="button" className="btn-primary" onClick={handleDevLogin} disabled={devLoggingIn}>
                {devLoggingIn ? 'Giriş yapılıyor...' : 'Yönetici olarak gir (yerel)'}
              </button>
            </div>

            <p className="login-note" style={{ marginTop: 16 }}>
              veya elle bir JWT ile:
            </p>
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
          </>
        )}
      </div>
    </div>
  )
}
