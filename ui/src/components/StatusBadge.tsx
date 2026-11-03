import { getStatusBadgeClass, getProviderBadgeClass, getTierBadgeClass, getActionBadgeClass, getPoolTypeColor } from '../utils/format'

interface StatusBadgeProps {
  label: string
  status?: string
  variant?: 'status' | 'provider' | 'tier' | 'action' | 'type'
}

export default function StatusBadge({ label, status, variant = 'status' }: StatusBadgeProps) {
  if (!label) return null
  const key = status ?? label

  let className: string
  switch (variant) {
    case 'provider':
      className = getProviderBadgeClass(key)
      break
    case 'tier':
      className = getTierBadgeClass(key)
      break
    case 'action':
      className = getActionBadgeClass(key)
      break
    case 'type': {
      const dot = getPoolTypeColor(key)
      return (
        <span className="inline-flex items-center gap-1 text-xs font-medium text-gray-600 dark:text-gray-300">
          <span className={`w-2 h-2 rounded-full ${dot}`} />
          {label}
        </span>
      )
    }
    default:
      className = getStatusBadgeClass(key)
  }

  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium whitespace-nowrap ${className}`}>
      {label}
    </span>
  )
}
