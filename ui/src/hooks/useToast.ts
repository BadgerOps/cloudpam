import { createContext, useContext, useCallback, useState } from 'react'

export interface Toast {
  id: string
  message: string
  type: 'info' | 'error' | 'success'
}

export interface ToastContextValue {
  toasts: Toast[]
  showToast: (message: string, type?: 'info' | 'error' | 'success') => void
}

export const ToastContext = createContext<ToastContextValue>({
  toasts: [],
  showToast: () => {},
})

export function useToastState(): ToastContextValue {
  const [toasts, setToasts] = useState<Toast[]>([])

  const showToast = useCallback((message: string, type: 'info' | 'error' | 'success' = 'info') => {
    const id = Math.random().toString(36).slice(2)
    setToasts((prev) => [...prev, { id, message, type }])
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id))
    }, 3000)
  }, [])

  return { toasts, showToast }
}

export function useToast(): ToastContextValue {
  return useContext(ToastContext)
}
