import { poolTypeDot } from './poolTypes'

export function formatHostCount(count: number): string {
  if (count >= 1000000) return (count / 1000000).toFixed(1) + 'M'
  if (count >= 1000) return (count / 1000).toFixed(1) + 'K'
  return count.toString()
}

export function getHostCount(cidr: string): number {
  if (!cidr) return 0
  const prefix = parseInt(cidr.split('/')[1], 10)
  // Only IPv4 prefix lengths are meaningful here; anything else would produce
  // a fractional or absurdly large host count.
  if (isNaN(prefix) || prefix < 0 || prefix > 32) return 0
  return Math.pow(2, 32 - prefix)
}

const DAYS_PER_MONTH = 30.4375

/** Rounds a runway in days to the coarsest unit a planner reads at a glance. */
export function formatRunway(days: number): string {
  if (!isFinite(days) || days < 0) return 'unknown'
  if (days < 1) return 'less than a day'
  if (days < 30) {
    const d = Math.round(days)
    return d + ' day' + (d === 1 ? '' : 's')
  }
  if (days < 365) {
    const m = Math.max(1, Math.round(days / DAYS_PER_MONTH))
    return m + ' month' + (m === 1 ? '' : 's')
  }
  const y = Math.round((days / 365) * 10) / 10
  return y + ' year' + (y === 1 ? '' : 's')
}

export function formatTimeAgo(timestamp: string): string {
  if (!timestamp) return 'unknown'
  const date = new Date(timestamp)
  if (isNaN(date.getTime())) return 'unknown'
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
  return poolTypeDot(type)
}

export function getUtilizationColor(util: number): string {
  if (util > 80) return 'bg-red-500'
  if (util > 60) return 'bg-amber-500'
  return 'bg-green-500'
}

export function getProviderBadgeClass(provider: string): string {
  const classes: Record<string, string> = {
    aws: 'bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300',
    gcp: 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300',
    azure: 'bg-sky-100 text-sky-700 dark:bg-sky-900 dark:text-sky-300',
  }
  return classes[provider] || 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300'
}

export function getTierBadgeClass(tier: string): string {
  const classes: Record<string, string> = {
    prd: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
    stg: 'bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300',
    dev: 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300',
    sbx: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300',
  }
  return classes[tier] || 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300'
}

export function getStatusBadgeClass(status: string): string {
  const classes: Record<string, string> = {
    active: 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300',
    planned: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300',
    deprecated: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
    stale: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300',
    deleted: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
    completed: 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300',
    running: 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300',
    pending: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300',
    failed: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
    // locked/disabled/revoked/idle: account & key lifecycle vocabulary. 'idle' here
    // is NOT the self-upgrade "idle" (UpdatesPage) — that's the normal at-rest
    // state, where this amber would misread as attention-needed.
    locked: 'bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300',
    disabled: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
    revoked: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
    idle: 'bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300',
  }
  return classes[status] || 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300'
}

export function getActionBadgeClass(action: string): string {
  if (action?.includes('create')) return 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300'
  if (action?.includes('update')) return 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300'
  if (action?.includes('delete')) return 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300'
  return 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300'
}
