import { getStatusBadgeClass, getProviderBadgeClass, getTierBadgeClass, getActionBadgeClass, getPoolTypeColor } from '../utils/format'

interface StatusBadgeProps {
  label: string
  variant?: 'status' | 'provider' | 'tier' | 'action' | 'type'
}

export default function StatusBadge({ label, variant = 'status' }: StatusBadgeProps) {
  if (!label) return null

  let className: string
  switch (variant) {
    case 'provider':
      className = getProviderBadgeClass(label)
      break
    case 'tier':
      className = getTierBadgeClass(label)
      break
    case 'action':
      className = getActionBadgeClass(label)
      break
    case 'type': {
      const dot = getPoolTypeColor(label)
      return (
        <span className="inline-flex items-center gap-1 text-xs font-medium text-gray-600">
          <span className={`w-2 h-2 rounded-full ${dot}`} />
          {label}
        </span>
      )
    }
    default:
      className = getStatusBadgeClass(label)
  }

  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${className}`}>
      {label}
    </span>
  )
}
