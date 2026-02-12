import { useEffect, useState } from 'react'
import { RefreshCw, ChevronDown, ChevronUp } from 'lucide-react'
import { useAudit } from '../hooks/useAudit'
import { formatTimeAgo, getActionBadgeClass } from '../utils/format'
import type { AuditEvent } from '../api/types'

export default function AuditPage() {
  const { events, total, offset, loading, error, fetchEvents, nextPage, prevPage, pageSize } = useAudit()
  const [actionFilter, setActionFilter] = useState('')
  const [resourceFilter, setResourceFilter] = useState('')
  const [expandedId, setExpandedId] = useState<string | null>(null)

  useEffect(() => {
    fetchEvents(0, pageSize, actionFilter || undefined, resourceFilter || undefined)
  }, [fetchEvents, pageSize, actionFilter, resourceFilter])

  function handleRefresh() {
    fetchEvents(offset, pageSize, actionFilter || undefined, resourceFilter || undefined)
  }

  return (
    <div className="flex-1 overflow-auto p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-xl font-semibold text-gray-900">Audit Log</h1>
        <button
          onClick={handleRefresh}
          className="inline-flex items-center gap-1.5 px-3 py-2 border text-sm rounded hover:bg-gray-50"
        >
          <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </button>
      </div>

      {/* Filters */}
      <div className="flex gap-3 mb-4">
        <select
          value={actionFilter}
          onChange={e => setActionFilter(e.target.value)}
          className="px-3 py-2 border rounded-lg text-sm"
        >
          <option value="">All Actions</option>
          <option value="create">Created</option>
          <option value="update">Updated</option>
          <option value="delete">Deleted</option>
        </select>
        <select
          value={resourceFilter}
          onChange={e => setResourceFilter(e.target.value)}
          className="px-3 py-2 border rounded-lg text-sm"
        >
          <option value="">All Resources</option>
          <option value="pool">Pools</option>
          <option value="account">Accounts</option>
          <option value="api_key">API Keys</option>
        </select>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 text-red-700 text-sm p-3 rounded mb-4">{error}</div>
      )}

      {loading && events.length === 0 ? (
        <div className="text-center py-12 text-gray-400">Loading audit events...</div>
      ) : events.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-gray-500">No audit events found</p>
        </div>
      ) : (
        <>
          {/* Timeline */}
          <div className="space-y-2">
            {events.map(e => (
              <EventRow
                key={e.id}
                event={e}
                expanded={expandedId === e.id}
                onToggle={() => setExpandedId(expandedId === e.id ? null : e.id)}
              />
            ))}
          </div>

          {/* Pagination */}
          <div className="flex items-center justify-between mt-4 text-sm text-gray-500">
            <span>Showing {offset + 1}-{Math.min(offset + pageSize, total)} of {total} events</span>
            <div className="flex gap-2">
              <button
                onClick={prevPage}
                disabled={offset === 0}
                className="px-3 py-1 border rounded hover:bg-gray-50 disabled:opacity-40"
              >
                Previous
              </button>
              <button
                onClick={nextPage}
                disabled={offset + pageSize >= total}
                className="px-3 py-1 border rounded hover:bg-gray-50 disabled:opacity-40"
              >
                Next
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  )
}

function EventRow({ event, expanded, onToggle }: {
  event: AuditEvent
  expanded: boolean
  onToggle: () => void
}) {
  return (
    <div className="bg-white border rounded-lg">
      <div
        className="flex items-center gap-3 px-4 py-3 cursor-pointer hover:bg-gray-50"
        onClick={onToggle}
      >
        <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${getActionBadgeClass(event.action)}`}>
          {event.action}
        </span>
        <span className="text-xs text-gray-400 bg-gray-100 px-1.5 py-0.5 rounded">{event.resource_type}</span>
        <span className="text-sm text-gray-700">{event.resource_name || event.resource_id}</span>
        <span className="ml-auto flex items-center gap-3 flex-shrink-0">
          <span className="text-xs text-gray-400">{event.actor}</span>
          <span className="text-xs text-gray-400">{formatTimeAgo(event.timestamp)}</span>
          {event.changes ? (
            expanded ? <ChevronUp className="w-3.5 h-3.5 text-gray-400" /> : <ChevronDown className="w-3.5 h-3.5 text-gray-400" />
          ) : <span className="w-3.5" />}
        </span>
      </div>
      {expanded && (
        <div className="px-4 pb-3 border-t">
          <div className="grid grid-cols-2 gap-4 mt-3 text-xs">
            <div>
              <span className="text-gray-500">Request ID:</span>{' '}
              <span className="font-mono text-gray-600">{event.request_id || '-'}</span>
            </div>
            <div>
              <span className="text-gray-500">IP Address:</span>{' '}
              <span className="font-mono text-gray-600">{event.ip_address || '-'}</span>
            </div>
            <div>
              <span className="text-gray-500">Status Code:</span>{' '}
              <span className="text-gray-600">{event.status_code}</span>
            </div>
            <div>
              <span className="text-gray-500">Timestamp:</span>{' '}
              <span className="text-gray-600">{new Date(event.timestamp).toLocaleString()}</span>
            </div>
          </div>
          {event.changes && (
            <div className="mt-3">
              <h4 className="text-xs font-medium text-gray-500 mb-1">Changes</h4>
              <div className="grid grid-cols-2 gap-2">
                {event.changes.before && (
                  <div className="bg-red-50 rounded p-2 text-xs">
                    <div className="font-medium text-red-700 mb-1">Before</div>
                    <pre className="text-red-600 whitespace-pre-wrap">{JSON.stringify(event.changes.before, null, 2)}</pre>
                  </div>
                )}
                {event.changes.after && (
                  <div className="bg-green-50 rounded p-2 text-xs">
                    <div className="font-medium text-green-700 mb-1">After</div>
                    <pre className="text-green-600 whitespace-pre-wrap">{JSON.stringify(event.changes.after, null, 2)}</pre>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
