import { useEffect, useRef, type ReactNode } from 'react'

// Modal is the shared dialog shell: backdrop, Escape to close, click
// outside to close, and focus moved onto the close button so a keyboard
// user isn't left behind on the page underneath.
//
// Extracted from the task detail panel when a second dialog appeared —
// two copies of these three keyboard behaviours is exactly where one of
// them starts silently missing one.
//
// Two looks, one shell. The default is a compact dialog for a decision or
// a field or two. `variant="panel"` is for a form somebody sits inside for
// a while — wider, with a titled header band that keeps saying what is
// being configured while they scroll through it. The behaviour is
// identical; only the frame changes, so a dialog can never be nicer at the
// cost of being less usable.
export function Modal({
  title,
  subtitle,
  icon,
  variant = 'dialog',
  onClose,
  children,
}: {
  title: string
  subtitle?: string
  icon?: ReactNode
  variant?: 'dialog' | 'panel'
  onClose: () => void
  children: ReactNode
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    closeButtonRef.current?.focus()
  }, [])

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onClose])

  const isPanel = variant === 'panel'

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className={isPanel ? 'modal modal-panel' : 'modal'}
        role="dialog"
        aria-modal="true"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-header">
          {isPanel && icon && <span className="modal-icon">{icon}</span>}
          <div className="modal-heading">
            <h3>{title}</h3>
            {subtitle && <p className="modal-subtitle">{subtitle}</p>}
          </div>
          <button
            type="button"
            className="icon-button"
            ref={closeButtonRef}
            onClick={onClose}
            aria-label="Kapat"
            title="Kapat"
          >
            <CloseGlyph />
          </button>
        </div>
        <div className={isPanel ? 'modal-body' : 'modal-body is-plain'}>{children}</div>
      </div>
    </div>
  )
}

function CloseGlyph() {
  return (
    <svg
      viewBox="0 0 16 16"
      width={16}
      height={16}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      aria-hidden="true"
    >
      <path d="M4 4l8 8M12 4l-8 8" />
    </svg>
  )
}
