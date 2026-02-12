import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'
import { ThemeContext, useThemeState } from './hooks/useTheme'

function Root() {
  const themeState = useThemeState()
  return (
    <ThemeContext.Provider value={themeState}>
      <App />
    </ThemeContext.Provider>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Root />
  </StrictMode>,
)
