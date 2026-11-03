import { useEffect, useState, useMemo } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Server, Cpu, Cloud, Gauge } from 'lucide-react'
import { usePools } from '../hooks/usePools'
import { useAccounts } from '../hooks/useAccounts'
import { useAudit } from '../hooks/useAudit'
import { useAuth } from '../hooks/useAuth'
import { useAttention } from '../hooks/useAttention'
import type { AttentionLoad } from '../hooks/useAttention'
import PoolTree from '../components/PoolTree'
import PoolDetailPanel from '../components/PoolDetailPanel'
import AttentionCard from '../components/AttentionCard'
import { EmptyState, MetricCard } from '../components/primitives'
import type { PoolWithStats } from '../api/types'
import { summarizeUtilization } from '../utils/attention'
import { formatHostCount, formatTimeAgo, getHostCount, getActionBadgeClass } from '../utils/format'

const UTILIZATION_LINK = { label: 'Open pools', to: '/pools' }

export default function DashboardPage() {
  const navigate = useNavigate()
  const { hasPermission } = useAuth()
  const canReadAudit = hasPermission('audit:read')
  const { hierarchy, loading: poolsLoading, error: poolsError, fetchHierarchy } = usePools()
  const { accounts, loading: accountsLoading, fetchAccounts } = useAccounts()
  const { events, fetchEvents } = useAudit()
  const attention = useAttention()
  const [selectedPool, setSelectedPool] = useState<PoolWithStats | null>(null)

  useEffect(() => {
    fetchHierarchy()
    fetchAccounts()
  }, [fetchHierarchy, fetchAccounts])

  useEffect(() => {
    if (canReadAudit) fetchEvents(0, 5)
  }, [canReadAudit, fetchEvents])

  const inventory = useMemo(() => {
    let totalPools = 0
    let totalIPs = 0
    let usedIPs = 0
    const walk = (nodes: PoolWithStats[]) => {
      for (const n of nodes) {
        totalPools++
        totalIPs += n.stats?.total_ips ?? getHostCount(n.cidr)
        usedIPs += n.stats?.used_ips ?? 0
        if (n.children) walk(n.children)
      }
    }
    walk(hierarchy)
    return { totalPools, totalIPs, usedIPs }
  }, [hierarchy])

  const utilization: AttentionLoad = useMemo(() => {
    if (poolsLoading) return { state: 'loading' }
    if (poolsError) return { state: 'error', message: poolsError }
    return { state: 'ready', summary: summarizeUtilization(hierarchy) }
  }, [hierarchy, poolsLoading, poolsError])

  const providerSummary = useMemo(() => {
    const counts: Record<string, number> = {}
    for (const a of accounts) {
      const p = a.provider || 'other'
      counts[p] = (counts[p] || 0) + 1
    }
    return Object.entries(counts).map(([p, c]) => `${p}: ${c}`).join(', ')
  }, [accounts])

  return (
    <div className="flex-1 overflow-auto">
      <div className="p-6 space-y-6">
        <section aria-labelledby="dashboard-attention">
          <h2 id="dashboard-attention" className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-3">
            Needs attention
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
            <AttentionCard title="Pool utilization" icon={Gauge} link={UTILIZATION_LINK} load={utilization} />
            {attention.map(({ source, load }) => (
              <AttentionCard key={source.key} title={source.title} icon={source.icon} link={source.link} load={load} />
            ))}
          </div>
        </section>

        <section aria-labelledby="dashboard-inventory">
          <h2 id="dashboard-inventory" className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-3">
            Inventory
          </h2>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <MetricCard
              icon={<Server className="w-5 h-5 text-blue-600 dark:text-blue-400" />}
              label="Total pools"
              value={String(inventory.totalPools)}
              loading={poolsLoading}
              action={{ label: 'Open pools', to: '/pools' }}
            />
            <MetricCard
              icon={<Cpu className="w-5 h-5 text-purple-600 dark:text-purple-400" />}
              label="Allocated IPs"
              value={formatHostCount(inventory.usedIPs)}
              detail={`of ${formatHostCount(inventory.totalIPs)} total`}
              loading={poolsLoading}
              action={{ label: 'Open blocks', to: '/blocks' }}
            />
            <MetricCard
              icon={<Cloud className="w-5 h-5 text-green-600 dark:text-green-400" />}
              label="Cloud accounts"
              value={String(accounts.length)}
              detail={providerSummary || undefined}
              loading={accountsLoading}
              action={{ label: 'Open accounts', to: '/accounts' }}
            />
          </div>
        </section>

        <div className="flex gap-6">
          <div className="flex-1 min-w-0 space-y-6">
            <div className="bg-white dark:bg-gray-800 rounded-lg border dark:border-gray-700 p-4">
              <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-3">Pool Hierarchy</h2>
              {poolsLoading ? (
                <div className="text-center py-8 text-gray-400 dark:text-gray-500">Loading...</div>
              ) : hierarchy.length === 0 ? (
                <EmptyState
                  title="No pools yet"
                  description="Pools hold the address space everything else is planned in."
                  action={{ label: 'Create your first pool', onClick: () => navigate('/pools') }}
                />
              ) : (
                <PoolTree nodes={hierarchy} selectedId={selectedPool?.id} onSelect={setSelectedPool} />
              )}
            </div>

            {canReadAudit && (
              <div className="bg-white dark:bg-gray-800 rounded-lg border dark:border-gray-700 p-4">
                <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-3">Recent Activity</h3>
                {events.length === 0 ? (
                  <p className="text-sm text-gray-400 dark:text-gray-500">No recent activity</p>
                ) : (
                  <div className="space-y-2">
                    {events.slice(0, 5).map(e => (
                      <div key={e.id} className="flex items-center gap-2 text-sm">
                        <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium ${getActionBadgeClass(e.action)}`}>
                          {e.action}
                        </span>
                        <span className="text-gray-600 dark:text-gray-300 truncate">{e.resource_type} {e.resource_name || e.resource_id}</span>
                        <span className="ml-auto text-xs text-gray-400 dark:text-gray-500 flex-shrink-0">{formatTimeAgo(e.timestamp)}</span>
                      </div>
                    ))}
                    <Link
                      to="/audit"
                      className="inline-block text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
                    >
                      View full audit log
                    </Link>
                  </div>
                )}
              </div>
            )}
          </div>

          {selectedPool && <PoolDetailPanel pool={selectedPool} onClose={() => setSelectedPool(null)} />}
        </div>
      </div>
    </div>
  )
}
