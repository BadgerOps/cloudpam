import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

export type MetricTone = 'neutral' | 'ok' | 'info' | 'warning' | 'critical'

export interface MetricCardProps {
  label: string
  value: string
  icon?: ReactNode
  tone?: MetricTone
  detail?: ReactNode
  loading?: boolean
  action?: { label: string; to: string }
  children?: ReactNode
}

const TONE_STYLES: Record<MetricTone, { bar: string; value: string }> = {
  neutral: { bar: 'border-l-gray-200 dark:border-l-gray-700', value: 'text-gray-900 dark:text-gray-100' },
  ok: { bar: 'border-l-green-500', value: 'text-gray-900 dark:text-gray-100' },
  info: { bar: 'border-l-blue-500', value: 'text-blue-700 dark:text-blue-300' },
  warning: { bar: 'border-l-amber-500', value: 'text-amber-700 dark:text-amber-300' },
  critical: { bar: 'border-l-red-500', value: 'text-red-700 dark:text-red-300' },
}

export function MetricCard({ label, value, icon, tone = 'neutral', detail, loading = false, action, children }: MetricCardProps) {
  const styles = TONE_STYLES[tone]
  return (
    <section
      aria-label={label}
      aria-busy={loading || undefined}
      data-tone={tone}
      className={`flex flex-col bg-white dark:bg-gray-800 rounded-lg border border-l-4 border-gray-200 dark:border-gray-700 ${styles.bar} p-4`}
    >
      <div className="flex items-start gap-3">
        {icon && <div className="p-2 bg-gray-50 dark:bg-gray-900/40 rounded-lg flex-shrink-0">{icon}</div>}
        <div className="min-w-0 flex-1">
          <div className="text-sm text-gray-500 dark:text-gray-400">{label}</div>
          <div className={`text-2xl font-bold ${loading ? 'text-gray-300 dark:text-gray-600' : styles.value}`}>
            {loading ? '…' : value}
          </div>
          {detail && !loading && <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{detail}</div>}
        </div>
      </div>
      {children && !loading && <div className="mt-3">{children}</div>}
      {action && (
        <Link
          to={action.to}
          className="mt-auto pt-3 text-xs font-medium text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
        >
          {action.label}
        </Link>
      )}
    </section>
  )
}
