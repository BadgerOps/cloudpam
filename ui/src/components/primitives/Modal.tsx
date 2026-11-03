import { useEffect, useId, useRef } from 'react'
import type { ReactNode } from 'react'
import { X } from 'lucide-react'

export type ModalSize = 'sm' | 'md' | 'lg'

export interface ModalProps {
  open: boolean
  title: string
  description?: string
  size?: ModalSize
  onClose: () => void
  footer?: ReactNode
  children: ReactNode
}

const SIZE_CLASS: Record<ModalSize, string> = {
  sm: 'md:max-w-[420px]',
  md: 'md:max-w-[480px]',
  lg: 'md:max-w-[720px]',
}

const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

function focusables(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE))
}

export function Modal({ open, title, description, size = 'md', onClose, footer, children }: ModalProps) {
  const panelRef = useRef<HTMLDivElement>(null)
  const bodyRef = useRef<HTMLDivElement>(null)
  const titleId = useId()
  const descriptionId = useId()

  useEffect(() => {
    if (!open) return
    const panel = panelRef.current
    const previouslyFocused = document.activeElement as HTMLElement | null
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    if (panel) {
      const first = focusables(bodyRef.current ?? panel)[0] ?? focusables(panel)[0]
      ;(first ?? panel).focus()
    }

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key !== 'Tab' || !panel) return
      const items = focusables(panel)
      if (items.length === 0) {
        e.preventDefault()
        panel.focus()
        return
      }
      const first = items[0]
      const last = items[items.length - 1]
      const active = document.activeElement
      if (e.shiftKey && (active === first || !panel.contains(active))) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && (active === last || !panel.contains(active))) {
        e.preventDefault()
        first.focus()
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      document.body.style.overflow = previousOverflow
      previouslyFocused?.focus?.()
    }
  }, [open, onClose])

  if (!open) return null

  return (
    <div
      data-testid="modal-backdrop"
      className="fixed inset-0 z-50 flex items-end md:items-center justify-center bg-black/40"
      onClick={onClose}
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        tabIndex={-1}
        className={`bg-white dark:bg-gray-800 shadow-2xl w-full max-h-full flex flex-col rounded-t-xl md:rounded-xl outline-none ${SIZE_CLASS[size]}`}
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-start gap-3 px-5 py-4 border-b dark:border-gray-700">
          <div className="flex-1 min-w-0">
            <h2 id={titleId} className="text-base font-semibold text-gray-900 dark:text-white">{title}</h2>
            {description && (
              <p id={descriptionId} className="mt-1 text-sm text-gray-500 dark:text-gray-400">{description}</p>
            )}
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="p-1 rounded text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
        <div ref={bodyRef} className="px-5 py-4 overflow-y-auto text-sm text-gray-700 dark:text-gray-300">{children}</div>
        {footer && (
          <div className="flex items-center justify-end gap-2 px-5 py-3 border-t dark:border-gray-700 bg-gray-50 dark:bg-gray-800/60 rounded-b-xl">
            {footer}
          </div>
        )}
      </div>
    </div>
  )
}
