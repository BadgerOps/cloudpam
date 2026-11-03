import { AlertTriangle, Loader2, Radio, RefreshCw } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'
import { useLogDestinations } from '../hooks/useLogDestinations'
import type { LogDestination, LogDestinationState } from '../api/types'

const STATE_BADGES: Record<LogDestinationState, { label: string; className: string }> = {
  invalid: { label: 'Rejected', className: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300' },
  idle: { label: 'Waiting for events', className: 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300' },
  ok: { label: 'Delivering', className: 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300' },
  failing: { label: 'Failing', className: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300' },
}

const ENV_VARS: Array<[name: string, meaning: string]> = [
  ['CLOUDPAM_AUDIT_SYSLOG_ADDR', 'Syslog target as host:port. Setting it turns forwarding on.'],
  ['CLOUDPAM_AUDIT_SYSLOG_NETWORK', 'udp (default) or tcp. No TLS.'],
  ['CLOUDPAM_AUDIT_SYSLOG_APP_NAME', 'RFC 5424 APP-NAME in each message. Default cloudpam.'],
]

function formatTime(value?: string): string {
  if (!value) return 'never'
  const date = new Date(value)
  return isNaN(date.getTime()) ? 'unknown' : date.toLocaleString()
}

function DestinationCard({ destination }: { destination: LogDestination }) {
  const badge = STATE_BADGES[destination.state] ?? STATE_BADGES.idle
  const lastErrorTone = destination.state === 'failing'
    ? 'border-red-200 bg-red-50 text-red-900 dark:border-red-900 dark:bg-red-950 dark:text-red-100'
    : 'border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-100'

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-5 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <span className="inline-flex h-10 w-10 items-center justify-center rounded-lg bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300">
            <Radio className="h-5 w-5" />
          </span>
          <div>
            <h3 className="text-base font-semibold text-gray-900 dark:text-white">
              {destination.type === 'syslog' ? 'Syslog' : destination.type} ({destination.format.toUpperCase()})
            </h3>
            <code className="text-sm text-gray-600 dark:text-gray-300">{destination.network}://{destination.address}</code>
          </div>
        </div>
        <span
          data-testid="log-destination-state"
          className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${badge.className}`}
        >
          {badge.label}
        </span>
      </div>

      {destination.state === 'invalid' ? (
        <div className="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-900 dark:border-red-900 dark:bg-red-950 dark:text-red-100">
          Rejected at startup: {destination.config_error || 'unknown error'}. Nothing is forwarded. Fix the variable and restart the server.
        </div>
      ) : (
        <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <dt className="text-gray-500 dark:text-gray-400">App name</dt>
            <dd className="font-medium text-gray-900 dark:text-gray-100">{destination.app_name || 'cloudpam'}</dd>
          </div>
          <div>
            <dt className="text-gray-500 dark:text-gray-400">Delivered</dt>
            <dd className="font-medium text-gray-900 dark:text-gray-100">{destination.delivered}</dd>
          </div>
          <div>
            <dt className="text-gray-500 dark:text-gray-400">Failed</dt>
            <dd className="font-medium text-gray-900 dark:text-gray-100">{destination.failed}</dd>
          </div>
          <div>
            <dt className="text-gray-500 dark:text-gray-400">Last success</dt>
            <dd className="font-medium text-gray-900 dark:text-gray-100">{formatTime(destination.last_success_at)}</dd>
          </div>
        </dl>
      )}

      {destination.state !== 'invalid' && destination.last_error && (
        <div className={`rounded-lg border p-3 text-sm ${lastErrorTone}`}>
          <p className="font-medium">
            {destination.state === 'failing' ? 'Last delivery failed' : 'Earlier delivery failed'} at {formatTime(destination.last_failure_at)}
          </p>
          <p className="mt-1 break-words font-mono text-xs">{destination.last_error}</p>
        </div>
      )}
    </div>
  )
}

export default function LogDestinationsPage() {
  const { hasPermission } = useAuth()
  const canRead = hasPermission('settings:read')
  const { destinations, loading, refreshing, error, refreshedAt, refresh } = useLogDestinations(canRead)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Log Destinations</h1>
          <p className="text-gray-500 dark:text-gray-400 mt-1">
            Forward audit events to SIEM and log aggregation platforms
          </p>
        </div>
        {canRead && (
          <div className="flex items-center gap-3 text-sm text-gray-500 dark:text-gray-400">
            {refreshedAt && <span>Checked {refreshedAt.toLocaleTimeString()}</span>}
            <button
              type="button"
              onClick={() => { void refresh() }}
              disabled={refreshing}
              className="inline-flex items-center gap-2 rounded-lg border border-gray-300 dark:border-gray-600 px-3 py-1.5 text-sm font-medium text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-60"
            >
              <RefreshCw className={`h-4 w-4 ${refreshing ? 'animate-spin' : ''}`} />
              Refresh
            </button>
          </div>
        )}
      </div>

      {!canRead ? (
        <div className="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-100">
          You need the settings:read permission to see forwarding status.
        </div>
      ) : error ? (
        <div role="alert" className="flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-900 dark:border-red-900 dark:bg-red-950 dark:text-red-100">
          <AlertTriangle className="mt-0.5 h-4 w-4 flex-shrink-0" />
          <span>Could not load forwarding status: {error}</span>
        </div>
      ) : loading ? (
        <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading forwarding status
        </div>
      ) : destinations && destinations.length > 0 ? (
        <section className="space-y-4">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Audit forwarding</h2>
          {destinations.map((destination) => (
            <DestinationCard key={`${destination.type}-${destination.network}-${destination.address}`} destination={destination} />
          ))}
        </section>
      ) : (
        <section className="bg-white dark:bg-gray-800 rounded-lg border border-dashed border-gray-300 dark:border-gray-600 p-6">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Audit forwarding is off</h2>
          <p className="mt-2 text-sm text-gray-600 dark:text-gray-300">
            No destination is configured. Set <code>CLOUDPAM_AUDIT_SYSLOG_ADDR</code> on the server to forward every
            audit event as CEF over syslog, then restart it. This page then shows whether events reach the collector.
          </p>
        </section>
      )}

      <section className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
        <div className="border-b border-gray-200 dark:border-gray-700 px-6 py-4">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Configuration</h2>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
            Destinations are read from the server environment at startup. CloudPAM emits audit events as CEF over
            syslog, so point the address at any collector, forwarder, or SIEM listener that parses CEF.
          </p>
        </div>
        <div className="divide-y divide-gray-200 dark:divide-gray-700">
          {ENV_VARS.map(([name, meaning]) => (
            <div key={name} className="grid gap-2 px-6 py-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)]">
              <code className="text-sm font-semibold text-gray-900 dark:text-gray-100">{name}</code>
              <span className="text-sm text-gray-600 dark:text-gray-300">{meaning}</span>
            </div>
          ))}
        </div>
      </section>

      <div className="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-100">
        Application logs still use structured JSON on stdout. Use Vector, Fluent Bit, or your platform log
        collector for vendor-specific routes such as Splunk HEC or Datadog intake.
      </div>
    </div>
  )
}
