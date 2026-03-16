import { useEffect, useState, useCallback } from 'react'
import {
  GitCompareArrows,
  RefreshCw,
  Check,
  EyeOff,
  AlertTriangle,
  AlertCircle,
  Info,
  Play,
} from 'lucide-react'
import { useDrift } from '../hooks/useDrift'
import { useToast } from '../hooks/useToast'
import { formatTimeAgo } from '../utils/format'
import type { DriftItem } from '../api/types'

export default function DriftPage() {
  const [typeFilter, setTypeFilter] = useState('')
  const [severityFilter, setSeverityFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('open')
  const [detecting, setDetecting] = useState(false)
  const [showIgnoreModal, setShowIgnoreModal] = useState<DriftItem | null>(null)

  const { data, loading, error, fetch, detect, resolve, ignore } = useDrift()
  const { showToast } = useToast()

  const loadDrifts = useCallback(() => {
    fetch({
      type: typeFilter || undefined,
      severity: severityFilter || undefined,
      status: statusFilter || undefined,
    })
  }, [fetch, typeFilter, severityFilter, statusFilter])

  useEffect(() => {
    loadDrifts()
  }, [loadDrifts])

  const handleDetect = async () => {
    setDetecting(true)
    try {
      const resp = await detect()
      showToast(`Detection complete: ${resp.total} drift items found`, 'success')
      loadDrifts()
    } catch (err) {
      showToast(
        err instanceof Error ? err.message : 'Detection failed',
        'error',
      )
    } finally {
      setDetecting(false)
    }
  }

  const handleResolve = async (id: string) => {
    try {
      await resolve(id)
      showToast('Drift item resolved', 'success')
      loadDrifts()
    } catch (err) {
      showToast(
        err instanceof Error ? err.message : 'Resolve failed',
        'error',
      )
    }
  }

  const summary = data?.summary

  return (
    <div className="flex-1 overflow-auto p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <GitCompareArrows className="w-5 h-5 text-orange-500" />
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">
            Drift Detection
          </h1>
        </div>
        <div className="flex gap-2">
          <button
            onClick={loadDrifts}
            className="inline-flex items-center gap-1.5 px-3 py-2 border dark:border-gray-700 text-sm rounded hover:bg-gray-50 dark:hover:bg-gray-700/50"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            onClick={handleDetect}
            disabled={detecting}
            className="inline-flex items-center gap-1.5 px-3 py-2 bg-blue-600 text-white text-sm rounded hover:bg-blue-700 disabled:opacity-50"
          >
            <Play className={`w-4 h-4 ${detecting ? 'animate-pulse' : ''}`} />
            {detecting ? 'Detecting...' : 'Run Detection'}
          </button>
        </div>
      </div>

      {/* Summary cards */}
      {summary && summary.total_drifts > 0 && (
        <div className="grid grid-cols-3 gap-4 mb-4">
          <SummaryCard
            label="Critical"
            count={summary.by_severity?.critical || 0}
            icon={<AlertCircle className="w-5 h-5 text-red-500" />}
            className="border-red-200 dark:border-red-800"
          />
          <SummaryCard
            label="Warning"
            count={summary.by_severity?.warning || 0}
            icon={<AlertTriangle className="w-5 h-5 text-yellow-500" />}
            className="border-yellow-200 dark:border-yellow-800"
          />
          <SummaryCard
            label="Info"
            count={summary.by_severity?.info || 0}
            icon={<Info className="w-5 h-5 text-blue-500" />}
            className="border-blue-200 dark:border-blue-800"
          />
        </div>
      )}

      {/* Filters */}
      <div className="flex gap-3 mb-4">
        <select
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          className="px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
        >
          <option value="">All Types</option>
          <option value="unmanaged">Unmanaged</option>
          <option value="cidr_mismatch">CIDR Mismatch</option>
          <option value="orphaned_pool">Orphaned Pool</option>
          <option value="name_mismatch">Name Mismatch</option>
          <option value="account_drift">Account Drift</option>
        </select>
        <select
          value={severityFilter}
          onChange={(e) => setSeverityFilter(e.target.value)}
          className="px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
        >
          <option value="">All Severities</option>
          <option value="critical">Critical</option>
          <option value="warning">Warning</option>
          <option value="info">Info</option>
        </select>
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
        >
          <option value="">All Statuses</option>
          <option value="open">Open</option>
          <option value="resolved">Resolved</option>
          <option value="ignored">Ignored</option>
        </select>
      </div>

      {error && (
        <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 px-4 py-3 rounded mb-4">
          {error}
        </div>
      )}

      {/* Table */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border dark:border-gray-700 overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="bg-gray-50 dark:bg-gray-900/50 text-left">
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Severity</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Type</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Title</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Resource CIDR</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Pool CIDR</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Status</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Detected</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100 dark:divide-gray-700">
            {data?.items?.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center text-gray-500 dark:text-gray-400">
                  No drift items found. Click Run Detection to scan for mismatches.
                </td>
              </tr>
            )}
            {data?.items?.map((item) => (
              <DriftRow
                key={item.id}
                item={item}
                onResolve={() => handleResolve(item.id)}
                onIgnore={() => setShowIgnoreModal(item)}
              />
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {data && data.total > (data.page_size || 50) && (
        <div className="flex items-center justify-between mt-4 text-sm text-gray-600 dark:text-gray-400">
          <span>
            Showing {((data.page - 1) * data.page_size) + 1}-
            {Math.min(data.page * data.page_size, data.total)} of {data.total}
          </span>
        </div>
      )}

      {/* Ignore Modal */}
      {showIgnoreModal && (
        <IgnoreModal
          item={showIgnoreModal}
          onClose={() => setShowIgnoreModal(null)}
          onIgnore={async (reason) => {
            try {
              await ignore(showIgnoreModal.id, reason)
              showToast('Drift item ignored', 'success')
              setShowIgnoreModal(null)
              loadDrifts()
            } catch (err) {
              showToast(
                err instanceof Error ? err.message : 'Ignore failed',
                'error',
              )
            }
          }}
        />
      )}
    </div>
  )
}

function SummaryCard({
  label,
  count,
  icon,
  className,
}: {
  label: string
  count: number
  icon: React.ReactNode
  className?: string
}) {
  return (
    <div className={`bg-white dark:bg-gray-800 rounded-lg border p-4 ${className || 'dark:border-gray-700'}`}>
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm text-gray-500 dark:text-gray-400">{label}</p>
          <p className="text-2xl font-semibold text-gray-900 dark:text-gray-100 mt-1">{count}</p>
        </div>
        {icon}
      </div>
    </div>
  )
}

function DriftRow({
  item,
  onResolve,
  onIgnore,
}: {
  item: DriftItem
  onResolve: () => void
  onIgnore: () => void
}) {
  return (
    <tr className="hover:bg-gray-50 dark:hover:bg-gray-700/30">
      <td className="px-4 py-3">
        <SeverityBadge severity={item.severity} />
      </td>
      <td className="px-4 py-3">
        <TypeBadge type={item.type} />
      </td>
      <td className="px-4 py-3">
        <div className="text-gray-900 dark:text-gray-100">{item.title}</div>
        {item.description && (
          <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5 truncate max-w-md">
            {item.description}
          </div>
        )}
      </td>
      <td className="px-4 py-3 font-mono text-xs text-gray-700 dark:text-gray-300">
        {item.resource_cidr || '-'}
      </td>
      <td className="px-4 py-3 font-mono text-xs text-gray-700 dark:text-gray-300">
        {item.pool_cidr || '-'}
      </td>
      <td className="px-4 py-3">
        <StatusBadge status={item.status} />
      </td>
      <td className="px-4 py-3 text-gray-500 dark:text-gray-400 text-xs">
        {formatTimeAgo(item.detected_at)}
      </td>
      <td className="px-4 py-3">
        {item.status === 'open' && (
          <div className="flex gap-1">
            <button
              onClick={onResolve}
              className="p-1.5 text-green-600 hover:bg-green-50 dark:hover:bg-green-900/30 rounded"
              title="Resolve"
            >
              <Check className="w-4 h-4" />
            </button>
            <button
              onClick={onIgnore}
              className="p-1.5 text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 rounded"
              title="Ignore"
            >
              <EyeOff className="w-4 h-4" />
            </button>
          </div>
        )}
      </td>
    </tr>
  )
}

function SeverityBadge({ severity }: { severity: string }) {
  const cls =
    severity === 'critical'
      ? 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
      : severity === 'warning'
        ? 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300'
        : 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'

  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${cls}`}>
      {severity}
    </span>
  )
}

function TypeBadge({ type }: { type: string }) {
  const label = type.replace(/_/g, ' ')
  return (
    <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300">
      {label}
    </span>
  )
}

function StatusBadge({ status }: { status: string }) {
  const cls =
    status === 'resolved'
      ? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
      : status === 'ignored'
        ? 'bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-400'
        : 'bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300'

  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${cls}`}>
      {status}
    </span>
  )
}

function IgnoreModal({
  item,
  onClose,
  onIgnore,
}: {
  item: DriftItem
  onClose: () => void
  onIgnore: (reason?: string) => Promise<void>
}) {
  const [reason, setReason] = useState('')
  const [submitting, setSubmitting] = useState(false)

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
          Ignore Drift Item
        </h2>
        <p className="text-sm text-gray-600 dark:text-gray-400 mb-4">
          {item.title}
        </p>
        <div>
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
            Reason (optional)
          </label>
          <textarea
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={3}
            className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
            placeholder="Why is this drift acceptable?"
          />
        </div>
        <div className="flex justify-end gap-2 mt-4">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm border dark:border-gray-600 rounded hover:bg-gray-50 dark:hover:bg-gray-700"
          >
            Cancel
          </button>
          <button
            disabled={submitting}
            onClick={async () => {
              setSubmitting(true)
              try {
                await onIgnore(reason || undefined)
              } finally {
                setSubmitting(false)
              }
            }}
            className="px-4 py-2 text-sm bg-gray-600 text-white rounded hover:bg-gray-700 disabled:opacity-50"
          >
            {submitting ? 'Ignoring...' : 'Ignore'}
          </button>
        </div>
      </div>
    </div>
  )
}
