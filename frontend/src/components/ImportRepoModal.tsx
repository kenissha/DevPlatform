import { useEffect, useRef, useState, type FormEvent } from 'react'
import { api, ApiError } from '../api/client'
import type { ImportJob } from '../api/types'
import { Modal } from './Modal'

// How often the panel asks the server how the clone is going. Two seconds
// is slow enough to be nothing on a server and fast enough that the
// elapsed time on screen never looks stuck.
const POLL_MS = 2000

// Bringing a repository that already exists somewhere else onto the
// platform, with its history.
//
// The alternative was pushing it, and for a real project that does not
// work: the push-time secret scanner rejects the whole history if any
// commit ever held a credential, so a 600-commit repository fails after
// several minutes and there is nothing the person can do about it from
// here. An import writes the repository in directly — and then says what
// the history turned out to contain, rather than pretending the check
// never mattered.
export function ImportRepoModal({
  onClose,
  onImported,
}: {
  onClose: () => void
  onImported: () => void
}) {
  const [source, setSource] = useState('')
  const [name, setName] = useState('')
  // Touched tracks whether the person has edited the name themselves, so
  // the suggestion below stops overwriting a deliberate choice.
  const [nameTouched, setNameTouched] = useState(false)
  const [token, setToken] = useState('')
  const [job, setJob] = useState<ImportJob | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [starting, setStarting] = useState(false)
  const [elapsed, setElapsed] = useState(0)

  const suggested = suggestName(source)
  const effectiveName = nameTouched ? name : suggested

  // Poll while the clone runs. Stored in a ref so the cleanup can clear
  // whichever timer is actually pending.
  const timer = useRef<number | null>(null)
  useEffect(() => {
    if (!job || job.status !== 'running') return

    let cancelled = false
    const tick = () => {
      api
        .repoImport(job.id)
        .then((fresh) => {
          if (cancelled) return
          setJob(fresh)
          // onImported refreshes the repo list behind the modal, so the
          // new repository is already there when the person closes it.
          if (fresh.status === 'done') onImported()
        })
        // A failed poll is not a failed import — the clone is running on
        // the server regardless. Keep polling; the next one usually works.
        .catch(() => {})
        .finally(() => {
          if (!cancelled) timer.current = window.setTimeout(tick, POLL_MS)
        })
    }
    timer.current = window.setTimeout(tick, POLL_MS)

    return () => {
      cancelled = true
      if (timer.current) window.clearTimeout(timer.current)
    }
  }, [job?.id, job?.status])

  // A clone has no progress to report — git gives no percentage over
  // this path — so the elapsed seconds are the honest thing to show:
  // proof that something is still happening, without inventing a bar
  // that fills at a rate nobody measured.
  useEffect(() => {
    if (!job || job.status !== 'running') return
    const started = new Date(job.startedAt).getTime()
    const id = window.setInterval(() => setElapsed(Date.now() - started), 500)
    return () => window.clearInterval(id)
  }, [job?.id, job?.status])

  async function handleStart(e: FormEvent) {
    e.preventDefault()
    if (!source.trim() || !effectiveName.trim()) return
    setStarting(true)
    setError(null)
    try {
      setJob(await api.startRepoImport(source.trim(), effectiveName.trim(), token))
      // Dropped as soon as it has been sent. The panel has no further use
      // for a credential, and keeping one in component state is how it
      // ends up somewhere it was never meant to be.
      setToken('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'İçe aktarım başlatılamadı')
    } finally {
      setStarting(false)
    }
  }

  if (job) {
    return (
      <Modal title="Depo içe aktarılıyor" variant="panel" onClose={onClose}>
        <ImportProgress job={job} elapsed={elapsed} onClose={onClose} onRetry={() => setJob(null)} />
      </Modal>
    )
  }

  return (
    <Modal title="Git deposundan içe aktar" variant="panel" onClose={onClose}>
      <form onSubmit={handleStart} className="modal-form">
        <label className="field">
          <span className="field-label">Kaynak adresi</span>
          <input
            type="text"
            value={source}
            autoFocus
            placeholder="https://github.com/kullanici/proje.git"
            onChange={(e) => setSource(e.target.value)}
            required
          />
          <span className="field-hint">
            GitHub, GitLab ya da başka bir git sunucusu. Bütün dallar ve geçmiş olduğu gibi gelir.
          </span>
        </label>

        <label className="field">
          <span className="field-label">Buradaki adı</span>
          <input
            type="text"
            value={effectiveName}
            placeholder="proje"
            onChange={(e) => {
              setNameTouched(true)
              setName(e.target.value)
            }}
            required
          />
          <span className="field-hint">Harf, rakam, tire ve alt çizgi.</span>
        </label>

        <label className="field">
          <span className="field-label">
            Erişim anahtarı <span className="field-optional">— depo herkese açıksa gerekmez</span>
          </span>
          <input
            type="password"
            value={token}
            autoComplete="off"
            onChange={(e) => setToken(e.target.value)}
          />
          <span className="field-hint">
            Kaydedilmez. Sadece bu kopyalama için kullanılır, sonra unutulur.
          </span>
        </label>

        {error && <p className="error">{error}</p>}

        <div className="modal-actions">
          <button type="button" className="btn-secondary" onClick={onClose} disabled={starting}>
            Vazgeç
          </button>
          <button
            type="submit"
            className="btn-primary"
            disabled={starting || !source.trim() || !effectiveName.trim()}
          >
            {starting ? 'Başlatılıyor...' : 'İçe aktar'}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function ImportProgress({
  job,
  elapsed,
  onClose,
  onRetry,
}: {
  job: ImportJob
  elapsed: number
  onClose: () => void
  onRetry: () => void
}) {
  if (job.status === 'running') {
    return (
      <div className="import-state">
        <p className="import-line">
          <span className="import-spinner" aria-hidden="true" />
          <strong>{job.name}</strong> kopyalanıyor
        </p>
        <p className="import-source">{job.source}</p>
        <p className="import-elapsed">{formatElapsed(elapsed)}</p>
        <p className="field-hint">
          Büyük bir depo birkaç dakika sürebilir. Bu pencereyi kapatabilirsin — kopyalama sunucuda
          devam eder.
        </p>
        <div className="modal-actions">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Kapat
          </button>
        </div>
      </div>
    )
  }

  if (job.status === 'failed') {
    return (
      <div className="import-state">
        <p className="import-line import-failed">
          <strong>{job.name}</strong> içe aktarılamadı
        </p>
        <pre className="import-error">{job.error}</pre>
        <div className="modal-actions">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Kapat
          </button>
          <button type="button" className="btn-primary" onClick={onRetry}>
            Tekrar dene
          </button>
        </div>
      </div>
    )
  }

  const findings = job.findings ?? []
  return (
    <div className="import-state">
      <p className="import-line import-done">
        <strong>{job.name}</strong> içe aktarıldı
      </p>
      <p className="import-summary">
        {job.commits} commit · {job.branches} dal
      </p>

      {findings.length > 0 && (
        <div className="import-findings">
          <p className="import-findings-head">
            Geçmişte {findings.length} dosyada sır bulundu
          </p>
          <ul>
            {findings.map((f) => (
              <li key={`${f.pattern}:${f.path}`}>
                <code>{f.path}</code>
                <span className="tag tag-amber">{f.pattern}</span>
              </li>
            ))}
          </ul>
          {/* The import deliberately skipped the push-time scanner, so it
              owes the person this. These files may be long deleted — the
              blobs stay in the history either way, and a credential that
              was published somewhere else is still published. */}
          <p className="field-hint">
            Bu dosyalar artık depoda olmayabilir ama geçmişte duruyorlar. Hâlâ kullanılan bir şifre
            varsa değiştirilmesi gerekir — içe aktarmak onu geçersiz kılmaz.
          </p>
        </div>
      )}

      {findings.length === 0 && (
        <p className="field-hint">Geçmişte bilinen bir sır deseni bulunamadı.</p>
      )}

      <div className="modal-actions">
        <button type="button" className="btn-primary" onClick={onClose}>
          Tamam
        </button>
      </div>
    </div>
  )
}

// suggestName turns a clone URL into the repository name it would get.
// "https://github.com/kenissha/STK-React.git" → "STK-React". Saves typing
// and, more usefully, produces a name that passes the server's rule.
function suggestName(source: string): string {
  const last = source.trim().replace(/\/+$/, '').split('/').pop() ?? ''
  return last.replace(/\.git$/i, '').replace(/[^a-zA-Z0-9_-]/g, '-')
}

function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  if (minutes === 0) return `${seconds} saniye`
  return `${minutes} dk ${seconds} sn`
}
