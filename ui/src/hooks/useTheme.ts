import { createContext, useContext, useEffect, useState, useCallback } from 'react'

export type ThemeMode = 'light' | 'dark' | 'system'

interface ThemeContextValue {
  mode: ThemeMode
  resolvedTheme: 'light' | 'dark'
  cycle: () => void
  toggle: () => void
}

export const ThemeContext = createContext<ThemeContextValue>({
  mode: 'system',
  resolvedTheme: 'light',
  cycle: () => {},
  toggle: () => {},
})

export function useTheme() {
  return useContext(ThemeContext)
}

export function useThemeState(): ThemeContextValue {
  const [mode, setMode] = useState<ThemeMode>(() => {
    const stored = localStorage.getItem('theme')
    if (stored === 'light' || stored === 'dark' || stored === 'system') return stored
    return 'system'
  })

  const apply = useCallback((m: ThemeMode) => {
    const isDark =
      m === 'dark' ||
      (m === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
    document.documentElement.classList.toggle('dark', isDark)
  }, [])

  const cycle = useCallback(() => {
    setMode((prev) => {
      const modes: ThemeMode[] = ['light', 'dark', 'system']
      const next = modes[(modes.indexOf(prev) + 1) % modes.length]
      localStorage.setItem('theme', next)
      apply(next)
      return next
    })
  }, [apply])

  useEffect(() => {
    apply(mode)
  }, [mode, apply])

  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = () => {
      if (mode === 'system') apply('system')
    }
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, [mode, apply])

  const resolvedTheme: 'light' | 'dark' =
    mode === 'system'
      ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
      : mode

  const toggle = useCallback(() => {
    setMode((prev) => {
      const resolved = prev === 'system'
        ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
        : prev
      const next: ThemeMode = resolved === 'dark' ? 'light' : 'dark'
      localStorage.setItem('theme', next)
      apply(next)
      return next
    })
  }, [apply])

  return { mode, resolvedTheme, cycle, toggle }
}
