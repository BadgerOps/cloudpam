import { X } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import type { PoolWithStats } from '../api/types'
import { formatHostCount, getUtilizationColor } from '../utils/format'
import StatusBadge from './StatusBadge'

interface PoolDetailPanelProps {
  pool: PoolWithStats
  onClose: () => void
}

export default function PoolDetailPanel({ pool, onClose }: PoolDetailPanelProps) {
  const navigate = useNavigate()
  const stats = pool.stats

  return (
    <div className="border-l dark:border-gray-700 bg-white dark:bg-gray-800 w-80 flex-shrink-0 overflow-y-auto">
      <div className="p-4 border-b dark:border-gray-700 flex items-center justify-between">
        <h3 className="font-semibold text-gray-900 dark:text-gray-100 truncate">{pool.name}</h3>
        <button onClick={onClose} className="p-1 hover:bg-gray-100 dark:hover:bg-gray-700 rounded">
          <X className="w-4 h-4 text-gray-500 dark:text-gray-400" />
        </button>
      </div>

      <div className="p-4 space-y-4">
        {/* Pool info */}
        <div className="grid grid-cols-2 gap-3 text-sm">
          <div>
            <span className="text-gray-500 dark:text-gray-400 block text-xs">CIDR</span>
            <span className="font-mono">{pool.cidr}</span>
          </div>
          <div>
            <span className="text-gray-500 dark:text-gray-400 block text-xs">Type</span>
            <StatusBadge label={pool.type} variant="type" />
          </div>
          <div>
            <span className="text-gray-500 dark:text-gray-400 block text-xs">Status</span>
            <StatusBadge label={pool.status} />
          </div>
          <div>
            <span className="text-gray-500 dark:text-gray-400 block text-xs">Source</span>
            <span>{pool.source}</span>
          </div>
        </div>

        {pool.description && (
          <div className="text-sm">
            <span className="text-gray-500 dark:text-gray-400 block text-xs">Description</span>
            <p className="text-gray-700 dark:text-gray-300">{pool.description}</p>
          </div>
        )}

        {/* Utilization */}
        {stats && (
          <div>
            <div className="flex justify-between text-xs text-gray-500 dark:text-gray-400 mb-1">
              <span>Utilization</span>
              <span>{stats.utilization.toFixed(1)}%</span>
            </div>
            <div className="w-full h-2 bg-gray-200 dark:bg-gray-600 rounded-full overflow-hidden">
              <div
                className={`h-full rounded-full ${getUtilizationColor(stats.utilization)}`}
                style={{ width: `${Math.min(stats.utilization, 100)}%` }}
              />
            </div>
            <div className="grid grid-cols-3 gap-2 mt-3 text-center">
              <div>
                <div className="text-lg font-semibold text-gray-900 dark:text-gray-100">{formatHostCount(stats.total_ips)}</div>
                <div className="text-xs text-gray-500 dark:text-gray-400">Total IPs</div>
              </div>
              <div>
                <div className="text-lg font-semibold text-blue-600 dark:text-blue-400">{formatHostCount(stats.used_ips)}</div>
                <div className="text-xs text-gray-500 dark:text-gray-400">Used</div>
              </div>
              <div>
                <div className="text-lg font-semibold text-green-600 dark:text-green-400">{formatHostCount(stats.available_ips)}</div>
                <div className="text-xs text-gray-500 dark:text-gray-400">Available</div>
              </div>
            </div>
            <div className="text-xs text-gray-400 dark:text-gray-500 mt-2 text-center">
              {stats.direct_children} direct children, {stats.child_count} total
            </div>
          </div>
        )}

        {/* Actions */}
        <div className="flex gap-2">
          <button
            onClick={() => navigate('/pools')}
            className="flex-1 px-3 py-2 bg-blue-600 text-white text-sm rounded hover:bg-blue-700"
          >
            Manage Pool
          </button>
          <button
            onClick={() => navigate('/audit')}
            className="flex-1 px-3 py-2 border dark:border-gray-600 text-sm dark:text-gray-300 rounded hover:bg-gray-50 dark:hover:bg-gray-700"
          >
            View Audit
          </button>
        </div>
      </div>
    </div>
  )
}
