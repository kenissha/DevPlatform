import { useEffect, useRef, type ReactNode } from 'react'

// Modal is the shared dialog shell: backdrop, Escape to close, click
// outside to close, and focus moved onto the close button so a keyboard
// user isn't left behind on the page underneath.
//
// Extracted from the task detail panel when a second dialog appeared —
// two copies of these three keyboard behaviours is exactly where one of
// them starts silently missing one.
export function Modal({
  title,
  onClose,
  children,
}: {
  title: string
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

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h3>{title}</h3>
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
        {children}
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
