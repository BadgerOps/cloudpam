import { useEffect, useState, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { Search, LayoutGrid } from 'lucide-react'
import { useBlocks } from '../hooks/useBlocks'
import { useAccounts } from '../hooks/useAccounts'
import StatusBadge from '../components/StatusBadge'
import { formatHostCount, getHostCount } from '../utils/format'

export default function BlocksPage() {
  const navigate = useNavigate()
  const { blocks, loading, error, fetchBlocks } = useBlocks()
  const { accounts, fetchAccounts } = useAccounts()
  const [search, setSearch] = useState('')
  const [accountFilter, setAccountFilter] = useState<string>('')

  useEffect(() => {
    fetchBlocks()
    fetchAccounts()
  }, [fetchBlocks, fetchAccounts])

  const filtered = useMemo(() => {
    return blocks.filter(b => {
      if (accountFilter && String(b.account_id) !== accountFilter) return false
      if (!search) return true
      const q = search.toLowerCase()
      return b.name.toLowerCase().includes(q) ||
        b.cidr.includes(q) ||
        (b.account_name || '').toLowerCase().includes(q) ||
        (b.parent_name || '').toLowerCase().includes(q)
    })
  }, [blocks, search, accountFilter])

  // Summary stats
  const summaryStats = useMemo(() => {
    const totalBlocks = filtered.length
    let totalIPs = 0
    const uniqueAccounts = new Set<number>()
    for (const b of filtered) {
      totalIPs += getHostCount(b.cidr)
      if (b.account_id) uniqueAccounts.add(b.account_id)
    }
    return { totalBlocks, totalIPs, uniqueAccounts: uniqueAccounts.size }
  }, [filtered])

  return (
    <div className="flex-1 overflow-auto p-6">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Allocated Blocks</h1>
      </div>

      {/* Filters */}
      <div className="flex gap-3 mb-4">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 dark:text-gray-500" />
          <input
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder="Search blocks by name, CIDR, account, or parent..."
            className="w-full pl-9 pr-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
          />
        </div>
        <select
          value={accountFilter}
          onChange={e => setAccountFilter(e.target.value)}
          className="px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
        >
          <option value="">All Accounts</option>
          {accounts.map(a => (
            <option key={a.id} value={a.id}>{a.name}</option>
          ))}
        </select>
      </div>

      {/* Summary */}
      <div className="grid grid-cols-3 gap-4 mb-4">
        <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg p-3 text-center">
          <div className="text-lg font-bold text-gray-900 dark:text-gray-100">{summaryStats.totalBlocks}</div>
          <div className="text-xs text-gray-500 dark:text-gray-400">Total Blocks</div>
        </div>
        <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg p-3 text-center">
          <div className="text-lg font-bold text-gray-900 dark:text-gray-100">{formatHostCount(summaryStats.totalIPs)}</div>
          <div className="text-xs text-gray-500 dark:text-gray-400">Total IPs</div>
        </div>
        <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg p-3 text-center">
          <div className="text-lg font-bold text-gray-900 dark:text-gray-100">{summaryStats.uniqueAccounts}</div>
          <div className="text-xs text-gray-500 dark:text-gray-400">Unique Accounts</div>
        </div>
      </div>

      {error && (
        <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm p-3 rounded mb-4">{error}</div>
      )}

      {loading ? (
        <div className="text-center py-12 text-gray-400 dark:text-gray-500">Loading blocks...</div>
      ) : filtered.length === 0 ? (
        <div className="text-center py-12">
          <LayoutGrid className="w-12 h-12 mx-auto mb-2 text-gray-300 dark:text-gray-600" />
          <p className="text-gray-500 dark:text-gray-400">No blocks found</p>
          <button
            onClick={() => navigate('/pools')}
            className="mt-2 text-sm text-blue-600 hover:text-blue-800"
          >
            Go to Pools to create allocations
          </button>
        </div>
      ) : (
        <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg overflow-hidden">
          <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
            <thead className="bg-gray-50 dark:bg-gray-800">
              <tr>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Name</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">CIDR</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Parent</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Account</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Tier</th>
                <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">IPs</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100 dark:divide-gray-700">
              {filtered.map(b => {
                const hostCount = getHostCount(b.cidr)
                return (
                  <tr key={b.id} className="hover:bg-gray-50 dark:hover:bg-gray-700/50">
                    <td className="px-4 py-2 text-sm font-medium text-gray-900 dark:text-gray-100">{b.name}</td>
                    <td className="px-4 py-2 text-sm font-mono text-gray-600 dark:text-gray-300">{b.cidr}</td>
                    <td className="px-4 py-2 text-sm text-gray-500 dark:text-gray-400">{b.parent_name || '-'}</td>
                    <td className="px-4 py-2 text-sm text-gray-500 dark:text-gray-400">{b.account_name || '-'}</td>
                    <td className="px-4 py-2">
                      {b.account_tier && <StatusBadge label={b.account_tier} variant="tier" />}
                    </td>
                    <td className="px-4 py-2 text-sm text-gray-500 dark:text-gray-400">{formatHostCount(hostCount)}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
