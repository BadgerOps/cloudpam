export function formatHostCount(count: number): string {
  if (count >= 1000000) return (count / 1000000).toFixed(1) + 'M'
  if (count >= 1000) return (count / 1000).toFixed(1) + 'K'
  return count.toString()
}

export function getHostCount(cidr: string): number {
  if (!cidr) return 0
  const prefix = parseInt(cidr.split('/')[1], 10)
  if (isNaN(prefix)) return 0
  return Math.pow(2, 32 - prefix)
}

export function formatTimeAgo(timestamp: string): string {
  if (!timestamp) return 'unknown'
  const date = new Date(timestamp)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return diffMins + ' min ago'
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return diffHours + ' hour' + (diffHours > 1 ? 's' : '') + ' ago'
  const diffDays = Math.floor(diffHours / 24)
  return diffDays + ' day' + (diffDays > 1 ? 's' : '') + ' ago'
}

export function getPoolTypeColor(type: string): string {
  const colors: Record<string, string> = {
    supernet: 'bg-purple-500',
    root: 'bg-purple-500',
    region: 'bg-blue-500',
    environment: 'bg-green-500',
    vpc: 'bg-amber-500',
    subnet: 'bg-orange-500',
    account: 'bg-amber-500',
  }
  return colors[type] || 'bg-gray-400'
}

export function getUtilizationColor(util: number): string {
  if (util > 80) return 'bg-red-500'
  if (util > 60) return 'bg-amber-500'
  return 'bg-green-500'
}

export function getProviderBadgeClass(provider: string): string {
  const classes: Record<string, string> = {
    aws: 'bg-amber-100 text-amber-700',
    gcp: 'bg-blue-100 text-blue-700',
    azure: 'bg-sky-100 text-sky-700',
  }
  return classes[provider] || 'bg-gray-100 text-gray-700'
}

export function getTierBadgeClass(tier: string): string {
  const classes: Record<string, string> = {
    prd: 'bg-red-100 text-red-700',
    stg: 'bg-amber-100 text-amber-700',
    dev: 'bg-green-100 text-green-700',
    sbx: 'bg-gray-100 text-gray-700',
  }
  return classes[tier] || 'bg-gray-100 text-gray-700'
}

export function getStatusBadgeClass(status: string): string {
  const classes: Record<string, string> = {
    active: 'bg-green-100 text-green-700',
    planned: 'bg-gray-100 text-gray-700',
    deprecated: 'bg-red-100 text-red-700',
  }
  return classes[status] || 'bg-gray-100 text-gray-700'
}

export function getActionBadgeClass(action: string): string {
  if (action?.includes('create')) return 'bg-green-100 text-green-700'
  if (action?.includes('update')) return 'bg-blue-100 text-blue-700'
  if (action?.includes('delete')) return 'bg-red-100 text-red-700'
  return 'bg-gray-100 text-gray-700'
}
