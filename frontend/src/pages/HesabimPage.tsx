import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError, type GitEmails, type GitTokenInfo } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { CopyButton } from '../components/CopyButton'
import { KeyIcon, TerminalIcon } from '../components/icons'
import { formatDate } from '../labels'

// HesabimPage is where somebody sets their own machine up for git and
// sees which machines are already set up.
//
// It is written around the one-line installer, not around the manual
// token form it used to lead with. `devplatform-login` mints the token
// itself, caches it, and lines up `git config user.email` at the same
// time (see cmd/devplatform-login) — so in the normal case nobody presses
// a button here at all. What stays is the part a person genuinely acts
// on: the list of machines that currently have access, and the ability to
// cut one off. Manual key creation survives one disclosure down, for a
// machine or tool the CLI cannot run on.
export function HesabimPage() {
  const { user } = useAuth()
  const [tokens, setTokens] = useState<GitTokenInfo[] | null>(null)
  const [listError, setListError] = useState<string | null>(null)
  const [cliAvailable, setCliAvailable] = useState<boolean | null>(null)

  function reload() {
    api
      .listGitTokens()
      .then(setTokens)
      .catch((err) => setListError(err instanceof ApiError ? err.message : 'Anahtarlar yüklenemedi'))
  }

  useEffect(reload, [])
  useEffect(() => {
    api.loginCliAvailable().then(setCliAvailable)
  }, [])

  async function revoke(id: string, label: string) {
    if (
      !confirm(
        `"${label}" anahtarı iptal edilsin mi? Bu anahtarı kullanan makine artık git işlemi yapamaz.`,
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

  return (
    <div className="page account-page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Hesabım</h1>
          <p className="page-subtitle">{user?.email}</p>
        </div>
      </div>

      {cliAvailable && <SetupCard />}

      <div className="section-title">
        <h2>Bağlı makineler</h2>
        {tokens && tokens.length > 0 && <span className="badge badge-neutral">{tokens.length}</span>}
      </div>
      <div className="card">
        {listError && (
          <p className="error" style={{ padding: '14px 16px' }}>
            {listError}
          </p>
        )}
        {tokens === null && !listError && <p className="empty-state">Yükleniyor...</p>}
        {tokens?.length === 0 && (
          <p className="empty-state">
            {cliAvailable
              ? 'Henüz bağlı makine yok. Yukarıdaki komutu çalıştırınca burada görünecek.'
              : 'Henüz bağlı makine yok.'}
          </p>
        )}
        {tokens && tokens.length > 0 && (
          <ul className="row-list">
            {tokens.map((t) => (
              <li key={t.id}>
                <div className="row-main">
                  <KeyIcon className="muted" />
                  <span className="row-title">{t.label || '(etiketsiz)'}</span>
                  <div className="spacer" />
                  <span className="row-meta-inline">{formatDate(t.createdAt)}</span>
                  <button
                    type="button"
                    className="link-button danger"
                    onClick={() => revoke(t.id, t.label || '(etiketsiz)')}
                  >
                    İptal et
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      <ManualKey onCreated={reload} />

      <GitEmailsSection platformEmail={user?.email ?? ''} />
    </div>
  )
}

// The whole setup, in one command. This is the page's headline because it
// is the only thing most people ever need from it.
function SetupCard() {
  const command = `irm ${window.location.origin}/api/devplatform-login/install.ps1 | iex`

  return (
    <div className="setup-card">
      <div className="setup-head">
        <span className="setup-icon">
          <TerminalIcon />
        </span>
        <div>
          <p className="setup-title">Makineni bağla</p>
          <p className="setup-sub">
            PowerShell'de bir kere çalıştır. Kullanıcı adı ve Windows şifreni sorar; gerisini kendi
            halleder.
          </p>
        </div>
      </div>

      <div className="setup-command">
        <code>{command}</code>
        <CopyButton value={command} label="Kurulum komutunu kopyala" />
      </div>

      <ol className="setup-steps">
        <li>Git anahtarını oluşturup bu makineye kaydeder — bir daha şifre sorulmaz.</li>
        <li>
          Commit'lerin bu hesabın adresiyle imzalansın diye <code>git config user.email</code> ayarını
          da hizalar.
        </li>
        <li>Sonrası normal git: clone, commit, push.</li>
      </ol>
    </div>
  )
}

// Manual key creation, kept behind a disclosure. Needed only where the CLI
// cannot run (a build agent, a machine without PowerShell) — leading with
// it made the ordinary path look like the exceptional one.
function ManualKey({ onCreated }: { onCreated: () => void }) {
  const [open, setOpen] = useState(false)
  const [label, setLabel] = useState('')
  const [newToken, setNewToken] = useState<string | null>(null)
  const [generating, setGenerating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function generate(e: FormEvent) {
    e.preventDefault()
    if (!label.trim()) return
    setGenerating(true)
    setError(null)
    try {
      const res = await api.generateGitToken(label.trim())
      setNewToken(res.token)
      setLabel('')
      onCreated()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Anahtar oluşturulamadı')
    } finally {
      setGenerating(false)
    }
  }

  if (!open) {
    return (
      <p className="account-aside">
        Komutu çalıştıramadığın bir makine mi var (build sunucusu gibi)?{' '}
        <button type="button" className="link-button" onClick={() => setOpen(true)}>
          Elle anahtar oluştur
        </button>
      </p>
    )
  }

  return (
    <div className="card manual-key">
      <div className="card-body">
        <form onSubmit={generate} className="inline-form">
          <input
            type="text"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="örn. build sunucusu"
            aria-label="Anahtar etiketi"
            autoFocus
          />
          <button type="submit" className="btn-primary" disabled={generating || !label.trim()}>
            {generating ? 'Oluşturuluyor...' : 'Oluştur'}
          </button>
          <button type="button" className="btn-secondary" onClick={() => setOpen(false)}>
            Kapat
          </button>
        </form>
        <p className="field-hint">Etiket, aşağıdaki listede hangi makine olduğunu ayırt etmen için.</p>

        {error && <p className="error">{error}</p>}

        {newToken && (
          <div className="new-token">
            <p className="new-token-warn">Bu anahtar bir daha gösterilmeyecek — şimdi kaydet.</p>
            <div className="setup-command">
              <code>{newToken}</code>
              <CopyButton value={newToken} label="Anahtarı kopyala" />
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

// Lets a person confirm which git author addresses are theirs.
//
// A commit records whatever `git config user.email` was set to on the
// machine that made it, and nothing in the commit links that to a panel
// account. The login CLI now lines the two up at setup time, so this
// section is a fallback rather than a step: it answers "why is my graph
// empty" for commits made before the CLI existed, or pushed from
// somewhere else entirely.
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
        <h2>Commit imzaların</h2>
      </div>
      <div className="card">
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
                  className="link-button"
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
                  className="link-button danger"
                  disabled={busy}
                  onClick={() => run(() => api.removeMyGitEmail(email), 'Kaldırılamadı')}
                >
                  Kaldır
                </button>
              </div>
            </li>
          ))}
        </ul>

        {error && (
          <p className="error" style={{ padding: '0 16px 12px' }}>
            {error}
          </p>
        )}
      </div>

      {!showManual ? (
        <p className="account-aside">
          Katkı grafiğinde eksik commit'lerin mi var?{' '}
          <button type="button" className="link-button" onClick={() => setShowManual(true)}>
            Elle adres ekle
          </button>
        </p>
      ) : (
        <div className="card manual-key">
          <div className="card-body">
            <form onSubmit={addManually} className="inline-form">
              <input
                type="text"
                value={input}
                onChange={(e) => setInput(e.target.value)}
                placeholder="ornek@gmail.com"
                aria-label="Git e-posta adresi"
                spellCheck={false}
                autoFocus
              />
              <button type="submit" className="btn-primary" disabled={busy || !input.trim()}>
                {busy ? 'Ekleniyor...' : 'Ekle'}
              </button>
              <button type="button" className="btn-secondary" onClick={() => setShowManual(false)}>
                Kapat
              </button>
            </form>
            <p className="field-hint">
              O adresle atılmış commit'ler de senin katkı grafiğinde sayılmaya başlar.
            </p>
          </div>
        </div>
      )}
    </>
  )
}
