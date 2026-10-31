import { useState } from 'react'
import { ChevronDown, ChevronRight, Loader2, Pencil, X } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import type { FragmentationAnalysis, FragmentationSeverity, GapAnalysis, PoolWithStats } from '../api/types'
import { usePoolAnalysis } from '../hooks/usePoolAnalysis'
import { formatHostCount, getUtilizationColor } from '../utils/format'
import StatusBadge from './StatusBadge'

const MAX_RANGES = 5
const MAX_ISSUES = 3
const MAX_RECOMMENDATIONS = 3

function fragmentationBadge(score: number): { label: string; className: string } {
  if (score <= 20) return { label: 'Healthy', className: 'bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300' }
  if (score <= 50) return { label: 'Attention', className: 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300' }
  return { label: 'Severe', className: 'bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300' }
}

const severityClass: Record<FragmentationSeverity, string> = {
  error: 'text-red-600 dark:text-red-400',
  warning: 'text-amber-600 dark:text-amber-400',
  info: 'text-blue-600 dark:text-blue-400',
}

function MoreCount({ total, shown }: { total: number; shown: number }) {
  if (total <= shown) return null
  return <li className="text-xs text-gray-400 dark:text-gray-500">+{total - shown} more</li>
}

function AnalysisLoading({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
      <Loader2 className="h-3.5 w-3.5 animate-spin" />
      {label}
    </div>
  )
}

function AnalysisError({ message }: { message: string }) {
  return <p role="alert" className="text-xs text-red-600 dark:text-red-400">{message}</p>
}

function GapSummary({ gaps }: { gaps: GapAnalysis }) {
  const available = gaps.available_blocks ?? []
  return (
    <div className="grid grid-cols-3 gap-2 text-sm">
      <div>
        <span className="text-gray-500 dark:text-gray-400 block text-xs">Free</span>
        <span className="text-gray-900 dark:text-gray-100">{formatHostCount(gaps.free_addresses)}</span>
      </div>
      <div>
        <span className="text-gray-500 dark:text-gray-400 block text-xs">Utilization</span>
        <span className="text-gray-900 dark:text-gray-100">{gaps.utilization_percent.toFixed(1)}%</span>
      </div>
      <div>
        <span className="text-gray-500 dark:text-gray-400 block text-xs">Free blocks</span>
        <span data-testid="available-block-count" className="text-gray-900 dark:text-gray-100">{available.length}</span>
      </div>
    </div>
  )
}

function AvailableRanges({ gaps }: { gaps: GapAnalysis }) {
  const available = gaps.available_blocks ?? []
  if (available.length === 0) return null
  return (
    <div>
      <span className="text-gray-500 dark:text-gray-400 block text-xs mb-1">Available ranges</span>
      <ul className="space-y-0.5">
        {available.slice(0, MAX_RANGES).map(block => (
          <li key={block.cidr} className="flex justify-between text-xs">
            <span data-testid="available-range" className="font-mono text-gray-700 dark:text-gray-300">{block.cidr}</span>
            <span className="text-gray-400 dark:text-gray-500">{formatHostCount(block.address_count)}</span>
          </li>
        ))}
        <MoreCount total={available.length} shown={MAX_RANGES} />
      </ul>
    </div>
  )
}

function FragmentationDetails({ fragmentation }: { fragmentation: FragmentationAnalysis }) {
  const badge = fragmentationBadge(fragmentation.score)
  const issues = fragmentation.issues ?? []
  const recommendations = fragmentation.recommendations ?? []
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <div>
          <span className="text-gray-500 dark:text-gray-400 block text-xs">Fragmentation</span>
          <span className="text-sm text-gray-900 dark:text-gray-100">{fragmentation.score} / 100</span>
        </div>
        <span data-testid="fragmentation-badge" className={`px-2 py-0.5 rounded text-xs font-medium ${badge.className}`}>
          {badge.label}
        </span>
      </div>
      {issues.length > 0 && (
        <ul className="space-y-1">
          {issues.slice(0, MAX_ISSUES).map((issue, i) => (
            <li key={`${issue.cidr}-${issue.type}-${i}`} className="text-xs text-gray-700 dark:text-gray-300">
              <span className={`font-medium uppercase mr-1 ${severityClass[issue.severity] ?? 'text-gray-500 dark:text-gray-400'}`}>
                {issue.severity}
              </span>
              <span>{issue.description}</span>
            </li>
          ))}
          <MoreCount total={issues.length} shown={MAX_ISSUES} />
        </ul>
      )}
      {recommendations.length > 0 && (
        <div>
          <span className="text-gray-500 dark:text-gray-400 block text-xs mb-1">Recommendations</span>
          <ul className="space-y-1 list-disc list-inside">
            {recommendations.slice(0, MAX_RECOMMENDATIONS).map((rec, i) => (
              <li key={`${i}-${rec}`} className="text-xs text-gray-700 dark:text-gray-300">{rec}</li>
            ))}
            <MoreCount total={recommendations.length} shown={MAX_RECOMMENDATIONS} />
          </ul>
        </div>
      )}
    </div>
  )
}

function AddressAnalysis({ poolId }: { poolId: number }) {
  const [open, setOpen] = useState(true)
  const { gaps, fragmentation, gapsError, fragmentationError, retry } = usePoolAnalysis(poolId)
  const noChildren = gaps !== null && (gaps.allocated_blocks ?? []).length === 0
    && fragmentation !== null && fragmentation.score === 0

  return (
    <div className="border-t dark:border-gray-700 pt-3">
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        aria-expanded={open}
        className="flex w-full items-center gap-1 text-xs font-semibold text-gray-700 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100"
      >
        {open
          ? <ChevronDown className="w-3.5 h-3.5 text-gray-500 dark:text-gray-400" />
          : <ChevronRight className="w-3.5 h-3.5 text-gray-500 dark:text-gray-400" />}
        Address Analysis
      </button>
      {open && (
        <div className="mt-3 space-y-3">
          {gapsError
            ? <AnalysisError message={gapsError} />
            : gaps
              ? <GapSummary gaps={gaps} />
              : <AnalysisLoading label="Analyzing free space..." />}

          {noChildren ? (
            <p className="text-xs text-green-700 dark:text-green-400">
              No child allocations yet; the whole range is available
            </p>
          ) : (
            <>
              {gaps && !gapsError && <AvailableRanges gaps={gaps} />}
              {fragmentationError
                ? <AnalysisError message={fragmentationError} />
                : fragmentation
                  ? <FragmentationDetails fragmentation={fragmentation} />
                  : <AnalysisLoading label="Scoring fragmentation..." />}
            </>
          )}

          {(gapsError || fragmentationError) && (
            <button
              type="button"
              onClick={retry}
              className="px-2 py-1 border dark:border-gray-600 text-xs text-gray-700 dark:text-gray-300 rounded hover:bg-gray-50 dark:hover:bg-gray-700"
            >
              Retry
            </button>
          )}
        </div>
      )}
    </div>
  )
}

interface PoolDetailPanelProps {
  pool: PoolWithStats
  onClose: () => void
  onEdit?: () => void
}

export default function PoolDetailPanel({ pool, onClose, onEdit }: PoolDetailPanelProps) {
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

        <AddressAnalysis poolId={pool.id} />

        {/* Actions */}
        <div className="flex gap-2">
          {onEdit ? (
            <button
              onClick={onEdit}
              className="inline-flex flex-1 items-center justify-center gap-1.5 px-3 py-2 bg-blue-600 text-white text-sm rounded hover:bg-blue-700"
            >
              <Pencil className="h-4 w-4" />
              Edit Pool
            </button>
          ) : (
            <button
              onClick={() => navigate('/pools')}
              className="flex-1 px-3 py-2 bg-blue-600 text-white text-sm rounded hover:bg-blue-700"
            >
              Manage Pool
            </button>
          )}
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
