import { useEffect, useState, useCallback, useRef } from 'react'
import {
  RefreshCw,
  Link2,
  Unlink,
  Cloud,
  Search,
  BookOpen,
  ChevronDown,
  ChevronRight,
  Activity,
  Wand2,
} from 'lucide-react'
import {
  useDiscoveryResources,
  useSyncJobs,
  useDiscoveryAgents,
} from '../hooks/useDiscovery'
import { useAccounts } from '../hooks/useAccounts'
import { useToast } from '../hooks/useToast'
import StatusBadge from '../components/StatusBadge'
import DiscoveryWizard from '../components/DiscoveryWizard'
import { formatTimeAgo } from '../utils/format'
import type {
  Account,
  DiscoveredResource,
  SyncJob,
  DiscoveryAgent,
  AgentStatus,
} from '../api/types'

type Tab = 'resources' | 'sync' | 'agents'

export default function DiscoveryPage() {
  const [tab, setTab] = useState<Tab>('resources')
  const [selectedAccountId, setSelectedAccountId] = useState<number | null>(null)
  const [statusFilter, setStatusFilter] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const [linkedFilter, setLinkedFilter] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [showGuide, setShowGuide] = useState(false)
  const [showWizard, setShowWizard] = useState(false)

  const { accounts, fetchAccounts } = useAccounts()
  const {
    data: resourcesData,
    loading: resLoading,
    error: resError,
    fetch: fetchResources,
    linkToPool,
    unlinkFromPool,
  } = useDiscoveryResources()
  const {
    jobs,
    loading: jobsLoading,
    error: jobsError,
    fetch: fetchJobs,
    triggerSync,
  } = useSyncJobs()
  const {
    agents,
    loading: agentsLoading,
    error: agentsError,
    fetch: fetchAgents,
  } = useDiscoveryAgents()
  const { showToast } = useToast()
  const agentsRefreshInterval = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    fetchAccounts()
  }, [fetchAccounts])

  // Auto-select first account
  useEffect(() => {
    if (accounts.length > 0 && selectedAccountId === null) {
      setSelectedAccountId(accounts[0].id)
    }
  }, [accounts, selectedAccountId])

  // Fetch data when account/filters change
  const loadResources = useCallback(() => {
    if (selectedAccountId) {
      fetchResources(selectedAccountId, {
        status: statusFilter || undefined,
        resource_type: typeFilter || undefined,
        linked: linkedFilter || undefined,
      })
    }
  }, [selectedAccountId, statusFilter, typeFilter, linkedFilter, fetchResources])

  useEffect(() => {
    loadResources()
  }, [loadResources])

  useEffect(() => {
    if (selectedAccountId && tab === 'sync') {
      fetchJobs(selectedAccountId)
    }
  }, [selectedAccountId, tab, fetchJobs])

  // Auto-refresh agents every 30 seconds when on agents tab
  useEffect(() => {
    if (tab === 'agents') {
      fetchAgents(selectedAccountId ?? undefined)
      agentsRefreshInterval.current = setInterval(() => {
        fetchAgents(selectedAccountId ?? undefined)
      }, 30000)
    } else {
      if (agentsRefreshInterval.current) {
        clearInterval(agentsRefreshInterval.current)
        agentsRefreshInterval.current = null
      }
    }
    return () => {
      if (agentsRefreshInterval.current) {
        clearInterval(agentsRefreshInterval.current)
      }
    }
  }, [tab, selectedAccountId, fetchAgents])

  async function handleSync() {
    if (!selectedAccountId) return
    try {
      const job = await triggerSync(selectedAccountId)
      showToast(
        job.status === 'completed'
          ? `Sync complete: ${job.resources_found} resources found`
          : `Sync ${job.status}${job.error_message ? ': ' + job.error_message : ''}`,
        job.status === 'completed' ? 'success' : 'error',
      )
      loadResources()
      fetchJobs(selectedAccountId)
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Sync failed', 'error')
    }
  }

  async function handleLink(resource: DiscoveredResource) {
    const poolIdStr = prompt('Enter pool ID to link:')
    if (!poolIdStr) return
    const poolId = parseInt(poolIdStr, 10)
    if (isNaN(poolId) || poolId < 1) {
      showToast('Invalid pool ID', 'error')
      return
    }
    try {
      await linkToPool(resource.id, poolId)
      showToast('Resource linked to pool', 'success')
      loadResources()
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Link failed', 'error')
    }
  }

  async function handleUnlink(resource: DiscoveredResource) {
    try {
      await unlinkFromPool(resource.id)
      showToast('Resource unlinked', 'success')
      loadResources()
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Unlink failed', 'error')
    }
  }

  const filteredResources = (resourcesData?.items ?? []).filter((r) => {
    if (!searchQuery) return true
    const q = searchQuery.toLowerCase()
    return (
      r.name.toLowerCase().includes(q) ||
      r.resource_id.toLowerCase().includes(q) ||
      (r.cidr || '').toLowerCase().includes(q)
    )
  })

  if (accounts.length === 0) {
    return (
      <div className="flex-1 overflow-auto p-6">
        <div className="max-w-2xl mx-auto">
          <div className="text-center mb-8">
            <Cloud className="w-16 h-16 mx-auto mb-4 text-gray-300 dark:text-gray-600" />
            <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100 mb-2">
              Cloud Discovery
            </h2>
            <p className="text-gray-500 dark:text-gray-400 mb-4">
              Create a cloud account first to start discovering resources.
            </p>
            <button
              onClick={() => setShowWizard(true)}
              className="inline-flex items-center gap-2 rounded bg-green-600 hover:bg-green-700 text-white px-4 py-2 text-sm"
            >
              <Wand2 className="w-4 h-4" />
              Plan Discovery
            </button>
          </div>
          <SetupGuide defaultOpen />
        </div>
        {showWizard && (
          <DiscoveryWizard
            accounts={accounts}
            onAccountCreated={fetchAccounts}
            onClose={() => setShowWizard(false)}
            onComplete={() => {
              setTab('agents')
              fetchAgents(selectedAccountId ?? undefined)
            }}
          />
        )}
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">
            Cloud Discovery
          </h1>
          <button
            onClick={() => setShowGuide(!showGuide)}
            title="Setup guide"
            className="p-1.5 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 rounded hover:bg-gray-100 dark:hover:bg-gray-700"
          >
            <BookOpen className="w-4 h-4" />
          </button>
        </div>
        <div className="flex items-center gap-3">
          <select
            value={selectedAccountId ?? ''}
            onChange={(e) => setSelectedAccountId(Number(e.target.value))}
            className="rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 px-3 py-1.5 text-sm"
          >
            {accounts.map((a: Account) => (
              <option key={a.id} value={a.id}>
                {a.name} ({a.provider || 'unknown'})
              </option>
            ))}
          </select>
          <button
            onClick={() => setShowWizard(true)}
            className="flex items-center gap-2 rounded bg-green-600 hover:bg-green-700 text-white px-4 py-1.5 text-sm"
          >
            <Wand2 className="w-4 h-4" />
            Plan Discovery
          </button>
          <button
            onClick={handleSync}
            className="flex items-center gap-2 rounded bg-blue-600 hover:bg-blue-700 text-white px-4 py-1.5 text-sm"
          >
            <RefreshCw className="w-4 h-4" />
            Sync Now
          </button>
        </div>
      </div>

      {showGuide && (
        <div className="mb-4">
          <SetupGuide defaultOpen />
        </div>
      )}

      {/* Tabs */}
      <div className="flex gap-1 border-b border-gray-200 dark:border-gray-700 mb-4">
        <button
          onClick={() => setTab('resources')}
          className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px ${
            tab === 'resources'
              ? 'border-blue-600 text-blue-600 dark:text-blue-400'
              : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
          }`}
        >
          Resources
          {resourcesData && (
            <span className="ml-2 text-xs bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 px-1.5 py-0.5 rounded-full">
              {resourcesData.total}
            </span>
          )}
        </button>
        <button
          onClick={() => setTab('sync')}
          className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px ${
            tab === 'sync'
              ? 'border-blue-600 text-blue-600 dark:text-blue-400'
              : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
          }`}
        >
          Sync History
        </button>
        <button
          onClick={() => setTab('agents')}
          className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px ${
            tab === 'agents'
              ? 'border-blue-600 text-blue-600 dark:text-blue-400'
              : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
          }`}
        >
          Agents
          {agents.length > 0 && (
            <span className="ml-2 text-xs bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 px-1.5 py-0.5 rounded-full">
              {agents.length}
            </span>
          )}
        </button>
      </div>

      {tab === 'resources' && (
        <ResourcesTab
          resources={filteredResources}
          loading={resLoading}
          error={resError}
          searchQuery={searchQuery}
          onSearchChange={setSearchQuery}
          statusFilter={statusFilter}
          onStatusChange={setStatusFilter}
          typeFilter={typeFilter}
          onTypeChange={setTypeFilter}
          linkedFilter={linkedFilter}
          onLinkedChange={setLinkedFilter}
          onLink={handleLink}
          onUnlink={handleUnlink}
        />
      )}

      {tab === 'sync' && (
        <SyncTab jobs={jobs} loading={jobsLoading} error={jobsError} />
      )}

      {tab === 'agents' && (
        <AgentsTab
          agents={agents}
          loading={agentsLoading}
          error={agentsError}
        />
      )}

      {showWizard && (
        <DiscoveryWizard
          accounts={accounts}
          onAccountCreated={fetchAccounts}
          onClose={() => setShowWizard(false)}
        />
      )}
    </div>
  )
}

function ResourcesTab({
  resources,
  loading,
  error,
  searchQuery,
  onSearchChange,
  statusFilter,
  onStatusChange,
  typeFilter,
  onTypeChange,
  linkedFilter,
  onLinkedChange,
  onLink,
  onUnlink,
}: {
  resources: DiscoveredResource[]
  loading: boolean
  error: string | null
  searchQuery: string
  onSearchChange: (q: string) => void
  statusFilter: string
  onStatusChange: (s: string) => void
  typeFilter: string
  onTypeChange: (t: string) => void
  linkedFilter: string
  onLinkedChange: (l: string) => void
  onLink: (r: DiscoveredResource) => void
  onUnlink: (r: DiscoveredResource) => void
}) {
  return (
    <>
      {/* Filters */}
      <div className="flex flex-wrap gap-3 mb-4">
        <div className="relative flex-1 min-w-[200px]">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
          <input
            type="text"
            placeholder="Search by name, ID, or CIDR..."
            value={searchQuery}
            onChange={(e) => onSearchChange(e.target.value)}
            className="w-full pl-10 pr-3 py-1.5 text-sm rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100"
          />
        </div>
        <select
          value={typeFilter}
          onChange={(e) => onTypeChange(e.target.value)}
          className="rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 px-3 py-1.5 text-sm"
        >
          <option value="">All types</option>
          <option value="vpc">VPC</option>
          <option value="subnet">Subnet</option>
          <option value="elastic_ip">Elastic IP</option>
          <option value="network_interface">NIC</option>
        </select>
        <select
          value={statusFilter}
          onChange={(e) => onStatusChange(e.target.value)}
          className="rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 px-3 py-1.5 text-sm"
        >
          <option value="">All statuses</option>
          <option value="active">Active</option>
          <option value="stale">Stale</option>
          <option value="deleted">Deleted</option>
        </select>
        <select
          value={linkedFilter}
          onChange={(e) => onLinkedChange(e.target.value)}
          className="rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 px-3 py-1.5 text-sm"
        >
          <option value="">All</option>
          <option value="true">Linked to pool</option>
          <option value="false">Unlinked</option>
        </select>
      </div>

      {error && (
        <div className="mb-4 p-3 rounded bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-400 text-sm">
          {error}
        </div>
      )}

      {loading ? (
        <div className="text-center py-8 text-gray-500 dark:text-gray-400">
          Loading...
        </div>
      ) : resources.length === 0 ? (
        <div className="py-6">
          <div className="text-center mb-6 text-gray-500 dark:text-gray-400">
            No discovered resources. Click &quot;Sync Now&quot; to discover cloud
            resources, or read the setup guide below.
          </div>
          <SetupGuide defaultOpen />
        </div>
      ) : (
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900">
                <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
                  Type
                </th>
                <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
                  Name / ID
                </th>
                <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
                  CIDR
                </th>
                <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
                  Region
                </th>
                <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
                  Status
                </th>
                <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
                  Pool
                </th>
                <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
                  Last Seen
                </th>
                <th className="px-4 py-2 text-right text-gray-600 dark:text-gray-400 font-medium">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              {resources.map((r) => (
                <tr
                  key={r.id}
                  className="border-b border-gray-100 dark:border-gray-700/50 hover:bg-gray-50 dark:hover:bg-gray-700/50"
                >
                  <td className="px-4 py-2">
                    <ResourceTypeBadge type={r.resource_type} />
                  </td>
                  <td className="px-4 py-2">
                    <div className="text-gray-900 dark:text-gray-100 font-medium">
                      {r.name || r.resource_id}
                    </div>
                    {r.name && (
                      <div className="text-xs text-gray-500 dark:text-gray-400">
                        {r.resource_id}
                      </div>
                    )}
                  </td>
                  <td className="px-4 py-2 font-mono text-gray-700 dark:text-gray-300">
                    {r.cidr || '-'}
                  </td>
                  <td className="px-4 py-2 text-gray-600 dark:text-gray-400">
                    {r.region || '-'}
                  </td>
                  <td className="px-4 py-2">
                    <StatusBadge label={r.status} />
                  </td>
                  <td className="px-4 py-2 text-gray-600 dark:text-gray-400">
                    {r.pool_id ? (
                      <span className="text-blue-600 dark:text-blue-400">
                        Pool #{r.pool_id}
                      </span>
                    ) : (
                      <span className="text-gray-400 dark:text-gray-500">
                        unlinked
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2 text-gray-500 dark:text-gray-400">
                    {formatTimeAgo(r.last_seen_at)}
                  </td>
                  <td className="px-4 py-2 text-right">
                    {r.pool_id ? (
                      <button
                        onClick={() => onUnlink(r)}
                        title="Unlink from pool"
                        className="p-1 text-gray-400 hover:text-red-600 dark:hover:text-red-400"
                      >
                        <Unlink className="w-4 h-4" />
                      </button>
                    ) : (
                      <button
                        onClick={() => onLink(r)}
                        title="Link to pool"
                        className="p-1 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400"
                      >
                        <Link2 className="w-4 h-4" />
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}

function SyncTab({
  jobs,
  loading,
  error,
}: {
  jobs: SyncJob[]
  loading: boolean
  error: string | null
}) {
  if (error) {
    return (
      <div className="p-3 rounded bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-400 text-sm">
        {error}
      </div>
    )
  }

  if (loading) {
    return (
      <div className="text-center py-8 text-gray-500 dark:text-gray-400">
        Loading...
      </div>
    )
  }

  if (jobs.length === 0) {
    return (
      <div className="text-center py-8 text-gray-500 dark:text-gray-400">
        No sync jobs yet. Click "Sync Now" to run the first discovery.
      </div>
    )
  }

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900">
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Status
            </th>
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Started
            </th>
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Found
            </th>
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Created
            </th>
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Updated
            </th>
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Stale
            </th>
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Error
            </th>
          </tr>
        </thead>
        <tbody>
          {jobs.map((j) => (
            <tr
              key={j.id}
              className="border-b border-gray-100 dark:border-gray-700/50"
            >
              <td className="px-4 py-2">
                <StatusBadge label={j.status} />
              </td>
              <td className="px-4 py-2 text-gray-600 dark:text-gray-400">
                {j.started_at ? formatTimeAgo(j.started_at) : '-'}
              </td>
              <td className="px-4 py-2 text-gray-900 dark:text-gray-100">
                {j.resources_found}
              </td>
              <td className="px-4 py-2 text-green-600 dark:text-green-400">
                {j.resources_created}
              </td>
              <td className="px-4 py-2 text-blue-600 dark:text-blue-400">
                {j.resources_updated}
              </td>
              <td className="px-4 py-2 text-yellow-600 dark:text-yellow-400">
                {j.resources_deleted}
              </td>
              <td className="px-4 py-2 text-red-600 dark:text-red-400 text-xs max-w-xs truncate">
                {j.error_message || '-'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function ResourceTypeBadge({ type }: { type: string }) {
  const labels: Record<string, string> = {
    vpc: 'VPC',
    subnet: 'Subnet',
    elastic_ip: 'EIP',
    network_interface: 'NIC',
  }
  const colors: Record<string, string> = {
    vpc: 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400',
    subnet:
      'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
    elastic_ip:
      'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400',
    network_interface:
      'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300',
  }
  return (
    <span
      className={`inline-block px-2 py-0.5 rounded text-xs font-medium ${colors[type] || colors.network_interface}`}
    >
      {labels[type] || type}
    </span>
  )
}

function AgentsTab({
  agents,
  loading,
  error,
}: {
  agents: DiscoveryAgent[]
  loading: boolean
  error: string | null
}) {
  if (error) {
    return (
      <div className="p-3 rounded bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-400 text-sm">
        {error}
      </div>
    )
  }

  if (loading) {
    return (
      <div className="text-center py-8 text-gray-500 dark:text-gray-400">
        Loading...
      </div>
    )
  }

  if (agents.length === 0) {
    return (
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-6">
        <div className="text-center">
          <Activity className="w-12 h-12 mx-auto mb-3 text-gray-300 dark:text-gray-600" />
          <h3 className="text-lg font-medium text-gray-900 dark:text-gray-100 mb-2">
            No Agents Registered
          </h3>
          <p className="text-gray-500 dark:text-gray-400 mb-4">
            Deploy cloudpam-agent to start remote discovery. Agents run close to
            your cloud resources and push discovered data to this server.
          </p>
          <div className="bg-gray-50 dark:bg-gray-900 rounded p-4 text-left text-sm text-gray-600 dark:text-gray-400">
            <p className="font-medium text-gray-900 dark:text-gray-100 mb-2">
              Quick Start:
            </p>
            <ol className="list-decimal pl-5 space-y-2">
              <li>
                Provision an agent via the API:
                <pre className="mt-1 bg-gray-100 dark:bg-gray-800 rounded px-2 py-1.5 text-xs font-mono overflow-x-auto whitespace-pre">
{`curl -X POST /api/v1/discovery/agents/provision \\
  -H 'Content-Type: application/json' \\
  -d '{"name": "my-agent"}'`}
                </pre>
              </li>
              <li>
                Copy the{' '}
                <code className="text-xs bg-gray-100 dark:bg-gray-800 px-1 py-0.5 rounded">token</code>{' '}
                from the response (shown only once)
              </li>
              <li>
                Deploy the agent with the token:
                <pre className="mt-1 bg-gray-100 dark:bg-gray-800 rounded px-2 py-1.5 text-xs font-mono overflow-x-auto whitespace-pre">
{`CLOUDPAM_BOOTSTRAP_TOKEN=<token> \\
CLOUDPAM_ACCOUNT_ID=1 \\
./cloudpam-agent`}
                </pre>
              </li>
              <li>The agent registers automatically and appears here once it sends its first heartbeat</li>
            </ol>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900">
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Name
            </th>
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Status
            </th>
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Version
            </th>
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Hostname
            </th>
            <th className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
              Last Seen
            </th>
          </tr>
        </thead>
        <tbody>
          {agents.map((agent) => (
            <tr
              key={agent.id}
              className="border-b border-gray-100 dark:border-gray-700/50"
            >
              <td className="px-4 py-2 text-gray-900 dark:text-gray-100 font-medium">
                {agent.name}
              </td>
              <td className="px-4 py-2">
                <AgentStatusBadge status={agent.status} />
              </td>
              <td className="px-4 py-2 text-gray-600 dark:text-gray-400">
                {agent.version || 'unknown'}
              </td>
              <td className="px-4 py-2 text-gray-600 dark:text-gray-400">
                {agent.hostname || '-'}
              </td>
              <td className="px-4 py-2 text-gray-500 dark:text-gray-400">
                {formatTimeAgo(agent.last_seen_at)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function AgentStatusBadge({ status }: { status: AgentStatus }) {
  const colors: Record<AgentStatus, string> = {
    healthy:
      'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400',
    stale:
      'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400',
    offline: 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400',
  }
  return (
    <span
      className={`inline-block px-2 py-0.5 rounded text-xs font-medium ${colors[status]}`}
    >
      {status}
    </span>
  )
}

function SetupGuide({ defaultOpen = false }: { defaultOpen?: boolean }) {
  const [openSection, setOpenSection] = useState<string | null>(
    defaultOpen ? 'overview' : null,
  )

  function toggle(section: string) {
    setOpenSection(openSection === section ? null : section)
  }

  const sectionClass =
    'border-b border-gray-100 dark:border-gray-700/50 last:border-b-0'
  const headerClass =
    'flex items-center gap-2 w-full px-4 py-3 text-left text-sm font-medium text-gray-900 dark:text-gray-100 hover:bg-gray-50 dark:hover:bg-gray-700/50'
  const bodyClass =
    'px-4 pb-4 text-sm text-gray-600 dark:text-gray-400 space-y-3'

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
      <div className="px-4 py-3 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900 rounded-t-lg">
        <div className="flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-gray-100">
          <BookOpen className="w-4 h-4 text-blue-600 dark:text-blue-400" />
          Discovery Setup Guide
        </div>
      </div>

      {/* Overview */}
      <div className={sectionClass}>
        <button onClick={() => toggle('overview')} className={headerClass}>
          {openSection === 'overview' ? (
            <ChevronDown className="w-4 h-4 text-gray-400" />
          ) : (
            <ChevronRight className="w-4 h-4 text-gray-400" />
          )}
          How Discovery Works
        </button>
        {openSection === 'overview' && (
          <div className={bodyClass}>
            <p>
              Discovery automatically finds VPCs, subnets, and Elastic IPs in
              your cloud accounts. It uses an{' '}
              <strong>approval workflow</strong>: resources are stored separately
              from your pool hierarchy until you explicitly link them.
            </p>
            <div className="bg-gray-50 dark:bg-gray-900 rounded p-3 space-y-1">
              <div className="flex items-center gap-2">
                <span className="font-mono text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 px-1.5 py-0.5 rounded">
                  1
                </span>
                <span>
                  <strong>Sync</strong> &mdash; CloudPAM calls cloud APIs to
                  discover resources
                </span>
              </div>
              <div className="flex items-center gap-2">
                <span className="font-mono text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 px-1.5 py-0.5 rounded">
                  2
                </span>
                <span>
                  <strong>Review</strong> &mdash; discovered resources appear
                  here as &quot;unlinked&quot;
                </span>
              </div>
              <div className="flex items-center gap-2">
                <span className="font-mono text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 px-1.5 py-0.5 rounded">
                  3
                </span>
                <span>
                  <strong>Link</strong> &mdash; associate resources with your
                  IPAM pools to track them
                </span>
              </div>
            </div>
            <p>
              Resources not seen on a subsequent sync are marked{' '}
              <StatusBadge label="stale" /> &mdash; they may have been deleted
              from the cloud.
            </p>
          </div>
        )}
      </div>

      {/* AWS Setup */}
      <div className={sectionClass}>
        <button onClick={() => toggle('aws')} className={headerClass}>
          {openSection === 'aws' ? (
            <ChevronDown className="w-4 h-4 text-gray-400" />
          ) : (
            <ChevronRight className="w-4 h-4 text-gray-400" />
          )}
          AWS Configuration
        </button>
        {openSection === 'aws' && (
          <div className={bodyClass}>
            <p>
              <strong>Step 1:</strong> Create a cloud account on the{' '}
              <a
                href="/accounts"
                className="text-blue-600 dark:text-blue-400 underline"
              >
                Accounts page
              </a>{' '}
              with <code className="text-xs bg-gray-100 dark:bg-gray-900 px-1 py-0.5 rounded">provider: aws</code> and
              set the <code className="text-xs bg-gray-100 dark:bg-gray-900 px-1 py-0.5 rounded">regions</code> field
              (e.g., <code className="text-xs bg-gray-100 dark:bg-gray-900 px-1 py-0.5 rounded">us-east-1</code>).
            </p>
            <p>
              <strong>Step 2:</strong> Configure AWS credentials. The collector
              uses the standard AWS SDK credential chain:
            </p>
            <ul className="list-disc pl-5 space-y-1">
              <li>
                Environment variables:{' '}
                <code className="text-xs bg-gray-100 dark:bg-gray-900 px-1 py-0.5 rounded">
                  AWS_ACCESS_KEY_ID
                </code>
                ,{' '}
                <code className="text-xs bg-gray-100 dark:bg-gray-900 px-1 py-0.5 rounded">
                  AWS_SECRET_ACCESS_KEY
                </code>
              </li>
              <li>
                Shared credentials file{' '}
                <code className="text-xs bg-gray-100 dark:bg-gray-900 px-1 py-0.5 rounded">
                  ~/.aws/credentials
                </code>
              </li>
              <li>EC2 instance profile or ECS task role</li>
              <li>
                AWS SSO via{' '}
                <code className="text-xs bg-gray-100 dark:bg-gray-900 px-1 py-0.5 rounded">
                  aws sso login
                </code>
              </li>
            </ul>
            <p>
              <strong>Step 3:</strong> Ensure the IAM identity has these
              permissions:
            </p>
            <pre className="bg-gray-50 dark:bg-gray-900 rounded p-3 text-xs font-mono overflow-x-auto">
              {`ec2:DescribeVpcs
ec2:DescribeSubnets
ec2:DescribeAddresses`}
            </pre>
            <p>
              <strong>Step 4:</strong> Select the account above and click{' '}
              <strong>Sync Now</strong>.
            </p>
          </div>
        )}
      </div>

      {/* What gets discovered */}
      <div className={sectionClass}>
        <button onClick={() => toggle('resources')} className={headerClass}>
          {openSection === 'resources' ? (
            <ChevronDown className="w-4 h-4 text-gray-400" />
          ) : (
            <ChevronRight className="w-4 h-4 text-gray-400" />
          )}
          What Gets Discovered
        </button>
        {openSection === 'resources' && (
          <div className={bodyClass}>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div className="bg-gray-50 dark:bg-gray-900 rounded p-3">
                <div className="flex items-center gap-2 mb-1">
                  <ResourceTypeBadge type="vpc" />
                </div>
                <p className="text-xs">
                  VPC ID, CIDR block, Name tag, state, default flag
                </p>
              </div>
              <div className="bg-gray-50 dark:bg-gray-900 rounded p-3">
                <div className="flex items-center gap-2 mb-1">
                  <ResourceTypeBadge type="subnet" />
                </div>
                <p className="text-xs">
                  Subnet ID, CIDR block, VPC (parent), AZ, available IPs
                </p>
              </div>
              <div className="bg-gray-50 dark:bg-gray-900 rounded p-3">
                <div className="flex items-center gap-2 mb-1">
                  <ResourceTypeBadge type="elastic_ip" />
                </div>
                <p className="text-xs">
                  Allocation ID, public IP as /32, domain, associations
                </p>
              </div>
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-500">
              GCP and Azure collectors are planned for a future release.
            </p>
          </div>
        )}
      </div>

      {/* Agent Deployment */}
      <div className={sectionClass}>
        <button onClick={() => toggle('agent')} className={headerClass}>
          {openSection === 'agent' ? (
            <ChevronDown className="w-4 h-4 text-gray-400" />
          ) : (
            <ChevronRight className="w-4 h-4 text-gray-400" />
          )}
          Deploying a Discovery Agent
        </button>
        {openSection === 'agent' && (
          <div className={bodyClass}>
            <p>
              For production use, deploy the <strong>cloudpam-agent</strong>{' '}
              binary near your cloud resources. It discovers VPCs, subnets, and
              Elastic IPs, then pushes data to this server over HTTPS.
            </p>
            <div className="bg-gray-50 dark:bg-gray-900 rounded p-3 space-y-2">
              <div className="flex items-start gap-2">
                <span className="font-mono text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 px-1.5 py-0.5 rounded mt-0.5">
                  1
                </span>
                <span>
                  <strong>Provision</strong> &mdash;{' '}
                  <code className="text-xs bg-gray-100 dark:bg-gray-800 px-1 py-0.5 rounded">
                    POST /api/v1/discovery/agents/provision
                  </code>{' '}
                  to get a bootstrap token
                </span>
              </div>
              <div className="flex items-start gap-2">
                <span className="font-mono text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 px-1.5 py-0.5 rounded mt-0.5">
                  2
                </span>
                <span>
                  <strong>Deploy</strong> &mdash; set{' '}
                  <code className="text-xs bg-gray-100 dark:bg-gray-800 px-1 py-0.5 rounded">
                    CLOUDPAM_BOOTSTRAP_TOKEN
                  </code>{' '}
                  and{' '}
                  <code className="text-xs bg-gray-100 dark:bg-gray-800 px-1 py-0.5 rounded">
                    CLOUDPAM_ACCOUNT_ID
                  </code>
                </span>
              </div>
              <div className="flex items-start gap-2">
                <span className="font-mono text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 px-1.5 py-0.5 rounded mt-0.5">
                  3
                </span>
                <span>
                  <strong>Monitor</strong> &mdash; agents register automatically
                  and appear in the Agents tab
                </span>
              </div>
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-500">
              The token contains the server URL, API key, and agent name. See{' '}
              <code className="bg-gray-100 dark:bg-gray-800 px-1 py-0.5 rounded">
                docs/DISCOVERY.md
              </code>{' '}
              for Docker, Helm, and Terraform deployment guides.
            </p>
          </div>
        )}
      </div>

      {/* Linking */}
      <div className={sectionClass}>
        <button onClick={() => toggle('linking')} className={headerClass}>
          {openSection === 'linking' ? (
            <ChevronDown className="w-4 h-4 text-gray-400" />
          ) : (
            <ChevronRight className="w-4 h-4 text-gray-400" />
          )}
          Linking Resources to Pools
        </button>
        {openSection === 'linking' && (
          <div className={bodyClass}>
            <p>
              After syncing, discovered resources appear as{' '}
              <strong>unlinked</strong>. To track a cloud resource in your IPAM
              hierarchy:
            </p>
            <ol className="list-decimal pl-5 space-y-1">
              <li>Find the resource in the Resources tab</li>
              <li>
                Click the{' '}
                <Link2 className="w-3.5 h-3.5 inline text-blue-600 dark:text-blue-400" />{' '}
                link icon in the Actions column
              </li>
              <li>Enter the pool ID to associate it with</li>
            </ol>
            <p>
              Linking is advisory &mdash; it does not modify the cloud resource.
              Use the{' '}
              <Unlink className="w-3.5 h-3.5 inline text-red-600 dark:text-red-400" />{' '}
              unlink icon to remove the association.
            </p>
            <p>
              Use the filters above the table to find unlinked resources quickly:
              set the &quot;All&quot; dropdown to &quot;Unlinked&quot;.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}
