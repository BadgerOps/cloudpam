import { useEffect, useState } from 'react'
import {
  ArrowUpCircle,
  CheckCircle2,
  ExternalLink,
  Info,
  Loader2,
  Package,
  RefreshCw,
  ShieldAlert,
  TriangleAlert,
} from 'lucide-react'
import { useAuth } from '../hooks/useAuth'
import { useToast } from '../hooks/useToast'
import { useUpdates } from '../hooks/useUpdates'
import type { UpdateStatusResponse } from '../api/types'

const ACTIVE_UPGRADE_STATUSES = new Set([
  'upgrade_requested',
  'pending',
  'queued',
  'running',
  'backing_up',
  'pulling',
  'restarting',
  'verifying',
  'starting',
])

function normalizeStatus(status?: string): string {
  return (status || 'idle').trim().toLowerCase()
}

function isActiveUpgradeStatus(status?: string): boolean {
  return ACTIVE_UPGRADE_STATUSES.has(normalizeStatus(status))
}

function formatVersion(version?: string): string {
  if (!version) return 'Unavailable'
  if (version === 'dev') return version
  return version.startsWith('v') ? version : `v${version}`
}

function formatTimestamp(value?: string): string {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString()
}

function humanizeStatus(status?: string): string {
  const normalized = normalizeStatus(status)
  return normalized.replace(/_/g, ' ')
}

function statusBadgeClass(status?: string): string {
  switch (normalizeStatus(status)) {
    case 'completed':
    case 'healthy':
      return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
    case 'failed':
    case 'timeout':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    case 'upgrade_requested':
    case 'pending':
    case 'queued':
    case 'running':
    case 'backing_up':
    case 'pulling':
    case 'restarting':
    case 'verifying':
    case 'starting':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    default:
      return 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'
  }
}

function buildStatusRows(status: UpdateStatusResponse) {
  return [
    ['Status', humanizeStatus(status.status)],
    ['Target version', status.target_version ? formatVersion(status.target_version) : '—'],
    ['Target image tag', typeof status.target_image_tag === 'string' ? status.target_image_tag : '—'],
    ['Current version', status.current_version ? formatVersion(status.current_version) : '—'],
    ['Step', typeof status.step === 'string' && status.step ? status.step : '—'],
    ['Message', typeof status.message === 'string' && status.message ? status.message : '—'],
    ['Requested by', typeof status.requested_by === 'string' && status.requested_by ? status.requested_by : '—'],
    ['Requested at', formatTimestamp(typeof status.requested_at === 'string' ? status.requested_at : undefined)],
    ['Started at', formatTimestamp(typeof status.started_at === 'string' ? status.started_at : undefined)],
    ['Finished at', formatTimestamp(typeof status.finished_at === 'string' ? status.finished_at : undefined)],
    ['Backup path', typeof status.backup_path === 'string' && status.backup_path ? status.backup_path : '—'],
  ]
}

function SummaryCard({
  icon,
  label,
  value,
  detail,
}: {
  icon: React.ReactNode
  label: string
  value: string
  detail?: string
}) {
  return (
    <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-4 shadow-sm">
      <div className="flex items-center gap-3 mb-3">
        <div className="p-2 rounded-lg bg-blue-50 text-blue-600 dark:bg-blue-900/30 dark:text-blue-300">
          {icon}
        </div>
        <p className="text-sm font-medium text-gray-600 dark:text-gray-400">{label}</p>
      </div>
      <p className="text-2xl font-semibold text-gray-900 dark:text-white">{value}</p>
      {detail && <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">{detail}</p>}
    </div>
  )
}

function Callout({
  tone,
  children,
}: {
  tone: 'error' | 'warning' | 'info'
  children: React.ReactNode
}) {
  const classes = {
    error: 'bg-red-50 text-red-700 border-red-200 dark:bg-red-900/20 dark:text-red-300 dark:border-red-800',
    warning: 'bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-900/20 dark:text-amber-300 dark:border-amber-800',
    info: 'bg-blue-50 text-blue-700 border-blue-200 dark:bg-blue-900/20 dark:text-blue-300 dark:border-blue-800',
  }

  const Icon = tone === 'error' ? TriangleAlert : Info

  return (
    <div className={`flex items-start gap-3 border rounded-xl px-4 py-3 ${classes[tone]}`}>
      <Icon className="w-4 h-4 mt-0.5 flex-shrink-0" />
      <div className="text-sm">{children}</div>
    </div>
  )
}

export default function UpdatesPage() {
  const { role } = useAuth()
  const { showToast } = useToast()
  const {
    summary,
    status,
    loadingSummary,
    loadingStatus,
    actionLoading,
    summaryError,
    statusError,
    actionError,
    refreshSummary,
    refreshStatus,
    triggerUpgrade,
  } = useUpdates()
  const [pendingTargetVersion, setPendingTargetVersion] = useState<string | null>(null)

  useEffect(() => {
    if (pendingTargetVersion && normalizeStatus(status?.status) !== 'idle') {
      setPendingTargetVersion(null)
    }
  }, [pendingTargetVersion, status?.status])

  useEffect(() => {
    const shouldPoll = pendingTargetVersion !== null || isActiveUpgradeStatus(status?.status)
    if (!shouldPoll) return

    const interval = window.setInterval(() => {
      void refreshStatus()
      void refreshSummary(true)
    }, 5000)

    return () => window.clearInterval(interval)
  }, [pendingTargetVersion, refreshStatus, refreshSummary, status?.status])

  async function handleRefresh() {
    const results = await Promise.allSettled([refreshSummary(true), refreshStatus()])
    if (results.some((result) => result.status === 'rejected')) {
      showToast('Refresh completed with errors', 'error')
      return
    }
    showToast('Update metadata refreshed', 'success')
  }

  async function handleUpgrade() {
    try {
      const response = await triggerUpgrade()
      setPendingTargetVersion(response.target_version)
      showToast(`Upgrade request for ${formatVersion(response.target_version)} submitted`, 'success')
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to request upgrade', 'error')
    }
  }

  if (role !== 'admin') {
    return (
      <div className="p-6">
        <div className="max-w-2xl bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-6 shadow-sm">
          <div className="flex items-center gap-3 mb-3">
            <div className="p-2 rounded-lg bg-red-50 text-red-600 dark:bg-red-900/30 dark:text-red-300">
              <ShieldAlert className="w-5 h-5" />
            </div>
            <h1 className="text-xl font-semibold text-gray-900 dark:text-white">Updates</h1>
          </div>
          <p className="text-sm text-gray-600 dark:text-gray-300">
            Only administrators can view release metadata or trigger an in-app upgrade.
          </p>
        </div>
      </div>
    )
  }

  const displayStatus: UpdateStatusResponse =
    status && Object.keys(status).length > 0
      ? status
      : { status: pendingTargetVersion ? 'upgrade_requested' : 'idle', target_version: pendingTargetVersion ?? undefined }
  const waitingForHost = pendingTargetVersion !== null && normalizeStatus(status?.status) === 'idle'
  const canTriggerUpgrade = summary?.update_available === true && !actionLoading

  return (
    <div className="p-6 space-y-6">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Updates</h1>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
            Check GitHub Releases, submit an upgrade request, and watch the host-side rollout state.
          </p>
        </div>
        <div className="flex flex-wrap gap-3">
          <button
            onClick={() => void handleRefresh()}
            disabled={loadingSummary || loadingStatus}
            className="inline-flex items-center gap-2 px-4 py-2 rounded-lg border border-gray-300 text-sm font-medium text-gray-700 bg-white hover:bg-gray-50 disabled:opacity-50 dark:bg-gray-800 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
          >
            <RefreshCw className={`w-4 h-4 ${(loadingSummary || loadingStatus) ? 'animate-spin' : ''}`} />
            Check again
          </button>
          <button
            onClick={() => void handleUpgrade()}
            disabled={!canTriggerUpgrade}
            className="inline-flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-blue-400"
          >
            {actionLoading ? <Loader2 className="w-4 h-4 animate-spin" /> : <ArrowUpCircle className="w-4 h-4" />}
            {summary?.update_available ? `Upgrade to ${formatVersion(summary.latest_version)}` : 'No update available'}
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <SummaryCard
          icon={<Package className="w-5 h-5" />}
          label="Current Deployment"
          value={loadingSummary ? 'Loading...' : formatVersion(summary?.current_version)}
          detail={summary?.checked_at ? `Checked ${formatTimestamp(summary.checked_at)}` : 'Current application version reported by the server'}
        />
        <SummaryCard
          icon={<ArrowUpCircle className="w-5 h-5" />}
          label="Latest Stable Release"
          value={loadingSummary ? 'Loading...' : formatVersion(summary?.latest_version)}
          detail={summary?.published_at ? `Published ${formatTimestamp(summary.published_at)}` : 'No published stable release metadata available'}
        />
        <SummaryCard
          icon={<CheckCircle2 className="w-5 h-5" />}
          label="Upgrade State"
          value={humanizeStatus(displayStatus.status)}
          detail={displayStatus.target_version ? `Target ${formatVersion(displayStatus.target_version)}` : 'No upgrade requested'}
        />
      </div>

      {summaryError && <Callout tone="error">{summaryError}</Callout>}
      {statusError && <Callout tone="error">{statusError}</Callout>}
      {actionError && <Callout tone="error">{actionError}</Callout>}
      {summary?.error && <Callout tone="warning">{summary.error}</Callout>}
      {summary?.warning && <Callout tone="warning">{summary.warning}</Callout>}
      {waitingForHost && (
        <Callout tone="info">
          The upgrade request was written, but the host has not published progress yet. This page expects the NixOS
          control directory mount plus `cloudpam-upgrade-trigger.path` and `cloudpam-upgrade-handler.service`.
        </Callout>
      )}

      <div className="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1.2fr)_minmax(0,0.8fr)]">
        <section className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-5 shadow-sm">
          <div className="flex items-start justify-between gap-4 mb-4">
            <div>
              <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Release Notes</h2>
              <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
                Latest stable release information from GitHub.
              </p>
            </div>
            {summary?.release_url && (
              <a
                href={summary.release_url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 text-sm font-medium text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
              >
                View release
                <ExternalLink className="w-4 h-4" />
              </a>
            )}
          </div>

          {loadingSummary ? (
            <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
              <Loader2 className="w-4 h-4 animate-spin" />
              Loading release metadata...
            </div>
          ) : summary?.release_notes ? (
            <div className="rounded-xl bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 p-4">
              <pre className="whitespace-pre-wrap break-words text-sm leading-6 text-gray-700 dark:text-gray-200 font-sans">
                {summary.release_notes}
              </pre>
            </div>
          ) : (
            <p className="text-sm text-gray-500 dark:text-gray-400">No release notes are available for the latest stable release.</p>
          )}
        </section>

        <section className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-5 shadow-sm">
          <div className="flex items-start justify-between gap-4 mb-4">
            <div>
              <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Host Upgrade Status</h2>
              <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
                Latest status payload from `/api/v1/updates/status`.
              </p>
            </div>
            <span className={`inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold capitalize ${statusBadgeClass(displayStatus.status)}`}>
              {humanizeStatus(displayStatus.status)}
            </span>
          </div>

          {loadingStatus ? (
            <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
              <Loader2 className="w-4 h-4 animate-spin" />
              Loading upgrade status...
            </div>
          ) : (
            <dl className="space-y-3">
              {buildStatusRows(displayStatus).map(([label, value]) => (
                <div key={label} className="flex items-start justify-between gap-4 border-b border-gray-100 dark:border-gray-700 pb-3 last:border-b-0 last:pb-0">
                  <dt className="text-sm text-gray-500 dark:text-gray-400">{label}</dt>
                  <dd className="text-sm text-right text-gray-900 dark:text-white break-all">{value}</dd>
                </div>
              ))}
            </dl>
          )}
        </section>
      </div>

      <section className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-5 shadow-sm">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">Host Payload</h2>
        <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">
          Raw JSON is shown here because the NixOS handler can add rollout-specific fields over time.
        </p>
        <pre className="overflow-x-auto rounded-xl bg-gray-950 text-gray-100 text-sm p-4">
          {JSON.stringify(displayStatus, null, 2)}
        </pre>
      </section>
    </div>
  )
}
