import { createContext, useContext } from 'react'

// Split from ThemeContext.tsx so that file exports only the provider
// component — same reasoning as notifications/useNotifications.ts.

// What the person chose. 'system' is the default and deliberately
// distinct from 'light'/'dark': it means "keep following the OS", so a
// laptop that switches to dark at sunset takes the panel with it. Only
// an explicit pick pins the theme.
export type ThemePreference = 'system' | 'light' | 'dark'

export interface ThemeState {
  /** The stored preference — what the person picked, not what's showing. */
  preference: ThemePreference
  /** What is actually on screen right now, with 'system' resolved. */
  resolved: 'light' | 'dark'
  /** Pins the theme to the opposite of what's currently showing. */
  toggle: () => void
}

export const ThemeContext = createContext<ThemeState | undefined>(undefined)

export function useTheme(): ThemeState {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used within a ThemeProvider')
  return ctx
}
