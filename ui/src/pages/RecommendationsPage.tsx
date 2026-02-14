import { useEffect, useState, useCallback } from 'react'
import {
  Lightbulb,
  RefreshCw,
  Check,
  X,
  Sparkles,
} from 'lucide-react'
import { useRecommendations } from '../hooks/useRecommendations'
import { usePools } from '../hooks/usePools'
import { useToast } from '../hooks/useToast'
import { formatTimeAgo } from '../utils/format'
import type { Recommendation, Pool } from '../api/types'

export default function RecommendationsPage() {
  const [typeFilter, setTypeFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [poolFilter, setPoolFilter] = useState<number | undefined>()
  const [showGenerateModal, setShowGenerateModal] = useState(false)
  const [showApplyModal, setShowApplyModal] = useState<Recommendation | null>(null)
  const [showDismissModal, setShowDismissModal] = useState<Recommendation | null>(null)

  const { data, loading, error, fetch, generate, apply, dismiss } = useRecommendations()
  const { pools, fetchPools } = usePools()
  const { showToast } = useToast()

  useEffect(() => {
    fetchPools()
  }, [fetchPools])

  const loadRecs = useCallback(() => {
    fetch({
      type: typeFilter || undefined,
      status: statusFilter || undefined,
      pool_id: poolFilter,
    })
  }, [fetch, typeFilter, statusFilter, poolFilter])

  useEffect(() => {
    loadRecs()
  }, [loadRecs])

  return (
    <div className="flex-1 overflow-auto p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <Lightbulb className="w-5 h-5 text-yellow-500" />
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">
            Recommendations
          </h1>
        </div>
        <div className="flex gap-2">
          <button
            onClick={loadRecs}
            className="inline-flex items-center gap-1.5 px-3 py-2 border dark:border-gray-700 text-sm rounded hover:bg-gray-50 dark:hover:bg-gray-700/50"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            onClick={() => setShowGenerateModal(true)}
            className="inline-flex items-center gap-1.5 px-3 py-2 bg-blue-600 text-white text-sm rounded hover:bg-blue-700"
          >
            <Sparkles className="w-4 h-4" />
            Generate
          </button>
        </div>
      </div>

      {/* Filters */}
      <div className="flex gap-3 mb-4">
        <select
          value={poolFilter ?? ''}
          onChange={(e) =>
            setPoolFilter(e.target.value ? Number(e.target.value) : undefined)
          }
          className="px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
        >
          <option value="">All Pools</option>
          {pools.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name} ({p.cidr})
            </option>
          ))}
        </select>
        <select
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          className="px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
        >
          <option value="">All Types</option>
          <option value="allocation">Allocation</option>
          <option value="compliance">Compliance</option>
        </select>
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
        >
          <option value="">All Statuses</option>
          <option value="pending">Pending</option>
          <option value="applied">Applied</option>
          <option value="dismissed">Dismissed</option>
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
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Priority</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Type</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Title</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Pool</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Score</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Status</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Created</th>
              <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-300">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100 dark:divide-gray-700">
            {data?.items?.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center text-gray-500 dark:text-gray-400">
                  No recommendations found. Click Generate to analyze your pools.
                </td>
              </tr>
            )}
            {data?.items?.map((rec) => (
              <RecRow
                key={rec.id}
                rec={rec}
                pools={pools}
                onApply={() => setShowApplyModal(rec)}
                onDismiss={() => setShowDismissModal(rec)}
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

      {/* Generate Modal */}
      {showGenerateModal && (
        <GenerateModal
          pools={pools}
          onClose={() => setShowGenerateModal(false)}
          onGenerate={async (poolIds, includeChildren) => {
            try {
              const resp = await generate(poolIds, includeChildren)
              showToast(`Generated ${resp.total} recommendations`, 'success')
              setShowGenerateModal(false)
              loadRecs()
            } catch (err) {
              showToast(
                err instanceof Error ? err.message : 'Generation failed',
                'error',
              )
            }
          }}
        />
      )}

      {/* Apply Modal */}
      {showApplyModal && (
        <ApplyModal
          rec={showApplyModal}
          onClose={() => setShowApplyModal(null)}
          onApply={async (name, accountId) => {
            try {
              await apply(showApplyModal.id, name, accountId)
              showToast('Recommendation applied', 'success')
              setShowApplyModal(null)
              loadRecs()
            } catch (err) {
              showToast(
                err instanceof Error ? err.message : 'Apply failed',
                'error',
              )
            }
          }}
        />
      )}

      {/* Dismiss Modal */}
      {showDismissModal && (
        <DismissModal
          rec={showDismissModal}
          onClose={() => setShowDismissModal(null)}
          onDismiss={async (reason) => {
            try {
              await dismiss(showDismissModal.id, reason)
              showToast('Recommendation dismissed', 'success')
              setShowDismissModal(null)
              loadRecs()
            } catch (err) {
              showToast(
                err instanceof Error ? err.message : 'Dismiss failed',
                'error',
              )
            }
          }}
        />
      )}
    </div>
  )
}

function RecRow({
  rec,
  pools,
  onApply,
  onDismiss,
}: {
  rec: Recommendation
  pools: Pool[]
  onApply: () => void
  onDismiss: () => void
}) {
  const pool = pools.find((p) => p.id === rec.pool_id)

  return (
    <tr className="hover:bg-gray-50 dark:hover:bg-gray-700/30">
      <td className="px-4 py-3">
        <PriorityBadge priority={rec.priority} />
      </td>
      <td className="px-4 py-3">
        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300">
          {rec.type}
        </span>
      </td>
      <td className="px-4 py-3">
        <div className="text-gray-900 dark:text-gray-100">{rec.title}</div>
        {rec.description && (
          <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5 truncate max-w-xs">
            {rec.description}
          </div>
        )}
      </td>
      <td className="px-4 py-3 text-gray-700 dark:text-gray-300">
        {pool ? pool.name : `Pool #${rec.pool_id}`}
      </td>
      <td className="px-4 py-3">
        <ScoreBadge score={rec.score} />
      </td>
      <td className="px-4 py-3">
        <StatusBadge status={rec.status} />
      </td>
      <td className="px-4 py-3 text-gray-500 dark:text-gray-400 text-xs">
        {formatTimeAgo(rec.created_at)}
      </td>
      <td className="px-4 py-3">
        {rec.status === 'pending' && (
          <div className="flex gap-1">
            <button
              onClick={onApply}
              className="p-1.5 text-green-600 hover:bg-green-50 dark:hover:bg-green-900/30 rounded"
              title="Apply"
            >
              <Check className="w-4 h-4" />
            </button>
            <button
              onClick={onDismiss}
              className="p-1.5 text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 rounded"
              title="Dismiss"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        )}
      </td>
    </tr>
  )
}

function PriorityBadge({ priority }: { priority: string }) {
  const cls =
    priority === 'high'
      ? 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
      : priority === 'medium'
        ? 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300'
        : 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'

  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${cls}`}>
      {priority}
    </span>
  )
}

function StatusBadge({ status }: { status: string }) {
  const cls =
    status === 'applied'
      ? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
      : status === 'dismissed'
        ? 'bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-400'
        : 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'

  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${cls}`}>
      {status}
    </span>
  )
}

function ScoreBadge({ score }: { score: number }) {
  const cls =
    score >= 70
      ? 'text-green-600 dark:text-green-400'
      : score >= 40
        ? 'text-yellow-600 dark:text-yellow-400'
        : 'text-gray-500 dark:text-gray-400'

  return <span className={`font-medium text-xs ${cls}`}>{score}</span>
}

function GenerateModal({
  pools,
  onClose,
  onGenerate,
}: {
  pools: Pool[]
  onClose: () => void
  onGenerate: (poolIds: number[], includeChildren: boolean) => Promise<void>
}) {
  const [selectedPools, setSelectedPools] = useState<number[]>([])
  const [includeChildren, setIncludeChildren] = useState(true)
  const [submitting, setSubmitting] = useState(false)

  const parentPools = pools.filter(
    (p) => p.type !== 'subnet' || pools.some((c) => c.parent_id === p.id),
  )

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
          Generate Recommendations
        </h2>
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              Select Pools
            </label>
            <select
              multiple
              size={6}
              value={selectedPools.map(String)}
              onChange={(e) =>
                setSelectedPools(
                  Array.from(e.target.selectedOptions, (o) => Number(o.value)),
                )
              }
              className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
            >
              {parentPools.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.cidr})
                </option>
              ))}
            </select>
            <p className="text-xs text-gray-500 mt-1">
              Hold Ctrl/Cmd to select multiple pools
            </p>
          </div>
          <label className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input
              type="checkbox"
              checked={includeChildren}
              onChange={(e) => setIncludeChildren(e.target.checked)}
              className="rounded"
            />
            Include child pools
          </label>
        </div>
        <div className="flex justify-end gap-2 mt-6">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm border dark:border-gray-600 rounded hover:bg-gray-50 dark:hover:bg-gray-700"
          >
            Cancel
          </button>
          <button
            disabled={selectedPools.length === 0 || submitting}
            onClick={async () => {
              setSubmitting(true)
              try {
                await onGenerate(selectedPools, includeChildren)
              } finally {
                setSubmitting(false)
              }
            }}
            className="px-4 py-2 text-sm bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
          >
            {submitting ? 'Generating...' : 'Generate'}
          </button>
        </div>
      </div>
    </div>
  )
}

function ApplyModal({
  rec,
  onClose,
  onApply,
}: {
  rec: Recommendation
  onClose: () => void
  onApply: (name?: string, accountId?: number) => Promise<void>
}) {
  const [name, setName] = useState(
    rec.type === 'allocation' ? `Allocation ${rec.suggested_cidr}` : '',
  )
  const [submitting, setSubmitting] = useState(false)

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
          Apply Recommendation
        </h2>
        <p className="text-sm text-gray-600 dark:text-gray-400 mb-4">
          {rec.title}
        </p>
        {rec.type === 'allocation' && rec.suggested_cidr && (
          <div className="space-y-3 mb-4">
            <div className="bg-blue-50 dark:bg-blue-900/30 rounded p-3 text-sm">
              <span className="font-medium">CIDR:</span> {rec.suggested_cidr}
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Pool Name
              </label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
              />
            </div>
          </div>
        )}
        {rec.type === 'compliance' && (
          <div className="bg-yellow-50 dark:bg-yellow-900/30 rounded p-3 text-sm mb-4">
            Marking this as applied acknowledges the compliance issue. The underlying fix should be applied manually.
          </div>
        )}
        <div className="flex justify-end gap-2">
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
                await onApply(name || undefined)
              } finally {
                setSubmitting(false)
              }
            }}
            className="px-4 py-2 text-sm bg-green-600 text-white rounded hover:bg-green-700 disabled:opacity-50"
          >
            {submitting ? 'Applying...' : 'Apply'}
          </button>
        </div>
      </div>
    </div>
  )
}

function DismissModal({
  rec,
  onClose,
  onDismiss,
}: {
  rec: Recommendation
  onClose: () => void
  onDismiss: (reason?: string) => Promise<void>
}) {
  const [reason, setReason] = useState('')
  const [submitting, setSubmitting] = useState(false)

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
          Dismiss Recommendation
        </h2>
        <p className="text-sm text-gray-600 dark:text-gray-400 mb-4">
          {rec.title}
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
            placeholder="Why is this recommendation not applicable?"
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
                await onDismiss(reason || undefined)
              } finally {
                setSubmitting(false)
              }
            }}
            className="px-4 py-2 text-sm bg-gray-600 text-white rounded hover:bg-gray-700 disabled:opacity-50"
          >
            {submitting ? 'Dismissing...' : 'Dismiss'}
          </button>
        </div>
      </div>
    </div>
  )
}
