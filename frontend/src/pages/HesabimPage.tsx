import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError, type GitEmails, type GitTokenInfo } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { formatDate } from '../labels'

// HesabimPage lets the signed-in person manage their own git
// credentials — see
// docs/superpowers/specs/2026-09-03-cli-git-login-design.md. A person
// can have several active tokens at once (one per machine/CLI login),
// each independently revocable — generating a new one never
// invalidates an existing one (see backend/internal/gittoken). The raw
// value of a newly generated token is shown exactly once, right after
// generation; it is never stored or re-displayed — only its SHA-256
// hash persists on the server.
export function HesabimPage() {
  const { user } = useAuth()
  const [tokens, setTokens] = useState<GitTokenInfo[] | null>(null)
  const [listError, setListError] = useState<string | null>(null)
  const [label, setLabel] = useState('')
  const [newToken, setNewToken] = useState<string | null>(null)
  const [generating, setGenerating] = useState(false)
  const [genError, setGenError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  function reload() {
    api
      .listGitTokens()
      .then(setTokens)
      .catch((err) => setListError(err instanceof ApiError ? err.message : 'Anahtarlar yüklenemedi'))
  }

  useEffect(reload, [])

  async function generate(e: FormEvent) {
    e.preventDefault()
    if (!label.trim()) return
    setGenerating(true)
    setGenError(null)
    setCopied(false)
    try {
      const res = await api.generateGitToken(label.trim())
      setNewToken(res.token)
      setLabel('')
      reload()
    } catch (err) {
      setGenError(err instanceof ApiError ? err.message : 'Anahtar oluşturulamadı')
    } finally {
      setGenerating(false)
    }
  }

  async function revoke(id: string) {
    if (
      !confirm(
        'Bu anahtarı iptal etmek istediğinize emin misiniz? Bu anahtarı kullanan makine/araç artık git işlemi yapamaz.',
      )
    ) {
      return
    }
    try {
      await api.revokeMyGitToken(id)
      reload()
    } catch (err) {
      setListError(err instanceof ApiError ? err.message : 'İptal edilemedi')
    }
  }

  async function copy() {
    if (!newToken) return
    try {
      await navigator.clipboard.writeText(newToken)
      setCopied(true)
    } catch {
      setGenError('Panoya kopyalanamadı — anahtarı aşağıdaki kutudan seçip elle kopyalayın.')
    }
  }

  const subject = user?.subject ?? ''
  const cloneExample = `git clone http://${encodeURIComponent(subject)}:${newToken ?? '<anahtar>'}@${window.location.host}/git/<repo>.git`

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Hesabım</h1>
          <p className="page-subtitle">Git üzerinden clone/push yapmak için kişisel anahtarlarınız</p>
        </div>
      </div>

      <div className="card">
        <p>
          Git'e paylaşılan bir şifreyle değil, kendinize ait anahtarlarla bağlanırsınız. Panelde hangi
          repolara erişebiliyorsanız git'te de aynı repolara erişebilirsiniz — ayrı bir izin sistemi
          yok. Birden fazla anahtarınız olabilir (örn. her makine için ayrı) — yeni bir anahtar
          oluşturmak diğerlerini geçersiz kılmaz.
        </p>

        {listError && <p className="error">{listError}</p>}
        {tokens === null && !listError && <p className="empty-state">Yükleniyor...</p>}
        {tokens?.length === 0 && <p className="empty-state">Henüz aktif bir anahtarınız yok.</p>}
        {tokens && tokens.length > 0 && (
          <ul className="row-list">
            {tokens.map((t) => (
              <li key={t.id}>
                <div className="row-main">
                  <span className="row-title">{t.label || '(etiketsiz)'}</span>
                  <span className="spacer" />
                  <span className="muted">{formatDate(t.createdAt)}</span>
                  <button type="button" className="btn-ghost" onClick={() => revoke(t.id)}>
                    İptal et
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>Yeni anahtar oluştur</h2>
      </div>
      <div className="card">
        <div className="card-body">
          <form onSubmit={generate} className="stacked-form">
            <div className="field">
              <label htmlFor="token-label">Etiket</label>
              <input
                id="token-label"
                type="text"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                placeholder="örn. iş dizüstü bilgisayarım"
              />
            </div>
            <div className="form-actions">
              <button type="submit" className="btn-primary" disabled={generating || !label.trim()}>
                {generating ? 'Oluşturuluyor...' : 'Anahtar oluştur'}
              </button>
            </div>
            {genError && <p className="error">{genError}</p>}
          </form>

          {newToken && (
            <div className="field">
              <label>Yeni anahtarınız</label>
              <p className="error">Bu anahtar bir daha gösterilmeyecek — şimdi bir yere kaydedin.</p>
              <textarea
                readOnly
                rows={2}
                value={newToken}
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

      <GitEmailsSection platformEmail={user?.email ?? ''} />
    </div>
  )
}


// Lets a person confirm which git author addresses are theirs.
//
// A commit records whatever `git config user.email` was set to on the
// machine that made it — usually not the address the platform knows
// someone by — and nothing in the commit links the two. So the git
// server reports what it saw on each of this person's pushes
// (`suggestions`) and they answer with one click. Nobody has to look up
// or type an address; the manual field below is only a fallback for
// commits pushed some other way (straight to GitHub, say).
function GitEmailsSection({ platformEmail }: { platformEmail: string }) {
  const [emails, setEmails] = useState<GitEmails | null>(null)
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [showManual, setShowManual] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api
      .listMyGitEmails()
      .then(setEmails)
      .catch((err) => setError(err instanceof ApiError ? err.message : 'E-postalar yüklenemedi'))
  }, [])

  // Every mutating call returns the full updated lists, so each of these
  // just swaps state in rather than re-fetching.
  async function run(action: () => Promise<GitEmails>, failure: string) {
    setBusy(true)
    setError(null)
    try {
      setEmails(await action())
    } catch (err) {
      setError(err instanceof ApiError ? err.message : failure)
    } finally {
      setBusy(false)
    }
  }

  async function addManually(e: FormEvent) {
    e.preventDefault()
    if (!input.trim()) return
    await run(() => api.claimMyGitEmail(input.trim()), 'E-posta eklenemedi')
    setInput('')
  }

  return (
    <>
      <div className="section-title">
        <h2>Git e-postaların</h2>
      </div>
      <div className="card">
        <div className="card-body">
          <p className="muted" style={{ fontSize: 13, marginTop: 0 }}>
            Katkı grafiğin, commit'lerini üzerlerindeki e-posta imzasına bakarak buluyor. Git, commit
            atarken kendi bilgisayarındaki <code>git config user.email</code> ne yazıyorsa onu damgalıyor
            — bu adres panel hesabındakinden farklı olabilir. Push attığında platform gördüğü imzaları
            burada sana sorar; sadece onaylaman yeterli.
          </p>

          {emails && emails.suggestions.length > 0 && (
            <div className="suggest-box">
              <p className="suggest-lead">Push'larında şu imzayı gördük — bu sen misin?</p>
              {emails.suggestions.map((email) => (
                <div key={email} className="suggest-row">
                  <span className="mono">{email}</span>
                  <div className="spacer" />
                  <button
                    type="button"
                    className="btn-primary btn-sm"
                    disabled={busy}
                    onClick={() => run(() => api.claimMyGitEmail(email), 'Onaylanamadı')}
                  >
                    Evet, benim
                  </button>
                  <button
                    type="button"
                    className="btn-ghost"
                    disabled={busy}
                    onClick={() => run(() => api.dismissMyGitEmail(email), 'İşlenemedi')}
                  >
                    Ben değilim
                  </button>
                </div>
              ))}
            </div>
          )}

          <ul className="row-list">
            <li>
              <div className="row-main">
                <span className="row-title mono">{platformEmail}</span>
                <div className="spacer" />
                <span className="badge badge-neutral">Panel hesabın</span>
              </div>
            </li>
            {emails?.claimed.map((email) => (
              <li key={email}>
                <div className="row-main">
                  <span className="row-title mono">{email}</span>
                  <div className="spacer" />
                  <button
                    type="button"
                    className="btn-ghost"
                    disabled={busy}
                    onClick={() => run(() => api.removeMyGitEmail(email), 'Kaldırılamadı')}
                  >
                    Kaldır
                  </button>
                </div>
              </li>
            ))}
          </ul>

          {error && <p className="error">{error}</p>}

          {!showManual && (
            <button type="button" className="btn-ghost" onClick={() => setShowManual(true)}>
              Elle adres ekle
            </button>
          )}
          {showManual && (
            <form onSubmit={addManually} className="stacked-form">
              <div className="field">
                <label htmlFor="git-email">E-posta adresi</label>
                <input
                  id="git-email"
                  type="text"
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  placeholder="ornek@gmail.com"
                  spellCheck={false}
                />
              </div>
              <div className="form-actions">
                <button type="submit" className="btn-primary" disabled={busy || !input.trim()}>
                  {busy ? 'Ekleniyor...' : 'Ekle'}
                </button>
              </div>
            </form>
          )}
        </div>
      </div>
    </>
  )
}
