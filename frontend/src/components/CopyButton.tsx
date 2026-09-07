import { useEffect, useState } from 'react'
import { CheckIcon, CopyIcon } from './icons'

// CopyButton copies one string and confirms it by swapping its own icon
// for a checkmark. The confirmation is the whole feedback mechanism: a
// clipboard write leaves no visible trace otherwise, and a person who
// isn't sure it worked will click again.
export function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 1600)
    return () => clearTimeout(timer)
  }, [copied])

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
    } catch {
      // navigator.clipboard is unavailable over plain HTTP on some
      // browsers, and there is nothing useful to say about it — the text
      // is on screen to select by hand.
    }
  }

  return (
    <button
      type="button"
      className="icon-button"
      onClick={copy}
      aria-label={label}
      title={copied ? 'Kopyalandı' : 'Kopyala'}
    >
      {copied ? <CheckIcon className="copied" /> : <CopyIcon />}
    </button>
  )
}
