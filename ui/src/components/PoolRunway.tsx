import { Loader2, TrendingUp } from 'lucide-react'
import type { GrowthProjection } from '../api/types'
import { usePoolProjection } from '../hooks/usePoolProjection'
import { formatHostCount, formatRunway, getUtilizationColor } from '../utils/format'

function formatRate(perDay: number): string {
  const rounded = perDay >= 10 ? Math.round(perDay) : Math.round(perDay * 10) / 10
  return formatHostCount(rounded) + ' IPs/day'
}

function formatMonthYear(iso: string): string {
  const date = new Date(iso)
  if (isNaN(date.getTime())) return 'unknown'
  return date.toLocaleDateString(undefined, { month: 'short', year: 'numeric' })
}

function runwayTone(days: number): string {
  if (days < 90) return 'text-red-600 dark:text-red-400'
  if (days < 365) return 'text-amber-600 dark:text-amber-400'
  return 'text-green-600 dark:text-green-400'
}

function Forecast({ projection }: { projection: GrowthProjection }) {
  return (
    <div className="grid grid-cols-3 gap-2 mt-2 text-center" data-testid="growth-forecast">
      {projection.forecast.map(point => (
        <div key={point.months}>
          <div className="text-xs text-gray-500 dark:text-gray-400">{point.months} mo</div>
          <div className="text-sm font-medium text-gray-900 dark:text-gray-100">{point.utilization.toFixed(0)}%</div>
          <div className="w-full h-1 bg-gray-200 dark:bg-gray-600 rounded-full overflow-hidden mt-1">
            <div
              className={`h-full rounded-full ${getUtilizationColor(point.utilization)}`}
              style={{ width: `${Math.min(point.utilization, 100)}%` }}
            />
          </div>
        </div>
      ))}
    </div>
  )
}

function ProjectionBody({ projection }: { projection: GrowthProjection }) {
  const window = `last ${projection.window_days} days`
  if (projection.status === 'insufficient_history') {
    return (
      <p className="text-xs text-gray-500 dark:text-gray-400">
        Not enough history to project growth yet; projections start after 7 days of allocations.
      </p>
    )
  }
  if (projection.status === 'exhausted') {
    return (
      <div>
        <div className="flex items-baseline justify-between">
          <span className="text-xs text-gray-500 dark:text-gray-400">Runway</span>
          <span data-testid="runway" className={`text-lg font-semibold ${runwayTone(0)}`}>None</span>
        </div>
        <p className="text-xs text-gray-500 dark:text-gray-400">The pool is fully allocated</p>
        <Forecast projection={projection} />
      </div>
    )
  }
  if (projection.status === 'no_growth' || projection.runway_days === undefined) {
    return (
      <div>
        <p className="text-sm text-gray-700 dark:text-gray-300">No growth over the {window}</p>
        <p className="text-xs text-gray-500 dark:text-gray-400">Runway is unbounded at the current pace</p>
        <Forecast projection={projection} />
      </div>
    )
  }
  return (
    <div>
      <div className="flex items-baseline justify-between">
        <span className="text-xs text-gray-500 dark:text-gray-400">Runway</span>
        <span data-testid="runway" className={`text-lg font-semibold ${runwayTone(projection.runway_days)}`}>
          {formatRunway(projection.runway_days)}
        </span>
      </div>
      <p className="text-xs text-gray-500 dark:text-gray-400">
        {formatRate(projection.growth_ips_per_day)} over the {window}
        {projection.exhaustion_date && <> · full by {formatMonthYear(projection.exhaustion_date)}</>}
      </p>
      <Forecast projection={projection} />
    </div>
  )
}

export default function PoolRunway({ poolId }: { poolId: number }) {
  const { projection, error, loading, retry } = usePoolProjection(poolId)

  return (
    <div className="border-t dark:border-gray-700 pt-3" data-testid="pool-runway">
      <div className="flex items-center gap-1 text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
        <TrendingUp className="w-3.5 h-3.5 text-gray-500 dark:text-gray-400" />
        Growth
      </div>
      {error ? (
        <div className="space-y-2">
          <p role="alert" className="text-xs text-red-600 dark:text-red-400">{error}</p>
          <button
            type="button"
            onClick={retry}
            className="px-2 py-1 border dark:border-gray-600 text-xs text-gray-700 dark:text-gray-300 rounded hover:bg-gray-50 dark:hover:bg-gray-700"
          >
            Retry
          </button>
        </div>
      ) : loading ? (
        <div className="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          Projecting growth...
        </div>
      ) : projection ? (
        <ProjectionBody projection={projection} />
      ) : (
        <p className="text-xs text-gray-500 dark:text-gray-400">Growth projection is not available for this pool</p>
      )}
    </div>
  )
}
