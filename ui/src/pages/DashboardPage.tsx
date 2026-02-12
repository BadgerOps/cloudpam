import { useEffect, useState, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { Server, Cpu, Cloud, AlertTriangle } from 'lucide-react'
import { usePools } from '../hooks/usePools'
import { useAccounts } from '../hooks/useAccounts'
import { useAudit } from '../hooks/useAudit'
import PoolTree from '../components/PoolTree'
import PoolDetailPanel from '../components/PoolDetailPanel'
import StatusBadge from '../components/StatusBadge'
import type { PoolWithStats } from '../api/types'
import { formatHostCount, formatTimeAgo, getHostCount, getActionBadgeClass } from '../utils/format'

export default function DashboardPage() {
  const navigate = useNavigate()
  const { hierarchy, loading: poolsLoading, fetchHierarchy } = usePools()
  const { accounts, loading: accountsLoading, fetchAccounts } = useAccounts()
  const { events, fetchEvents } = useAudit()
  const [selectedPool, setSelectedPool] = useState<PoolWithStats | null>(null)

  useEffect(() => {
    fetchHierarchy()
    fetchAccounts()
    fetchEvents(0, 5)
  }, [fetchHierarchy, fetchAccounts, fetchEvents])

  // Compute stats from hierarchy
  const stats = useMemo(() => {
    let totalPools = 0
    let totalIPs = 0
    let usedIPs = 0
    const alerts: { pool: PoolWithStats; severity: 'error' | 'warning' }[] = []

    function walk(nodes: PoolWithStats[]) {
      for (const n of nodes) {
        totalPools++
        totalIPs += n.stats?.total_ips ?? getHostCount(n.cidr)
        usedIPs += n.stats?.used_ips ?? 0
        const util = n.stats?.utilization ?? 0
        if (util > 80) alerts.push({ pool: n, severity: 'error' })
        else if (util > 60) alerts.push({ pool: n, severity: 'warning' })
        if (n.children) walk(n.children)
      }
    }
    walk(hierarchy)

    return { totalPools, totalIPs, usedIPs, alerts }
  }, [hierarchy])

  // Provider breakdown
  const providerCounts = useMemo(() => {
    const counts: Record<string, number> = {}
    for (const a of accounts) {
      const p = a.provider || 'other'
      counts[p] = (counts[p] || 0) + 1
    }
    return counts
  }, [accounts])

  const loading = poolsLoading || accountsLoading

  return (
    <div className="flex-1 overflow-auto">
      <div className="p-6">
        {/* Stats cards */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
          <StatsCard
            icon={<Server className="w-5 h-5 text-blue-600 dark:text-blue-400" />}
            label="Total Pools"
            value={loading ? '...' : String(stats.totalPools)}
          />
          <StatsCard
            icon={<Cpu className="w-5 h-5 text-purple-600 dark:text-purple-400" />}
            label="Allocated IPs"
            value={loading ? '...' : formatHostCount(stats.usedIPs)}
            sub={loading ? undefined : `of ${formatHostCount(stats.totalIPs)} total`}
          />
          <StatsCard
            icon={<Cloud className="w-5 h-5 text-green-600 dark:text-green-400" />}
            label="Cloud Accounts"
            value={loading ? '...' : String(accounts.length)}
            sub={Object.entries(providerCounts).map(([p, c]) => `${p}: ${c}`).join(', ') || undefined}
          />
          <StatsCard
            icon={<AlertTriangle className="w-5 h-5 text-amber-600 dark:text-amber-400" />}
            label="Active Alerts"
            value={loading ? '...' : String(stats.alerts.length)}
            sub={stats.alerts.length > 0
              ? `${stats.alerts.filter(a => a.severity === 'error').length} critical, ${stats.alerts.filter(a => a.severity === 'warning').length} warning`
              : 'All clear'}
          />
        </div>

        {/* Main content: tree + alerts */}
        <div className="flex gap-6">
          <div className="flex-1 min-w-0">
            {/* Pool hierarchy */}
            <div className="bg-white dark:bg-gray-800 rounded-lg border dark:border-gray-700 p-4 mb-6">
              <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-3">Pool Hierarchy</h2>
              {loading ? (
                <div className="text-center py-8 text-gray-400 dark:text-gray-500">Loading...</div>
              ) : hierarchy.length === 0 ? (
                <div className="text-center py-8">
                  <Server className="w-12 h-12 mx-auto mb-2 text-gray-300 dark:text-gray-600" />
                  <p className="text-gray-500 dark:text-gray-400">No pools yet</p>
                  <button
                    onClick={() => navigate('/pools')}
                    className="mt-2 text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
                  >
                    Create your first pool
                  </button>
                </div>
              ) : (
                <PoolTree
                  nodes={hierarchy}
                  selectedId={selectedPool?.id}
                  onSelect={setSelectedPool}
                />
              )}
            </div>

            {/* Bottom row: accounts + activity */}
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
              {/* Top accounts */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border dark:border-gray-700 p-4">
                <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-3">Cloud Accounts</h3>
                {accounts.length === 0 ? (
                  <p className="text-sm text-gray-400 dark:text-gray-500">No accounts configured</p>
                ) : (
                  <div className="space-y-2">
                    {accounts.slice(0, 5).map(a => (
                      <div key={a.id} className="flex items-center justify-between text-sm">
                        <div className="flex items-center gap-2">
                          <span className="text-gray-900 dark:text-gray-100">{a.name}</span>
                          <StatusBadge label={a.provider || 'other'} variant="provider" />
                        </div>
                        {a.tier && <StatusBadge label={a.tier} variant="tier" />}
                      </div>
                    ))}
                    {accounts.length > 5 && (
                      <button
                        onClick={() => navigate('/accounts')}
                        className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
                      >
                        View all {accounts.length} accounts
                      </button>
                    )}
                  </div>
                )}
              </div>

              {/* Recent activity */}
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
                    <button
                      onClick={() => navigate('/audit')}
                      className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
                    >
                      View full audit log
                    </button>
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* Alerts panel */}
          {stats.alerts.length > 0 && !selectedPool && (
            <div className="w-72 flex-shrink-0">
              <div className="bg-white dark:bg-gray-800 rounded-lg border dark:border-gray-700 p-4">
                <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-3">Utilization Alerts</h3>
                <div className="space-y-2">
                  {stats.alerts.map(({ pool: p, severity }) => (
                    <div
                      key={p.id}
                      className={`p-2 rounded text-sm cursor-pointer ${
                        severity === 'error' ? 'bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800' : 'bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-800'
                      }`}
                      onClick={() => setSelectedPool(p)}
                    >
                      <div className="font-medium text-gray-900 dark:text-gray-100">{p.name}</div>
                      <div className="text-xs text-gray-500 dark:text-gray-400">
                        {p.cidr} &mdash; {(p.stats?.utilization ?? 0).toFixed(1)}% used
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          )}

          {/* Pool detail panel */}
          {selectedPool && (
            <PoolDetailPanel pool={selectedPool} onClose={() => setSelectedPool(null)} />
          )}
        </div>
      </div>
    </div>
  )
}

function StatsCard({ icon, label, value, sub }: {
  icon: React.ReactNode
  label: string
  value: string
  sub?: string
}) {
  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg border dark:border-gray-700 p-4">
      <div className="flex items-center gap-3">
        <div className="p-2 bg-gray-50 dark:bg-gray-800 rounded-lg">{icon}</div>
        <div>
          <div className="text-2xl font-bold text-gray-900 dark:text-gray-100">{value}</div>
          <div className="text-sm text-gray-500 dark:text-gray-400">{label}</div>
          {sub && <div className="text-xs text-gray-400 dark:text-gray-500 mt-0.5">{sub}</div>}
        </div>
      </div>
    </div>
  )
}
