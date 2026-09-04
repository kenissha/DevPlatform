import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { ThemeContext, type ThemePreference } from './useTheme'

const STORAGE_KEY = 'devplatform.theme'

// Reads the stored preference. Anything unrecognised (including nothing
// stored yet, or a value from a future version) falls back to 'system',
// which is also the shipped default.
function storedPreference(): ThemePreference {
  const raw = localStorage.getItem(STORAGE_KEY)
  return raw === 'light' || raw === 'dark' ? raw : 'system'
}

function systemPrefersDark(): boolean {
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

// ThemeProvider owns the light/dark choice and applies it by stamping
// data-theme on <html>, which index.css's palette blocks key off. It
// deliberately stamps NOTHING for 'system' — an un-stamped document is
// what lets the CSS's prefers-color-scheme media query take over, so
// following the OS needs no JS beyond leaving the attribute off.
export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreference] = useState<ThemePreference>(storedPreference)
  const [systemDark, setSystemDark] = useState(systemPrefersDark)

  // Only matters while preference is 'system', but the listener is
  // always attached: subscribing conditionally would mean re-running
  // this effect on every preference change for no benefit.
  useEffect(() => {
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (e: MediaQueryListEvent) => setSystemDark(e.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])

  useEffect(() => {
    const root = document.documentElement
    if (preference === 'system') {
      root.removeAttribute('data-theme')
    } else {
      root.setAttribute('data-theme', preference)
    }
  }, [preference])

  const resolved = preference === 'system' ? (systemDark ? 'dark' : 'light') : preference

  const toggle = useCallback(() => {
    // Pins to the opposite of what's on screen, so the button always
    // does the visible thing regardless of whether the current theme
    // came from the OS or an earlier pick.
    const next: ThemePreference = resolved === 'dark' ? 'light' : 'dark'
    localStorage.setItem(STORAGE_KEY, next)
    setPreference(next)
  }, [resolved])

  return (
    <ThemeContext.Provider value={{ preference, resolved, toggle }}>{children}</ThemeContext.Provider>
  )
}
