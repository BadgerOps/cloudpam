import { useEffect, useState, useCallback, useMemo, useRef, type ReactNode } from 'react'
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
  Loader2,
  UploadCloud,
  Trash2,
  X,
  Plus,
  Network,
  Table2,
  GitBranch,
  ArrowRight,
} from 'lucide-react'
import {
  useDiscoveryResources,
  useSyncJobs,
  useDiscoveryAgents,
} from '../hooks/useDiscovery'
import { useAccounts } from '../hooks/useAccounts'
import { usePools } from '../hooks/usePools'
import { useNetworkView } from '../hooks/useNetwork'
import { useToast } from '../hooks/useToast'
import StatusBadge from '../components/StatusBadge'
import DiscoveryWizard from '../components/DiscoveryWizard'
import { formatTimeAgo } from '../utils/format'
import type {
  Account,
  CreatePoolRequest,
  DiscoveredResource,
  Pool,
  SyncJob,
  DiscoveryAgent,
  AgentStatus,
  DiscoveryImportPreviewResponse,
  DiscoveryImportPreviewItem,
  DiscoveryImportApplyResponse,
  NetworkNode,
  NetworkConflict,
  NetworkIssue,
  NetworkObject,
  NetworkRelationship,
  NetworkConflictActionResponse,
} from '../api/types'

type Tab = 'resources' | 'network' | 'sync' | 'agents'
type NetworkMode = 'hierarchy' | 'flat' | 'conflicts' | 'objects' | 'relationships'
type NetworkJumpRequest = {
  mode: NetworkMode
  query: string
  nodeID?: string
  token: number
}

const NETWORK_TRIAGE_GUIDES = [
  {
    type: 'unlinked_exact_pool',
    label: 'Link',
    summary: 'CIDR already exists as a pool. Link the discovery record instead of importing another pool.',
  },
  {
    type: 'alternate_exact_pool',
    label: 'Merge / relink',
    summary: 'Discovery is attached to one pool but exactly matches another. Move the soft link to the authoritative pool.',
  },
  {
    type: 'linked_pool_mismatch',
    label: 'Repair link',
    summary: 'The linked pool CIDR differs from cloud state. Relink, import the right pool, or mark intentional drift reviewed.',
  },
  {
    type: 'duplicate_cidr',
    label: 'Ignore expected reuse',
    summary: 'Repeated CIDRs can be normal in scoped tenancy. Review policy-scoped duplicates, then ignore or defer expected reuse.',
  },
] as const

function mergeJobs(current: SyncJob[], updates: SyncJob[]) {
  const byID = new Map(current.map((job) => [job.id, job]))
  updates.forEach((job) => byID.set(job.id, job))
  return Array.from(byID.values()).sort((a, b) =>
    new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  )
}

export default function DiscoveryPage() {
  const [tab, setTab] = useState<Tab>('resources')
  const [selectedAccountId, setSelectedAccountId] = useState<number | null>(null)
  const [statusFilter, setStatusFilter] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const [linkedFilter, setLinkedFilter] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [resourcePage, setResourcePage] = useState(1)
  const [resourcePageSize, setResourcePageSize] = useState(25)
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
    getJob,
    previewDiscoveryImport,
    applyDiscoveryImport,
  } = useSyncJobs()
  const {
    agents,
    loading: agentsLoading,
    error: agentsError,
    fetch: fetchAgents,
    deleteAgent,
  } = useDiscoveryAgents()
  const { pools, fetchPools, createPool } = usePools()
  const { showToast } = useToast()
  const agentsRefreshInterval = useRef<ReturnType<typeof setInterval> | null>(null)
  const scanPollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const [selectedScanAgentId, setSelectedScanAgentId] = useState('all')
  const [scanLoading, setScanLoading] = useState(false)
  const [importLoading, setImportLoading] = useState(false)
  const [applyImportLoading, setApplyImportLoading] = useState(false)
  const [selectedResourceIds, setSelectedResourceIds] = useState<string[]>([])
  const [importPreview, setImportPreview] = useState<DiscoveryImportPreviewResponse | null>(null)
  const [importApplyResult, setImportApplyResult] = useState<DiscoveryImportApplyResponse | null>(null)
  const [networkJumpRequest, setNetworkJumpRequest] = useState<NetworkJumpRequest | null>(null)
  const [trackedScanJobs, setTrackedScanJobs] = useState<SyncJob[]>([])
  const [linkingResource, setLinkingResource] = useState<DiscoveredResource | null>(null)
  const [linkingLoading, setLinkingLoading] = useState(false)
  const [bulkLinkingLoading, setBulkLinkingLoading] = useState(false)

  useEffect(() => {
    fetchAccounts()
  }, [fetchAccounts])

  useEffect(() => {
    fetchAgents()
  }, [fetchAgents])

  useEffect(() => {
    fetchPools()
  }, [fetchPools])

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
        q: searchQuery || undefined,
        page: resourcePage,
        page_size: resourcePageSize,
      })
    }
  }, [selectedAccountId, statusFilter, typeFilter, linkedFilter, searchQuery, resourcePage, resourcePageSize, fetchResources])

  useEffect(() => {
    loadResources()
  }, [loadResources])

  useEffect(() => {
    setResourcePage(1)
  }, [selectedAccountId, statusFilter, typeFilter, linkedFilter, searchQuery, resourcePageSize])

  useEffect(() => {
    setSelectedResourceIds([])
    setImportPreview(null)
    setImportApplyResult(null)
  }, [selectedAccountId])

  useEffect(() => {
    return () => {
      if (scanPollRef.current) {
        clearInterval(scanPollRef.current)
      }
    }
  }, [])

  useEffect(() => {
    if (selectedAccountId && tab === 'sync') {
      fetchJobs(selectedAccountId)
    }
  }, [selectedAccountId, tab, fetchJobs])

  // Auto-refresh agents every 30 seconds when on agents tab
  useEffect(() => {
    if (tab === 'agents') {
      fetchAgents()
      agentsRefreshInterval.current = setInterval(() => {
        fetchAgents()
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
  }, [tab, fetchAgents])

  const healthyAgents = agents.filter((agent) => agent.status === 'healthy')

  function pollScanJobs(jobIds: string[]) {
    if (scanPollRef.current) {
      clearInterval(scanPollRef.current)
    }
    const pending = new Set(jobIds)
    let attempts = 0
    scanPollRef.current = setInterval(async () => {
      attempts += 1
      const results = await Promise.allSettled(Array.from(pending).map((id) => getJob(id)))
      const updatedJobs: SyncJob[] = []
      results.forEach((result) => {
        if (result.status === 'fulfilled') {
          const job = result.value
          updatedJobs.push(job)
          if (job.status === 'completed' || job.status === 'failed') {
            pending.delete(job.id)
          }
        }
      })
      if (updatedJobs.length > 0) {
        setTrackedScanJobs((current) => mergeJobs(current, updatedJobs))
      }
      if (selectedAccountId) {
        fetchJobs(selectedAccountId)
      }
      if (pending.size === 0 || attempts >= 40) {
        if (scanPollRef.current) {
          clearInterval(scanPollRef.current)
          scanPollRef.current = null
        }
        fetchAccounts()
        loadResources()
        if (selectedAccountId) {
          fetchJobs(selectedAccountId)
        }
      }
    }, 3000)
  }

  async function handleSync(agentIdOverride?: string) {
    if (!selectedAccountId) return
    setScanLoading(true)
    try {
      const targetAgentId = agentIdOverride ?? selectedScanAgentId
      const response =
        targetAgentId === 'all' && healthyAgents.length > 0
          ? await triggerSync({ allAgents: true })
          : targetAgentId !== 'all'
            ? await triggerSync({ accountId: selectedAccountId, agentId: targetAgentId })
            : await triggerSync({ accountId: selectedAccountId })

      const queuedJobs = 'items' in response ? response.items : [response]
      setTrackedScanJobs((current) => mergeJobs(current, queuedJobs))
      const activeJobs = queuedJobs.filter((job) => job.status === 'pending' || job.status === 'running')
      if (activeJobs.length > 0) {
        showToast(
          activeJobs.length === 1
            ? 'Scan queued for connected agent'
            : `Scans queued for ${activeJobs.length} connected agents`,
          'success',
        )
        setTab('sync')
        pollScanJobs(activeJobs.map((job) => job.id))
      } else {
        const job = queuedJobs[0]
        showToast(
          job?.status === 'completed'
            ? `Scan complete: ${job.resources_found} resources found`
            : `Scan ${job?.status ?? 'requested'}${job?.error_message ? ': ' + job.error_message : ''}`,
          job?.status === 'failed' ? 'error' : 'success',
        )
      }
      fetchAccounts()
      loadResources()
      fetchJobs(selectedAccountId)
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Sync failed', 'error')
    } finally {
      setScanLoading(false)
    }
  }

  async function handlePreviewImport() {
    if (!selectedAccountId) return
    if (selectedResourceIds.length === 0) {
      showToast('Select discovered resources to preview import', 'error')
      return
    }
    setImportLoading(true)
    try {
      setImportApplyResult(null)
      const preview = await previewDiscoveryImport(selectedAccountId, selectedResourceIds)
      setImportPreview(preview)
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Import preview failed', 'error')
    } finally {
      setImportLoading(false)
    }
  }

  async function handleApplyImport() {
    if (!selectedAccountId || !importPreview) return
    const previewResourceIds = importPreview.items.map((item) => item.resource_id)
    const importableIds = importPreview.items.filter((item) => item.status === 'importable')
    if (importableIds.length === 0) {
      showToast('No importable resources in this preview', 'error')
      return
    }
    setApplyImportLoading(true)
    try {
      const result = await applyDiscoveryImport(selectedAccountId, previewResourceIds)
      const detail = result.errors.length > 0 ? ` (${result.errors.length} warning${result.errors.length === 1 ? '' : 's'})` : ''
      showToast(
        `Imported ${result.pools_created} pools and linked ${result.resources_linked} resources${detail}`,
        result.errors.length > 0 ? 'info' : 'success',
      )
      setImportApplyResult(result)
      setImportPreview(null)
      setSelectedResourceIds([])
      fetchPools()
      loadResources()
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Import apply failed', 'error')
    } finally {
      setApplyImportLoading(false)
    }
  }

  function toggleResourceSelection(resource: DiscoveredResource) {
    setSelectedResourceIds((current) =>
      current.includes(resource.id)
        ? current.filter((id) => id !== resource.id)
        : [...current, resource.id],
    )
  }

  function setVisibleImportableResources(resources: DiscoveredResource[], selected: boolean) {
    const ids = resources
      .filter((resource) => isSelectableDiscoveryResource(resource))
      .map((resource) => resource.id)
    if (selected) {
      setSelectedResourceIds((current) => Array.from(new Set([...current, ...ids])))
      return
    }
    setSelectedResourceIds((current) => current.filter((id) => !ids.includes(id)))
  }

  function handleViewImportResultResources(result: DiscoveryImportApplyResponse) {
    const query = importApplyResultQuery(result)
    if (!query) return
    setNetworkJumpRequest({ mode: 'flat', query, token: Date.now() })
    setTab('network')
  }

  async function handleDeleteAgent(agent: DiscoveryAgent) {
    if (!confirm(`Delete discovery agent "${agent.name}"? The agent will reappear if it is still running and heartbeating.`)) {
      return
    }
    try {
      await deleteAgent(agent.id)
      showToast('Agent deleted', 'success')
      fetchAgents()
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Delete agent failed', 'error')
    }
  }

  async function handleLink(resource: DiscoveredResource) {
    setLinkingResource(resource)
  }

  async function handleApplyLink(resource: DiscoveredResource, poolId: number) {
    setLinkingLoading(true)
    try {
      await linkToPool(resource.id, poolId)
      showToast('Resource linked to pool', 'success')
      setLinkingResource(null)
      loadResources()
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Link failed', 'error')
    } finally {
      setLinkingLoading(false)
    }
  }

  async function handleBulkLink(resourceIds: string[], poolId: number) {
    const currentResourcesByID = new Map((resourcesData?.items ?? []).map((resource) => [resource.id, resource]))
    const linkableResourceIds = resourceIds.filter((id) => {
      const resource = currentResourcesByID.get(id)
      return !resource || isSelectableDiscoveryResource(resource)
    })
    if (linkableResourceIds.length === 0) {
      showToast('Select discovered resources to link', 'error')
      return
    }
    setBulkLinkingLoading(true)
    try {
      const results = await Promise.allSettled(linkableResourceIds.map((id) => linkToPool(id, poolId)))
      const linkedIDs = linkableResourceIds.filter((_, index) => results[index].status === 'fulfilled')
      const failed = results.length - linkedIDs.length
      if (linkedIDs.length > 0) {
        showToast(
          `Linked ${linkedIDs.length} resource${linkedIDs.length === 1 ? '' : 's'} to pool`,
          failed > 0 ? 'info' : 'success',
        )
        setSelectedResourceIds((current) => current.filter((id) => !linkedIDs.includes(id)))
        loadResources()
      }
      if (failed > 0) {
        showToast(`${failed} resource${failed === 1 ? '' : 's'} failed to link`, 'error')
      }
    } finally {
      setBulkLinkingLoading(false)
    }
  }

  async function handleCreateAndLink(resource: DiscoveredResource, data: CreatePoolRequest) {
    setLinkingLoading(true)
    try {
      const pool = await createPool(data)
      await linkToPool(resource.id, pool.id)
      showToast('Pool created and resource linked', 'success')
      setLinkingResource(null)
      fetchPools()
      loadResources()
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Create and link failed', 'error')
    } finally {
      setLinkingLoading(false)
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

  const filteredResources = resourcesData?.items ?? []

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
              Set Up Discovery
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
    <div className="flex-1 overflow-auto p-4 sm:p-6">
      {/* Header */}
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between mb-4">
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
        <div className="flex flex-wrap items-center gap-2">
          <select
            value={selectedAccountId ?? ''}
            onChange={(e) => setSelectedAccountId(Number(e.target.value))}
            className="min-w-[180px] rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 px-3 py-1.5 text-sm"
          >
            {accounts.map((a: Account) => (
              <option key={a.id} value={a.id}>
                {a.name} ({a.provider || 'unknown'})
              </option>
            ))}
          </select>
          <button
            onClick={() => setShowWizard(true)}
            className="inline-flex items-center gap-2 rounded bg-green-600 hover:bg-green-700 text-white px-3 py-1.5 text-sm"
          >
            <Wand2 className="w-4 h-4" />
            Add Agent
          </button>
          <button
            onClick={() => void handlePreviewImport()}
            disabled={importLoading || selectedResourceIds.length === 0}
            className="inline-flex items-center gap-2 rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-700 px-3 py-1.5 text-sm disabled:opacity-50"
          >
            {importLoading ? <Loader2 className="w-4 h-4 animate-spin" /> : <UploadCloud className="w-4 h-4" />}
            Preview Import
          </button>
          <select
            value={selectedScanAgentId}
            onChange={(e) => setSelectedScanAgentId(e.target.value)}
            className="min-w-[190px] rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 px-3 py-1.5 text-sm"
          >
            <option value="all">
              {healthyAgents.length > 0 ? `All connected agents (${healthyAgents.length})` : 'Selected account'}
            </option>
            {agents.map((agent) => (
              <option key={agent.id} value={agent.id} disabled={agent.status !== 'healthy'}>
                {agent.name} ({agent.status})
              </option>
            ))}
          </select>
          <button
            onClick={() => void handleSync()}
            disabled={scanLoading}
            className="inline-flex items-center gap-2 rounded bg-blue-600 hover:bg-blue-700 text-white px-3 py-1.5 text-sm disabled:opacity-50"
          >
            {scanLoading ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
            Scan Now
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
          onClick={() => setTab('network')}
          className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px ${
            tab === 'network'
              ? 'border-blue-600 text-blue-600 dark:text-blue-400'
              : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
          }`}
        >
          Merged Network
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
        <div className="space-y-4">
          {importApplyResult && (
            <ImportApplySummaryPanel
              result={importApplyResult}
              onViewAffected={() => handleViewImportResultResources(importApplyResult)}
              onDismiss={() => setImportApplyResult(null)}
            />
          )}
          <ResourcesTab
            resources={filteredResources}
            loading={resLoading}
            error={resError}
            searchQuery={searchQuery}
            onSearchChange={(query) => {
              setSearchQuery(query)
              setResourcePage(1)
            }}
            statusFilter={statusFilter}
            onStatusChange={setStatusFilter}
            typeFilter={typeFilter}
            onTypeChange={setTypeFilter}
            linkedFilter={linkedFilter}
            onLinkedChange={setLinkedFilter}
            onLink={handleLink}
            onUnlink={handleUnlink}
            accounts={accounts}
            selectedAccountId={selectedAccountId}
            pools={pools}
            selectedResourceIds={selectedResourceIds}
            onToggleSelection={toggleResourceSelection}
            onSetVisibleSelection={setVisibleImportableResources}
            onClearSelection={() => setSelectedResourceIds([])}
            onBulkLink={handleBulkLink}
            bulkLinking={bulkLinkingLoading}
            total={resourcesData?.total ?? filteredResources.length}
            page={resourcesData?.page ?? resourcePage}
            pageSize={resourcesData?.page_size ?? resourcePageSize}
            onPageChange={setResourcePage}
            onPageSizeChange={(pageSize) => {
              setResourcePageSize(pageSize)
              setResourcePage(1)
            }}
          />
        </div>
      )}

      {tab === 'network' && (
        <NetworkTab selectedAccountId={selectedAccountId} accounts={accounts} pools={pools} jumpRequest={networkJumpRequest} />
      )}

      {tab === 'sync' && (
        <SyncTab jobs={mergeJobs(jobs, trackedScanJobs)} agents={agents} loading={jobsLoading} error={jobsError} />
      )}

      {tab === 'agents' && (
        <AgentsTab
          agents={agents}
          loading={agentsLoading}
          error={agentsError}
          onScan={(agentId) => {
            setSelectedScanAgentId(agentId)
            void handleSync(agentId)
          }}
          onDelete={(agent) => void handleDeleteAgent(agent)}
        />
      )}

      {showWizard && (
        <DiscoveryWizard
          accounts={accounts}
          onAccountCreated={fetchAccounts}
          onClose={() => setShowWizard(false)}
          onComplete={() => {
            setTab('agents')
            fetchAgents()
          }}
        />
      )}

      {linkingResource && (
        <ResourceLinkModal
          resource={linkingResource}
          pools={pools}
          loading={linkingLoading}
          onClose={() => setLinkingResource(null)}
          onLink={(poolId) => void handleApplyLink(linkingResource, poolId)}
          onCreateAndLink={(data) => void handleCreateAndLink(linkingResource, data)}
        />
      )}

      {importPreview && (
        <ImportPreviewModal
          preview={importPreview}
          pools={pools}
          loading={applyImportLoading}
          onClose={() => setImportPreview(null)}
          onApply={() => void handleApplyImport()}
        />
      )}
    </div>
  )
}

function isSelectableDiscoveryResource(resource: DiscoveredResource) {
  return !resource.pool_id
}

type ResourceColumnKey =
  | 'type'
  | 'name'
  | 'account'
  | 'cidr'
  | 'region'
  | 'status'
  | 'pool'
  | 'last_seen'

const RESOURCE_COLUMNS: Array<{ key: ResourceColumnKey; label: string }> = [
  { key: 'type', label: 'Type' },
  { key: 'name', label: 'Name / ID' },
  { key: 'account', label: 'Account / Project' },
  { key: 'cidr', label: 'CIDR' },
  { key: 'region', label: 'Region' },
  { key: 'status', label: 'Status' },
  { key: 'pool', label: 'Pool' },
  { key: 'last_seen', label: 'Last Seen' },
]

const DEFAULT_RESOURCE_COLUMNS: ResourceColumnKey[] = [
  'type',
  'name',
  'account',
  'cidr',
  'region',
  'status',
  'pool',
  'last_seen',
]

function NetworkTab({
  selectedAccountId,
  accounts,
  pools,
  jumpRequest,
}: {
  selectedAccountId: number | null
  accounts: Account[]
  pools: Pool[]
  jumpRequest?: NetworkJumpRequest | null
}) {
  const [mode, setMode] = useState<NetworkMode>('hierarchy')
  const [query, setQuery] = useState('')
  const [networkProvider, setNetworkProvider] = useState('')
  const [networkRegion, setNetworkRegion] = useState('')
  const [objectType, setObjectType] = useState('')
  const [objectState, setObjectState] = useState('')
  const [objectPoolID, setObjectPoolID] = useState('')
  const [objectSourceDiscoveredID, setObjectSourceDiscoveredID] = useState('')
  const [conflictType, setConflictType] = useState('')
  const [schemaPolicy, setSchemaPolicy] = useState('account_level')
  const [relationshipType, setRelationshipType] = useState('')
  const [relationshipState, setRelationshipState] = useState('')
  const [relationshipIDs, setRelationshipIDs] = useState<string[]>([])
  const [relationshipSourceKind, setRelationshipSourceKind] = useState('')
  const [relationshipSourceID, setRelationshipSourceID] = useState('')
  const [relationshipTargetKind, setRelationshipTargetKind] = useState('')
  const [relationshipTargetID, setRelationshipTargetID] = useState('')
  const [relationshipEntityKind, setRelationshipEntityKind] = useState('')
  const [relationshipEntityID, setRelationshipEntityID] = useState('')
  const [selectedConflict, setSelectedConflict] = useState<NetworkConflict | null>(null)
  const [pendingJumpNodeID, setPendingJumpNodeID] = useState<string | null>(null)
  const {
    flat,
    hierarchy,
    conflicts,
    objects,
    relationships,
    loading,
    error,
    fetchFlat,
    fetchHierarchy,
    fetchConflicts,
    fetchObjects,
    fetchRelationships,
    resolveConflict,
    linkConflict,
    importConflict,
    createPlaceholderParentConflict,
    resolveNetworkRelationship,
  } = useNetworkView()
  const { previewDiscoveryImport: previewNetworkImport } = useSyncJobs()
  const { showToast } = useToast()

  const filters = useMemo(() => ({
    account_id: selectedAccountId ?? undefined,
    provider: networkProvider || undefined,
    region: networkRegion || undefined,
    object_type: objectType || undefined,
    status: objectState || undefined,
    pool_id: objectPoolID ? Number(objectPoolID) : undefined,
    source_discovered_id: objectSourceDiscoveredID || undefined,
    conflict_type: conflictType || undefined,
    schema_policy: schemaPolicy,
    q: query || undefined,
  }), [selectedAccountId, networkProvider, networkRegion, objectType, objectState, objectPoolID, objectSourceDiscoveredID, conflictType, schemaPolicy, query])

  const relationshipFilters = useMemo(() => ({
    account_id: selectedAccountId ?? undefined,
    ids: relationshipIDs.length > 0 ? relationshipIDs : undefined,
    type: relationshipType || undefined,
    source_kind: relationshipSourceKind || undefined,
    source_id: relationshipSourceID || undefined,
    target_kind: relationshipTargetKind || undefined,
    target_id: relationshipTargetID || undefined,
    entity_kind: relationshipEntityKind || undefined,
    entity_id: relationshipEntityID || undefined,
    resolution_state: relationshipState || undefined,
  }), [selectedAccountId, relationshipIDs, relationshipType, relationshipSourceKind, relationshipSourceID, relationshipTargetKind, relationshipTargetID, relationshipEntityKind, relationshipEntityID, relationshipState])

  const load = useCallback(() => {
    if (mode === 'hierarchy') {
      void fetchHierarchy(filters)
    } else if (mode === 'flat') {
      void fetchFlat(filters)
    } else if (mode === 'conflicts') {
      void fetchConflicts(filters)
    } else if (mode === 'objects') {
      void fetchObjects(filters)
    } else {
      void fetchRelationships(relationshipFilters)
    }
  }, [fetchConflicts, fetchFlat, fetchHierarchy, fetchObjects, fetchRelationships, filters, mode, relationshipFilters])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    if (!jumpRequest) return
    setMode(jumpRequest.mode)
    setQuery(jumpRequest.query)
    setPendingJumpNodeID(jumpRequest.nodeID ?? null)
  }, [jumpRequest])

  const activeItems = mode === 'hierarchy' ? hierarchy?.items : flat?.items
  const activeTotal = mode === 'hierarchy' ? hierarchy?.total : flat?.total
  const activeConflictCount = mode === 'hierarchy' ? hierarchy?.conflict_count : flat?.conflict_count

  useEffect(() => {
    if (!pendingJumpNodeID || (mode !== 'flat' && mode !== 'hierarchy')) return
    const frame = window.requestAnimationFrame(() => {
      const target = document.getElementById(networkNodeElementID(pendingJumpNodeID))
      if (target) {
        target.scrollIntoView({ behavior: 'smooth', block: 'center' })
        setPendingJumpNodeID(null)
      }
    })
    return () => window.cancelAnimationFrame(frame)
  }, [mode, activeItems, pendingJumpNodeID])

  async function handleResolve(conflict: NetworkConflict, decision: string) {
    try {
      const resolved = await resolveConflict(conflict.id, decision)
      showToast(`Resolution requested: ${resolved.resolution_requested || decision}`, 'success')
      setSelectedConflict(resolved)
      load()
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Resolve failed', 'error')
    }
  }

  async function handleLinkAction(conflict: NetworkConflict, discoveredId: string, poolId: number, reason: string, override: boolean) {
    try {
      const resp = await linkConflict(conflict.id, {
        discovered_id: discoveredId,
        pool_id: poolId,
        reason: reason || undefined,
        override,
      })
      showToast(conflict.type === 'alternate_exact_pool' ? 'Resource relinked to pool' : 'Resource linked to pool', 'success')
      setSelectedConflict(resp.conflict)
      load()
      return resp
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Link action failed', 'error')
      throw err
    }
  }

  async function handleImportAction(conflict: NetworkConflict, resourceIds: string[], poolId: number | undefined, reason: string, override: boolean) {
    try {
      const resp = await importConflict(conflict.id, {
        resource_ids: resourceIds,
        pool_id: poolId,
        reason: reason || undefined,
        override,
      })
      showToast(`Imported ${resp.import?.pools_created ?? 0} pools and linked ${resp.import?.resources_linked ?? 0} resources`, 'success')
      setSelectedConflict(resp.conflict)
      load()
      return resp
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Import action failed', 'error')
      throw err
    }
  }

  async function handlePlaceholderParentAction(conflict: NetworkConflict, discoveredId: string, name: string, reason: string) {
    try {
      const resp = await createPlaceholderParentConflict(conflict.id, {
        discovered_id: discoveredId,
        name: name || undefined,
        reason: reason || undefined,
      })
      showToast(`Placeholder parent ${resp.network_object?.name || 'created'}`, 'success')
      setSelectedConflict(resp.conflict)
      load()
      return resp
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Placeholder parent action failed', 'error')
      throw err
    }
  }

  async function handleResolveRelationship(relationship: NetworkRelationship, resolutionState: string, reason: string) {
    try {
      await resolveNetworkRelationship({
        id: relationship.id,
        resolution_state: resolutionState,
        reason: reason || undefined,
      })
      showToast(`Relationship marked ${resolutionState}`, 'success')
      load()
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Relationship resolution failed', 'error')
      throw err
    }
  }

  async function handlePreviewImportAction(conflict: NetworkConflict, resourceIds: string[], poolId: number | undefined) {
    const accountId = conflict.account_ids?.[0] ?? selectedAccountId
    if (!accountId) {
      throw new Error('Conflict account is required for import preview')
    }
    return previewNetworkImport(accountId, resourceIds, poolId)
  }

  function showConflictInMode(conflict: NetworkConflict, nextMode: Extract<NetworkMode, 'flat' | 'hierarchy'>, requestedNodeID?: string) {
    const nodeID = requestedNodeID ?? conflict.node_ids?.[0]
    setMode(nextMode)
    if (nodeID) {
      setPendingJumpNodeID(nodeID)
      return
    }
    if (conflict.cidr) {
      setQuery(conflict.cidr)
    } else if (conflict.discovered_ids?.[0]) {
      setQuery(conflict.discovered_ids[0])
    }
  }

  function showRelationshipFilter(filter: {
    ids?: string[]
    type?: string
    source_kind?: string
    source_id?: string
    target_kind?: string
    target_id?: string
    entity_kind?: string
    entity_id?: string
  }) {
    setMode('relationships')
    setRelationshipIDs(filter.ids ?? [])
    setRelationshipEntityKind(filter.entity_kind || '')
    setRelationshipEntityID(filter.entity_id || '')
    setRelationshipType(filter.type || '')
    setRelationshipState('')
    setRelationshipSourceKind(filter.source_kind || '')
    setRelationshipSourceID(filter.source_id || '')
    setRelationshipTargetKind(filter.target_kind || '')
    setRelationshipTargetID(filter.target_id || '')
    if (filter.entity_kind && filter.entity_id) {
      setRelationshipSourceKind('')
      setRelationshipSourceID('')
      setRelationshipTargetKind('')
      setRelationshipTargetID('')
    }
    if (filter.ids?.length) {
      setRelationshipType('')
      setRelationshipSourceKind('')
      setRelationshipSourceID('')
      setRelationshipTargetKind('')
      setRelationshipTargetID('')
    }
  }

  function showRelationshipsForConflict(conflict: NetworkConflict) {
    const relationshipIDs = (conflict.relationships ?? []).map((relationship) => relationship.id).filter(Boolean)
    if (relationshipIDs.length > 0) {
      showRelationshipFilter({ ids: relationshipIDs })
      return
    }
    const discoveredID = conflict.discovered_ids?.[0]
    const poolID = conflict.pool_ids?.[0]
    showRelationshipFilter(discoveredID
      ? { entity_kind: 'discovered', entity_id: discoveredID }
      : poolID
        ? { entity_kind: 'pool', entity_id: String(poolID) }
        : { type: conflict.type })
  }

  function showRelationshipsForNode(node: NetworkNode) {
    const poolIDMatch = node.id.match(/^pool:(\d+)$/)
    const objectIDMatch = node.id.match(/^network_object:(\d+)$/)
    showRelationshipFilter({
      entity_kind: node.kind === 'pool' ? 'pool' : node.kind,
      entity_id: poolIDMatch?.[1] || objectIDMatch?.[1] || node.discovered_id || node.id,
    })
  }

  function showRelationshipsForObject(object: NetworkObject) {
    showRelationshipFilter({
      entity_kind: 'network_object',
      entity_id: String(object.id),
    })
  }

  const rowCount = mode === 'conflicts'
    ? conflicts?.total ?? 0
    : mode === 'objects'
      ? objects?.total ?? 0
      : mode === 'relationships'
        ? relationships?.total ?? 0
        : activeTotal ?? 0

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="inline-flex rounded border border-gray-200 bg-gray-50 p-1 text-sm dark:border-gray-700 dark:bg-gray-800">
          <NetworkModeButton active={mode === 'hierarchy'} onClick={() => setMode('hierarchy')} label="Hierarchy" />
          <NetworkModeButton active={mode === 'flat'} onClick={() => setMode('flat')} label="Flat" />
          <NetworkModeButton active={mode === 'conflicts'} onClick={() => setMode('conflicts')} label="Conflicts" />
          <NetworkModeButton active={mode === 'objects'} onClick={() => setMode('objects')} label="Objects" />
          <NetworkModeButton
            active={mode === 'relationships'}
            onClick={() => {
              setRelationshipIDs([])
              setRelationshipEntityKind('')
              setRelationshipEntityID('')
              setMode('relationships')
            }}
            label="Relationships"
          />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-[220px] flex-1">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search network"
              className="w-full rounded border border-gray-300 bg-white py-1.5 pl-10 pr-3 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
            />
          </div>
          <select
            value={schemaPolicy}
            onChange={(e) => setSchemaPolicy(e.target.value)}
            className="rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
          >
            <option value="account_level">Account policy</option>
            <option value="region_level">Region policy</option>
            <option value="global">Global policy</option>
            <option value="manual">Manual policy</option>
          </select>
          {(mode === 'objects' || mode === 'hierarchy' || mode === 'flat' || mode === 'conflicts') && (
            <>
              <input
                value={networkProvider}
                onChange={(e) => setNetworkProvider(e.target.value)}
                placeholder="Provider"
                className="w-28 rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              />
              <input
                value={networkRegion}
                onChange={(e) => setNetworkRegion(e.target.value)}
                placeholder="Region"
                className="w-32 rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              />
            </>
          )}
          <select
            value={objectType}
            onChange={(e) => setObjectType(e.target.value)}
            className="rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
          >
            <option value="">All objects</option>
            <option value="supernet">Pool</option>
            <option value="vpc">VPC</option>
            <option value="subnet">Subnet</option>
            <option value="eip">EIP object</option>
            <option value="public_ip">Public IP object</option>
            <option value="network">Network object</option>
            <option value="elastic_ip">EIP</option>
            <option value="network_interface">NIC</option>
          </select>
          {mode === 'objects' && (
            <>
              <select
                value={objectState}
                onChange={(e) => setObjectState(e.target.value)}
                className="rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              >
                <option value="">All states</option>
                <option value="managed">Managed</option>
                <option value="placeholder">Placeholder</option>
                <option value="imported">Imported</option>
                <option value="ignored">Ignored</option>
              </select>
              <input
                value={objectPoolID}
                onChange={(e) => setObjectPoolID(e.target.value)}
                placeholder="Pool ID"
                inputMode="numeric"
                className="w-28 rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              />
              <input
                value={objectSourceDiscoveredID}
                onChange={(e) => setObjectSourceDiscoveredID(e.target.value)}
                placeholder="Source discovered ID"
                className="w-44 rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              />
            </>
          )}
          <select
            value={conflictType}
            onChange={(e) => setConflictType(e.target.value)}
            className="rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
          >
            <option value="">All issues</option>
            <option value="missing_parent">Missing parent</option>
            <option value="unlinked_exact_pool">Exact pool match</option>
            <option value="alternate_exact_pool">Relink candidate</option>
            <option value="invalid_nesting">Invalid nesting</option>
            <option value="outside_pool">Outside pool</option>
            <option value="duplicate_cidr">Duplicate CIDR</option>
            <option value="managed_overlap">Managed overlap</option>
            <option value="linked_pool_mismatch">Linked mismatch</option>
          </select>
          {mode === 'relationships' && (
            <>
              <select
                value={relationshipType}
                onChange={(e) => {
                  setRelationshipIDs([])
                  setRelationshipEntityKind('')
                  setRelationshipEntityID('')
                  setRelationshipType(e.target.value)
                }}
                className="rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              >
                <option value="">All relationships</option>
                <option value="contains">Contains</option>
                <option value="matches">Matches</option>
                <option value="conflicts">Conflicts</option>
                <option value="missing_parent">Missing parent</option>
                <option value="candidate_import">Candidate import</option>
                <option value="imported_as">Imported as</option>
                <option value="duplicate_of">Duplicate of</option>
              </select>
              <select
                value={relationshipState}
                onChange={(e) => setRelationshipState(e.target.value)}
                className="rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              >
                <option value="">All resolutions</option>
                <option value="open">Open</option>
                <option value="resolved">Resolved</option>
                <option value="ignored">Ignored</option>
              </select>
              <input
                value={relationshipSourceKind}
                onChange={(e) => {
                  setRelationshipIDs([])
                  setRelationshipEntityKind('')
                  setRelationshipEntityID('')
                  setRelationshipSourceKind(e.target.value)
                }}
                placeholder="Source kind"
                className="w-32 rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              />
              <input
                value={relationshipSourceID}
                onChange={(e) => {
                  setRelationshipIDs([])
                  setRelationshipEntityKind('')
                  setRelationshipEntityID('')
                  setRelationshipSourceID(e.target.value)
                }}
                placeholder="Source ID"
                className="w-36 rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              />
              <input
                value={relationshipTargetKind}
                onChange={(e) => {
                  setRelationshipIDs([])
                  setRelationshipEntityKind('')
                  setRelationshipEntityID('')
                  setRelationshipTargetKind(e.target.value)
                }}
                placeholder="Target kind"
                className="w-32 rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              />
              <input
                value={relationshipTargetID}
                onChange={(e) => {
                  setRelationshipIDs([])
                  setRelationshipEntityKind('')
                  setRelationshipEntityID('')
                  setRelationshipTargetID(e.target.value)
                }}
                placeholder="Target ID"
                className="w-36 rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              />
            </>
          )}
          <button
            type="button"
            onClick={load}
            className="inline-flex items-center gap-1.5 rounded border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
        <span>{rowCount} rows</span>
        {(mode === 'hierarchy' || mode === 'flat') && <span>{activeConflictCount ?? 0} issues</span>}
        <span>{schemaPolicy.replace(/_/g, ' ')} policy, request-scoped</span>
        {selectedAccountId && <span>{accounts.find((a) => a.id === selectedAccountId)?.name || `Account ${selectedAccountId}`}</span>}
      </div>

      <NetworkTriageGuide
        schemaPolicy={schemaPolicy}
        onShowConflicts={(type) => {
          setMode('conflicts')
          setConflictType(type)
          setSelectedConflict(null)
        }}
        onShowAllConflicts={() => {
          setMode('conflicts')
          setConflictType('')
          setSelectedConflict(null)
        }}
      />

      {error && (
        <div className="rounded bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">
          {error}
        </div>
      )}

      {mode === 'conflicts' ? (
        <NetworkConflictList
          conflicts={conflicts?.items ?? []}
          loading={loading}
          selected={selectedConflict}
          onSelect={setSelectedConflict}
          onResolve={handleResolve}
          onLink={handleLinkAction}
          onImport={handleImportAction}
          onPlaceholderParent={handlePlaceholderParentAction}
          onPreviewImport={handlePreviewImportAction}
          onViewFlat={(conflict, nodeID) => showConflictInMode(conflict, 'flat', nodeID)}
          onViewHierarchy={(conflict, nodeID) => showConflictInMode(conflict, 'hierarchy', nodeID)}
          onShowRelationships={showRelationshipsForConflict}
          pools={pools}
          accounts={accounts}
        />
      ) : mode === 'objects' ? (
        <NetworkObjectTable
          objects={objects?.items ?? []}
          loading={loading}
          accounts={accounts}
          pools={pools}
          onShowRelationships={showRelationshipsForObject}
        />
      ) : mode === 'relationships' ? (
        <NetworkRelationshipTable
          relationships={relationships?.items ?? []}
          loading={loading}
          onResolve={handleResolveRelationship}
        />
      ) : mode === 'flat' ? (
        <NetworkFlatTable nodes={activeItems ?? []} loading={loading} highlightedNodeID={pendingJumpNodeID} onShowRelationships={showRelationshipsForNode} />
      ) : (
        <NetworkHierarchy nodes={activeItems ?? []} loading={loading} highlightedNodeID={pendingJumpNodeID} onShowRelationships={showRelationshipsForNode} />
      )}
    </div>
  )
}

function NetworkModeButton({
  active,
  onClick,
  label,
}: {
  active: boolean
  onClick: () => void
  label: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded px-3 py-1.5 ${active ? 'bg-white text-gray-900 shadow-sm dark:bg-gray-700 dark:text-gray-100' : 'text-gray-600 dark:text-gray-300'}`}
    >
      {label}
    </button>
  )
}

function NetworkTriageGuide({
  schemaPolicy,
  onShowConflicts,
  onShowAllConflicts,
}: {
  schemaPolicy: string
  onShowConflicts: (type: string) => void
  onShowAllConflicts: () => void
}) {
  return (
    <div className="rounded border border-blue-100 bg-blue-50/70 p-3 text-sm dark:border-blue-900/50 dark:bg-blue-950/30">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <div className="flex items-center gap-2 font-semibold text-blue-950 dark:text-blue-100">
            <BookOpen className="h-4 w-4" />
            Discovery triage
          </div>
          <div className="mt-1 text-blue-900 dark:text-blue-200">
            Start with link and relink candidates, import only resources that should become managed pools, and mark expected duplicate address space reviewed.
          </div>
          {schemaPolicy === 'account_level' && (
            <div className="mt-1 text-xs text-blue-800 dark:text-blue-300">
              Account policy flags repeated CIDRs inside one account. Use region or global policy when tenancy boundaries follow regions or shared global pools.
            </div>
          )}
        </div>
        <button
          type="button"
          onClick={onShowAllConflicts}
          className="inline-flex shrink-0 items-center gap-1 rounded border border-blue-300 px-2.5 py-1 text-xs font-medium text-blue-900 hover:bg-blue-100 dark:border-blue-800 dark:text-blue-200 dark:hover:bg-blue-900/40"
        >
          <ArrowRight className="h-3.5 w-3.5" />
          Review queue
        </button>
      </div>
      <div className="mt-3 grid gap-2 md:grid-cols-2 xl:grid-cols-4">
        {NETWORK_TRIAGE_GUIDES.map((item) => (
          <div key={item.type} className="rounded border border-blue-100 bg-white/80 p-2 dark:border-blue-900/40 dark:bg-gray-900/50">
            <div className="flex items-start justify-between gap-2">
              <div>
                <div className="text-xs font-semibold uppercase text-blue-700 dark:text-blue-300">{item.label}</div>
                <div className="mt-1 text-xs text-gray-600 dark:text-gray-300">{item.summary}</div>
              </div>
              <button
                type="button"
                onClick={() => onShowConflicts(item.type)}
                className="rounded border border-gray-300 px-2 py-0.5 text-xs text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
              >
                Show
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function networkNodeElementID(id: string) {
  return `network-node-${id.replace(/[^a-zA-Z0-9_-]/g, '-')}`
}

function NetworkHierarchy({
  nodes,
  loading,
  highlightedNodeID,
  onShowRelationships,
}: {
  nodes: NetworkNode[]
  loading: boolean
  highlightedNodeID: string | null
  onShowRelationships: (node: NetworkNode) => void
}) {
  if (loading) return <div className="py-8 text-center text-gray-500 dark:text-gray-400">Loading...</div>
  if (nodes.length === 0) return <div className="py-8 text-center text-gray-500 dark:text-gray-400">No merged network records found.</div>
  return (
    <div className="rounded border border-gray-200 bg-white p-2 dark:border-gray-700 dark:bg-gray-800">
      {nodes.map((node) => (
        <NetworkTreeNode key={node.id} node={node} depth={0} highlightedNodeID={highlightedNodeID} onShowRelationships={onShowRelationships} />
      ))}
    </div>
  )
}

function NetworkTreeNode({
  node,
  depth,
  highlightedNodeID,
  onShowRelationships,
}: {
  node: NetworkNode
  depth: number
  highlightedNodeID: string | null
  onShowRelationships: (node: NetworkNode) => void
}) {
  const [expanded, setExpanded] = useState(depth < 2)
  const hasChildren = (node.children?.length ?? 0) > 0
  const highlighted = highlightedNodeID === node.id
  return (
    <div>
      <div
        id={networkNodeElementID(node.id)}
        className={`flex min-h-10 items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-gray-50 dark:hover:bg-gray-700/50 ${highlighted ? 'ring-2 ring-blue-500 bg-blue-50 dark:bg-blue-900/20' : ''}`}
        style={{ paddingLeft: `${depth * 18 + 8}px` }}
      >
        {hasChildren ? (
          <button
            type="button"
            onClick={() => setExpanded(!expanded)}
            className="rounded p-0.5 text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-600"
          >
            {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </button>
        ) : (
          <span className="w-4" />
        )}
        <NetworkObjectIcon node={node} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium text-gray-900 dark:text-gray-100">{node.name}</span>
            <span className="font-mono text-xs text-gray-500 dark:text-gray-400">{node.cidr || node.ip_address || '-'}</span>
            <StatusBadge label={node.state} />
          </div>
          <NetworkIssueBadges issues={node.issues ?? []} />
          <RelationshipBadges relationships={node.relationships ?? []} />
        </div>
        {(node.relationships?.length ?? 0) > 0 && (
          <button
            type="button"
            onClick={() => onShowRelationships(node)}
            className="inline-flex items-center gap-1 rounded border border-gray-300 px-2 py-1 text-xs text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
          >
            <GitBranch className="h-3.5 w-3.5" />
            Relationships
          </button>
        )}
      </div>
      {expanded && hasChildren && (
        <div>
          {node.children!.map((child) => (
            <NetworkTreeNode key={child.id} node={child} depth={depth + 1} highlightedNodeID={highlightedNodeID} onShowRelationships={onShowRelationships} />
          ))}
        </div>
      )}
    </div>
  )
}

function NetworkFlatTable({
  nodes,
  loading,
  highlightedNodeID,
  onShowRelationships,
}: {
  nodes: NetworkNode[]
  loading: boolean
  highlightedNodeID: string | null
  onShowRelationships: (node: NetworkNode) => void
}) {
  if (loading) return <div className="py-8 text-center text-gray-500 dark:text-gray-400">Loading...</div>
  return (
    <div className="overflow-x-auto rounded border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-200 bg-gray-50 text-left dark:border-gray-700 dark:bg-gray-900">
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Object</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">CIDR/IP</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Provider</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Account</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Region</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">State</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Issues</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Relationships</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Actions</th>
          </tr>
        </thead>
        <tbody>
          {nodes.length === 0 && (
            <tr>
              <td colSpan={9} className="px-3 py-8 text-center text-gray-500 dark:text-gray-400">No merged network records found.</td>
            </tr>
          )}
          {nodes.map((node) => (
            <tr
              key={node.id}
              id={networkNodeElementID(node.id)}
              className={`border-b border-gray-100 last:border-b-0 dark:border-gray-700/50 ${highlightedNodeID === node.id ? 'bg-blue-50 ring-2 ring-inset ring-blue-500 dark:bg-blue-900/20' : ''}`}
            >
              <td className="px-3 py-2">
                <div className="flex items-center gap-2">
                  <NetworkObjectIcon node={node} />
                  <div>
                    <div className="font-medium text-gray-900 dark:text-gray-100">{node.name}</div>
                    <div className="text-xs text-gray-500 dark:text-gray-400">{node.object_type}</div>
                  </div>
                </div>
              </td>
              <td className="px-3 py-2 font-mono text-gray-700 dark:text-gray-300">{node.cidr || node.ip_address || '-'}</td>
              <td className="px-3 py-2 text-gray-600 dark:text-gray-400">{node.provider || '-'}</td>
              <td className="px-3 py-2 text-gray-600 dark:text-gray-400">{node.account_name || '-'}</td>
              <td className="px-3 py-2 text-gray-600 dark:text-gray-400">{node.region || '-'}</td>
              <td className="px-3 py-2"><StatusBadge label={node.state} /></td>
              <td className="px-3 py-2"><NetworkIssueBadges issues={node.issues ?? []} /></td>
              <td className="px-3 py-2"><RelationshipBadges relationships={node.relationships ?? []} /></td>
              <td className="px-3 py-2">
                {(node.relationships?.length ?? 0) > 0 ? (
                  <button
                    type="button"
                    onClick={() => onShowRelationships(node)}
                    className="inline-flex items-center gap-1 rounded border border-gray-300 px-2 py-1 text-xs text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                  >
                    <GitBranch className="h-3.5 w-3.5" />
                    Relationships
                  </button>
                ) : '-'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function NetworkObjectTable({
  objects,
  loading,
  accounts,
  pools,
  onShowRelationships,
}: {
  objects: NetworkObject[]
  loading: boolean
  accounts: Account[]
  pools: Pool[]
  onShowRelationships: (object: NetworkObject) => void
}) {
  if (loading) return <div className="py-8 text-center text-gray-500 dark:text-gray-400">Loading...</div>
  return (
    <div className="overflow-x-auto rounded border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-200 bg-gray-50 text-left dark:border-gray-700 dark:bg-gray-900">
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Object</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">CIDR/IP</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Provider</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Account</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Region</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">State</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Parent</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Pool</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Source</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Updated</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Actions</th>
          </tr>
        </thead>
        <tbody>
          {objects.length === 0 && (
            <tr>
              <td colSpan={11} className="px-3 py-8 text-center text-gray-500 dark:text-gray-400">No managed network objects found.</td>
            </tr>
          )}
          {objects.map((object) => {
            const account = accounts.find((item) => item.id === object.account_id)
            const pool = object.pool_id ? pools.find((item) => item.id === object.pool_id) : undefined
            return (
              <tr key={object.id} className="border-b border-gray-100 last:border-b-0 dark:border-gray-700/50">
                <td className="px-3 py-2">
                  <div className="font-medium text-gray-900 dark:text-gray-100">{object.name}</div>
                  <div className="text-xs text-gray-500 dark:text-gray-400">{object.object_type} · {object.provider_resource_id || `object:${object.id}`}</div>
                </td>
                <td className="px-3 py-2 font-mono text-gray-700 dark:text-gray-300">{object.cidr || object.ip_address || '-'}</td>
                <td className="px-3 py-2 text-gray-600 dark:text-gray-400">{object.provider || '-'}</td>
                <td className="px-3 py-2 text-gray-600 dark:text-gray-400">{account?.name || object.account_id}</td>
                <td className="px-3 py-2 text-gray-600 dark:text-gray-400">{object.region || '-'}</td>
                <td className="px-3 py-2"><StatusBadge label={object.state} /></td>
                <td className="px-3 py-2 text-gray-600 dark:text-gray-400">{object.parent_object_id ?? '-'}</td>
                <td className="px-3 py-2 text-gray-600 dark:text-gray-400">{pool ? `${pool.name} (${pool.id})` : object.pool_id ?? '-'}</td>
                <td className="px-3 py-2 font-mono text-xs text-gray-500 dark:text-gray-400">{object.source_discovered_id || '-'}</td>
                <td className="px-3 py-2 text-gray-600 dark:text-gray-400">{formatTimeAgo(object.updated_at)}</td>
                <td className="px-3 py-2">
                  <button
                    type="button"
                    onClick={() => onShowRelationships(object)}
                    className="inline-flex items-center gap-1 rounded border border-gray-300 px-2 py-1 text-xs text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                  >
                    <GitBranch className="h-3.5 w-3.5" />
                    Relationships
                  </button>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

export function NetworkRelationshipTable({
  relationships,
  loading,
  onResolve,
}: {
  relationships: NetworkRelationship[]
  loading: boolean
  onResolve: (relationship: NetworkRelationship, resolutionState: string, reason: string) => Promise<void>
}) {
  const [resolutionStateByID, setResolutionStateByID] = useState<Record<string, string>>({})
  const [resolutionReasonByID, setResolutionReasonByID] = useState<Record<string, string>>({})
  const [resolvingByID, setResolvingByID] = useState<Record<string, boolean>>({})
  const [resolutionErrorByID, setResolutionErrorByID] = useState<Record<string, string>>({})

  async function applyResolution(relationship: NetworkRelationship) {
    const nextState = resolutionStateByID[relationship.id] || relationship.resolution_state || 'open'
    const reason = resolutionReasonByID[relationship.id] || ''
    setResolvingByID((current) => ({ ...current, [relationship.id]: true }))
    setResolutionErrorByID((current) => {
      const next = { ...current }
      delete next[relationship.id]
      return next
    })
    try {
      await onResolve(relationship, nextState, reason)
    } catch (err) {
      setResolutionErrorByID((current) => ({
        ...current,
        [relationship.id]: err instanceof Error ? err.message : 'Relationship resolution failed',
      }))
    } finally {
      setResolvingByID((current) => ({ ...current, [relationship.id]: false }))
    }
  }

  if (loading) return <div className="py-8 text-center text-gray-500 dark:text-gray-400">Loading...</div>
  return (
    <div className="overflow-x-auto rounded border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-200 bg-gray-50 text-left dark:border-gray-700 dark:bg-gray-900">
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Relationship</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Source</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Target</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Confidence</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Reason / Evidence</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Resolution</th>
            <th className="px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Updated</th>
          </tr>
        </thead>
        <tbody>
          {relationships.length === 0 && (
            <tr>
              <td colSpan={7} className="px-3 py-8 text-center text-gray-500 dark:text-gray-400">No network relationships found.</td>
            </tr>
          )}
          {relationships.map((relationship) => {
            const resolving = resolvingByID[relationship.id] || false
            return (
              <tr key={relationship.id} className="border-b border-gray-100 align-top last:border-b-0 dark:border-gray-700/50">
                <td className="px-3 py-2">
                  <div className="flex items-center gap-2">
                    <GitBranch className="h-4 w-4 text-blue-600" />
                    <div>
                      <div className="font-medium text-gray-900 dark:text-gray-100">{relationship.type.replace(/_/g, ' ')}</div>
                      <div className="font-mono text-xs text-gray-500 dark:text-gray-400">{relationship.id}</div>
                    </div>
                  </div>
                </td>
                <td className="px-3 py-2">
                  <EntityRef kind={relationship.source_kind} id={relationship.source_id} />
                </td>
                <td className="px-3 py-2">
                  <EntityRef kind={relationship.target_kind} id={relationship.target_id} />
                </td>
                <td className="px-3 py-2 text-gray-600 dark:text-gray-400">{Math.round(relationship.confidence * 100)}%</td>
                <td className="max-w-md px-3 py-2 text-gray-600 dark:text-gray-400">
                  <div>{relationship.reason || '-'}</div>
                  {(relationship.evidence ?? []).length > 0 && (
                    <div className="mt-1 space-y-0.5 font-mono text-xs text-gray-500 dark:text-gray-500">
                      {relationship.evidence!.slice(0, 3).map((item) => <div key={item}>{item}</div>)}
                    </div>
                  )}
                </td>
                <td className="px-3 py-2">
                  <div className="flex min-w-[280px] flex-wrap gap-2">
                    <select
                      aria-label={`Resolution for ${relationship.id}`}
                      value={resolutionStateByID[relationship.id] || relationship.resolution_state || 'open'}
                      onChange={(e) => setResolutionStateByID((current) => ({ ...current, [relationship.id]: e.target.value }))}
                      disabled={resolving}
                      className="rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-900 disabled:opacity-50 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                    >
                      <option value="open">Open</option>
                      <option value="resolved">Resolved</option>
                      <option value="ignored">Ignored</option>
                    </select>
                    <input
                      value={resolutionReasonByID[relationship.id] || ''}
                      onChange={(e) => setResolutionReasonByID((current) => ({ ...current, [relationship.id]: e.target.value }))}
                      placeholder="Reason"
                      disabled={resolving}
                      className="min-w-[120px] flex-1 rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-900 disabled:opacity-50 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                    />
                    <button
                      type="button"
                      aria-label={`Apply relationship ${relationship.id}`}
                      disabled={resolving}
                      onClick={() => void applyResolution(relationship)}
                      className="inline-flex items-center gap-1 rounded bg-blue-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {resolving && <Loader2 className="h-3 w-3 animate-spin" />}
                      Apply
                    </button>
                  </div>
                  {resolutionErrorByID[relationship.id] && (
                    <div className="mt-1 text-xs text-red-600 dark:text-red-400">{resolutionErrorByID[relationship.id]}</div>
                  )}
                </td>
                <td className="px-3 py-2 text-gray-600 dark:text-gray-400">{formatTimeAgo(relationship.updated_at)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function EntityRef({ kind, id }: { kind: string; id: string }) {
  return (
    <div>
      <div className="text-xs uppercase text-gray-500 dark:text-gray-400">{kind}</div>
      <div className="font-mono text-xs text-gray-700 dark:text-gray-300">{id}</div>
    </div>
  )
}

function ConflictDetailSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="border-t border-gray-100 pt-3 dark:border-gray-700">
      <div className="text-xs font-semibold uppercase text-gray-500 dark:text-gray-400">{title}</div>
      <div className="mt-2 space-y-1">{children}</div>
    </div>
  )
}

function DetailList({ label, values }: { label: string; values: Array<string | number> }) {
  const filtered = values.filter((value) => value !== '' && value != null).map((value) => String(value))
  return (
    <div className="text-xs text-gray-500 dark:text-gray-400">
      <span className="font-medium text-gray-700 dark:text-gray-300">{label}: </span>
      {filtered.length > 0 ? (
        <span className="font-mono">{filtered.join(', ')}</span>
      ) : (
        <span>-</span>
      )}
    </div>
  )
}

function evidenceValues(evidence: string[] | undefined, keys: string[]) {
  return (evidence ?? []).filter((line) => keys.some((key) => line.startsWith(`${key}=`) || line.includes(`${key}=`)))
}

const DEFAULT_CONFLICT_DECISIONS = ['skip', 'ignore', 'defer'] as const

type ActionAvailability = {
  enabled: boolean
  reason: string
}

function conflictNavigationQuery(conflict: NetworkConflict) {
  return conflict.cidr || conflict.discovered_ids?.[0] || ''
}

function conflictActionAvailability(
  conflict: NetworkConflict,
  candidatePools: Pool[],
): {
  link: ActionAvailability
  import: ActionAvailability
  placeholder: ActionAvailability
} {
  const hasDiscoveredIDs = (conflict.discovered_ids?.length ?? 0) > 0
  const hasCandidatePools = candidatePools.length > 0
  return {
    link: {
      enabled: hasDiscoveredIDs && hasCandidatePools,
      reason: !hasDiscoveredIDs
        ? 'Linking needs an affected discovered resource.'
        : !hasCandidatePools
          ? 'Linking needs a candidate pool from this conflict or the loaded pool list.'
          : 'A discovered resource and candidate pool are available.',
    },
    import: {
      enabled: hasDiscoveredIDs,
      reason: hasDiscoveredIDs
        ? 'Affected discovered resources can be previewed before import.'
        : 'Import needs at least one affected discovered resource.',
    },
    placeholder: {
      enabled: conflict.type === 'missing_parent' && hasDiscoveredIDs,
      reason: conflict.type !== 'missing_parent'
        ? 'Placeholder parents are only available for missing-parent conflicts.'
        : hasDiscoveredIDs
          ? 'A missing parent can be represented as a placeholder object.'
          : 'Placeholder parent creation needs an affected discovered resource.',
    },
  }
}

export function importApplyResultQuery(result: DiscoveryImportApplyResponse) {
  const affected = new Set(result.summary.affected_resource_ids ?? result.linked_resource_ids ?? [])
  const affectedItem = result.preview.items.find((item) => affected.has(item.resource_id))
  const fallbackItem = result.preview.items.find((item) => item.status === 'importable') ?? result.preview.items[0]
  const item = affectedItem ?? fallbackItem
  if (item?.provider_resource_id) return item.provider_resource_id
  if (item?.name) return item.name
  if (item?.cidr) return item.cidr
  const createdPoolID = result.summary.created_pool_ids?.[0] ?? result.created_pool_ids?.[0]
  return createdPoolID ? String(createdPoolID) : ''
}

export function ImportApplySummaryPanel({
  result,
  onViewAffected,
  onDismiss,
}: {
  result: DiscoveryImportApplyResponse
  onViewAffected?: () => void
  onDismiss?: () => void
}) {
  const summary = result.summary
  const canViewAffected = importApplyResultQuery(result) !== ''
  const countCards = [
    { label: 'Imported', value: summary.imported },
    { label: 'Linked-only', value: summary.linked_only },
    { label: 'Skipped', value: summary.skipped },
    { label: 'Blocked', value: summary.blocked },
    { label: 'Conflicts', value: summary.conflicts },
    { label: 'Created records', value: summary.created_records },
    { label: 'Linked records', value: summary.linked_records },
  ]

  return (
    <section className="rounded border border-green-200 bg-green-50 p-4 text-sm dark:border-green-800 dark:bg-green-900/20">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-green-950 dark:text-green-100">Import apply summary</h2>
          <p className="mt-1 text-xs text-green-800 dark:text-green-300">
            {summary.created_records} records created, {summary.linked_records} records linked, {summary.skipped} skipped
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {onViewAffected && (
            <button
              type="button"
              onClick={onViewAffected}
              disabled={!canViewAffected}
              className="inline-flex items-center gap-1 rounded border border-green-300 px-2.5 py-1.5 text-xs font-medium text-green-900 hover:bg-green-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-green-700 dark:text-green-200 dark:hover:bg-green-900/40"
            >
              <ArrowRight className="h-3.5 w-3.5" />
              View affected resources
            </button>
          )}
          {onDismiss && (
            <button
              type="button"
              onClick={onDismiss}
              className="rounded p-1.5 text-green-700 hover:bg-green-100 hover:text-green-950 dark:text-green-300 dark:hover:bg-green-900/40 dark:hover:text-green-100"
              title="Dismiss summary"
            >
              <X className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>

      <div className="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-7">
        {countCards.map((card) => (
          <div key={card.label} className="rounded border border-green-200 bg-white px-3 py-2 dark:border-green-800 dark:bg-gray-900">
            <div className="text-lg font-semibold text-gray-900 dark:text-gray-100">{card.value}</div>
            <div className="text-xs text-gray-500 dark:text-gray-400">{card.label}</div>
          </div>
        ))}
      </div>

      {result.errors.length > 0 && (
        <div className="mt-3 rounded border border-yellow-200 bg-yellow-50 p-2 text-xs text-yellow-900 dark:border-yellow-800 dark:bg-yellow-900/20 dark:text-yellow-200">
          {result.errors.length} warning{result.errors.length === 1 ? '' : 's'} while applying import
        </div>
      )}

      <div className="mt-3 overflow-hidden rounded border border-green-200 bg-white dark:border-green-800 dark:bg-gray-900">
        <table className="w-full text-xs">
          <thead>
            <tr className="border-b border-green-100 bg-green-100/60 text-left dark:border-green-800 dark:bg-green-900/30">
              <th className="px-3 py-2 font-medium text-green-950 dark:text-green-100">Resource</th>
              <th className="px-3 py-2 font-medium text-green-950 dark:text-green-100">Outcome</th>
              <th className="px-3 py-2 font-medium text-green-950 dark:text-green-100">Action</th>
              <th className="px-3 py-2 font-medium text-green-950 dark:text-green-100">Issues</th>
            </tr>
          </thead>
          <tbody>
            {result.preview.items.map((item) => (
              <tr key={item.resource_id} className="border-b border-green-50 last:border-b-0 dark:border-green-900/40">
                <td className="px-3 py-2">
                  <div className="font-medium text-gray-900 dark:text-gray-100">{item.name || item.provider_resource_id || item.resource_id}</div>
                  {item.provider_resource_id && item.name && (
                    <div className="font-mono text-[11px] text-gray-500 dark:text-gray-400">{item.provider_resource_id}</div>
                  )}
                </td>
                <td className="px-3 py-2 text-gray-700 dark:text-gray-300">{importApplyItemOutcome(result, item)}</td>
                <td className="px-3 py-2 text-gray-700 dark:text-gray-300">{importActionLabel(item)}</td>
                <td className="px-3 py-2 text-gray-500 dark:text-gray-400">{item.issues.length > 0 ? item.issues.join(', ') : '-'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function importApplyItemOutcome(result: DiscoveryImportApplyResponse, item: DiscoveryImportPreviewItem) {
  const affected = new Set(result.summary.affected_resource_ids ?? result.linked_resource_ids ?? [])
  if (affected.has(item.resource_id)) {
    if (item.proposed_action === 'link_pool') return 'Linked existing pool'
    return 'Imported'
  }
  if (item.status === 'conflict') return 'Conflict'
  if (item.status === 'blocked') return 'Blocked'
  if (item.status === 'linked_only') return 'Skipped link-only object'
  if (item.status === 'already_linked') return 'Already linked'
  if (item.status === 'not_found') return 'Not found'
  return 'Skipped'
}

function NetworkActionResultSummary({
  result,
  onViewFlat,
  onShowRelationships,
}: {
  result: NetworkConflictActionResponse
  onViewFlat: (conflict: NetworkConflict, nodeID?: string) => void
  onShowRelationships: (conflict: NetworkConflict) => void
}) {
  const relationshipCount = result.relationships?.length ?? 0
  return (
    <div className="mt-3 rounded bg-green-50 p-2 text-xs text-green-800 dark:bg-green-900/20 dark:text-green-300">
      <div className="font-semibold">
        {result.action === 'create_placeholder_parent'
          ? 'Placeholder parent ready'
          : result.action === 'import'
            ? 'Import applied'
            : result.previous_pool_id
              ? 'Resource relinked'
              : 'Resource linked'}
      </div>
      <div className="mt-1 space-y-0.5">
        {result.discovered_id && <div>Discovered resource: <span className="font-mono">{result.discovered_id}</span></div>}
        {result.previous_pool_id != null && <div>Previous pool: {result.previous_pool_id}</div>}
        {result.pool_id != null && <div>Target pool: {result.pool_id}</div>}
        {result.import && (
          <div>
            {result.import.pools_created} pools created, {result.import.resources_linked} resources linked, {result.import.skipped} skipped
          </div>
        )}
        {result.network_object && (
          <div>
            Object: {result.network_object.name} ({result.network_object.object_type}, {result.network_object.state})
          </div>
        )}
        <div>{relationshipCount} relationship{relationshipCount === 1 ? '' : 's'} recorded</div>
      </div>
      <div className="mt-2 flex flex-wrap gap-2">
        <button
          type="button"
          onClick={() => onViewFlat(result.conflict)}
          className="inline-flex items-center gap-1 rounded border border-green-300 px-2 py-1 text-xs text-green-800 hover:bg-green-100 dark:border-green-700 dark:text-green-300 dark:hover:bg-green-900/40"
        >
          <ArrowRight className="h-3.5 w-3.5" />
          View affected row
        </button>
        <button
          type="button"
          onClick={() => onShowRelationships(result.conflict)}
          className="inline-flex items-center gap-1 rounded border border-green-300 px-2 py-1 text-xs text-green-800 hover:bg-green-100 dark:border-green-700 dark:text-green-300 dark:hover:bg-green-900/40"
        >
          <GitBranch className="h-3.5 w-3.5" />
          View relationships
        </button>
      </div>
    </div>
  )
}

export function NetworkConflictList({
  conflicts,
  loading,
  selected,
  onSelect,
  onResolve,
  onLink,
  onImport,
  onPlaceholderParent,
  onPreviewImport,
  onViewFlat,
  onViewHierarchy,
  onShowRelationships,
  pools,
  accounts,
}: {
  conflicts: NetworkConflict[]
  loading: boolean
  selected: NetworkConflict | null
  onSelect: (conflict: NetworkConflict | null) => void
  onResolve: (conflict: NetworkConflict, decision: string) => void
  onLink: (conflict: NetworkConflict, discoveredId: string, poolId: number, reason: string, override: boolean) => Promise<NetworkConflictActionResponse>
  onImport: (conflict: NetworkConflict, resourceIds: string[], poolId: number | undefined, reason: string, override: boolean) => Promise<NetworkConflictActionResponse>
  onPlaceholderParent: (conflict: NetworkConflict, discoveredId: string, name: string, reason: string) => Promise<NetworkConflictActionResponse>
  onPreviewImport: (conflict: NetworkConflict, resourceIds: string[], poolId: number | undefined) => Promise<DiscoveryImportPreviewResponse>
  onViewFlat: (conflict: NetworkConflict, nodeID?: string) => void
  onViewHierarchy: (conflict: NetworkConflict, nodeID?: string) => void
  onShowRelationships: (conflict: NetworkConflict) => void
  pools: Pool[]
  accounts: Account[]
}) {
  const [actionMode, setActionMode] = useState<'link' | 'import' | 'placeholder_parent' | null>(null)
  const [linkDiscoveredID, setLinkDiscoveredID] = useState('')
  const [linkPoolID, setLinkPoolID] = useState('')
  const [importResourceIDs, setImportResourceIDs] = useState<string[]>([])
  const [importPoolID, setImportPoolID] = useState('')
  const [placeholderDiscoveredID, setPlaceholderDiscoveredID] = useState('')
  const [placeholderName, setPlaceholderName] = useState('')
  const [reason, setReason] = useState('')
  const [override, setOverride] = useState(false)
  const [preview, setPreview] = useState<DiscoveryImportPreviewResponse | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [actionResult, setActionResult] = useState<NetworkConflictActionResponse | null>(null)

  useEffect(() => {
    setActionMode(null)
    setReason('')
    setOverride(false)
    setPreview(null)
    setActionError(null)
    setActionResult(null)
    setLinkDiscoveredID(selected?.discovered_ids?.[0] ?? '')
    setLinkPoolID(selected?.pool_ids?.[0] ? String(selected.pool_ids[0]) : '')
    setImportResourceIDs(selected?.discovered_ids ?? [])
    setImportPoolID('')
    setPlaceholderDiscoveredID(selected?.discovered_ids?.[0] ?? '')
    setPlaceholderName('')
  }, [selected?.id])

  if (loading) return <div className="py-8 text-center text-gray-500 dark:text-gray-400">Loading...</div>
  const poolOptions = pools.filter((pool) => !selected?.pool_ids?.length || selected.pool_ids.includes(pool.id))
  const allPoolOptions = poolOptions.length > 0 ? poolOptions : pools
  const canCreatePlaceholderParent = selected?.type === 'missing_parent' && (selected.discovered_ids?.length ?? 0) > 0
  const isRelinkCandidate = selected?.type === 'alternate_exact_pool'
  const selectedPools = selected?.pool_ids?.map((id) => pools.find((pool) => pool.id === id) ?? null) ?? []
  const selectedAccounts = selected?.account_ids?.map((id) => accounts.find((account) => account.id === id) ?? null) ?? []
  const selectedAvailability = selected ? conflictActionAvailability(selected, allPoolOptions) : null
  const selectedNodeIDs = selected?.node_ids ?? []
  const selectedNavigationFallback = selected ? conflictNavigationQuery(selected) : ''
  const reviewDecisions = Array.from(new Set([
    ...DEFAULT_CONFLICT_DECISIONS,
    ...(selected?.available_decisions ?? []),
  ]))
  const availableDecisionSet = selected?.available_decisions ? new Set(selected.available_decisions) : null

  async function previewImport() {
    if (!selected) return
    setPreviewLoading(true)
    setActionError(null)
    try {
      const next = await onPreviewImport(
        selected,
        importResourceIDs,
        importPoolID ? Number(importPoolID) : undefined,
      )
      setPreview(next)
    } catch (err) {
      setPreview(null)
      setActionError(err instanceof Error ? err.message : 'Import preview failed')
    } finally {
      setPreviewLoading(false)
    }
  }

  async function runAction(next: () => Promise<NetworkConflictActionResponse>) {
    setActionError(null)
    try {
      setActionResult(await next())
    } catch (err) {
      setActionResult(null)
      setActionError(err instanceof Error ? err.message : 'Action failed')
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_360px]">
      <div className="overflow-hidden rounded border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
        {conflicts.length === 0 ? (
          <div className="px-4 py-8 text-center text-gray-500 dark:text-gray-400">No network conflicts found.</div>
        ) : conflicts.map((conflict) => (
          <button
            key={conflict.id}
            type="button"
            onClick={() => onSelect(conflict)}
            className={`block w-full border-b border-gray-100 px-4 py-3 text-left last:border-b-0 hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-700/50 ${selected?.id === conflict.id ? 'bg-blue-50 dark:bg-blue-900/20' : ''}`}
          >
            <div className="flex flex-wrap items-center gap-2">
              <StatusBadge label={conflict.severity} />
              <span className="font-medium text-gray-900 dark:text-gray-100">{conflict.title}</span>
            </div>
            <div className="mt-1 text-sm text-gray-500 dark:text-gray-400">{conflict.description}</div>
          </button>
        ))}
      </div>
      <div className="rounded border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
        {selected ? (
          <div className="space-y-3">
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <div className="text-sm font-semibold text-gray-900 dark:text-gray-100">{selected.title}</div>
                <StatusBadge label={selected.status || 'open'} />
                {selected.resolution_state && <StatusBadge label={selected.resolution_state} />}
              </div>
              <div className="mt-1 text-sm text-gray-500 dark:text-gray-400">{selected.description}</div>
            </div>
            {selected.recommended_action && (
              <div className="rounded bg-blue-50 p-2 text-sm text-blue-800 dark:bg-blue-900/20 dark:text-blue-300">
                {selected.recommended_action}
              </div>
            )}
            <ConflictOperatorNote conflict={selected} />
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => onViewFlat(selected)}
                className="inline-flex items-center gap-1 rounded border border-gray-300 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
              >
                <ArrowRight className="h-3.5 w-3.5" />
                View in flat
              </button>
              <button
                type="button"
                onClick={() => onViewHierarchy(selected)}
                className="inline-flex items-center gap-1 rounded border border-gray-300 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
              >
                <ArrowRight className="h-3.5 w-3.5" />
                View in hierarchy
              </button>
              <button
                type="button"
                onClick={() => onShowRelationships(selected)}
                className="inline-flex items-center gap-1 rounded border border-gray-300 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
              >
                <GitBranch className="h-3.5 w-3.5" />
                Show relationships
              </button>
            </div>
            <ConflictDetailSection title="Affected rows">
              {selectedNodeIDs.length > 0 ? (
                <div className="space-y-2">
                  {selectedNodeIDs.map((nodeID) => (
                    <div key={nodeID} className="rounded border border-gray-200 bg-gray-50 p-2 text-xs dark:border-gray-700 dark:bg-gray-900/40">
                      <div className="font-mono text-gray-700 dark:text-gray-300">{nodeID}</div>
                      <div className="mt-2 flex flex-wrap gap-2">
                        <button
                          type="button"
                          onClick={() => onViewFlat(selected, nodeID)}
                          className="inline-flex items-center gap-1 rounded border border-gray-300 px-2 py-1 text-xs text-gray-700 hover:bg-white dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
                        >
                          <ArrowRight className="h-3.5 w-3.5" />
                          Flat row
                        </button>
                        <button
                          type="button"
                          onClick={() => onViewHierarchy(selected, nodeID)}
                          className="inline-flex items-center gap-1 rounded border border-gray-300 px-2 py-1 text-xs text-gray-700 hover:bg-white dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
                        >
                          <ArrowRight className="h-3.5 w-3.5" />
                          Hierarchy row
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              ) : selectedNavigationFallback ? (
                <div className="rounded border border-gray-200 bg-gray-50 p-2 text-xs text-gray-600 dark:border-gray-700 dark:bg-gray-900/40 dark:text-gray-300">
                  No merged row ID was returned. Navigation will search for <span className="font-mono">{selectedNavigationFallback}</span>.
                </div>
              ) : (
                <div className="text-xs text-gray-500 dark:text-gray-400">No affected merged rows were returned for this conflict.</div>
              )}
            </ConflictDetailSection>
            <ConflictDetailSection title="Affected resources">
              <DetailList label="Discovered" values={selected.discovered_ids ?? []} />
              <DetailList label="Nodes" values={selected.node_ids ?? []} />
              <DetailList label="Managed objects" values={evidenceValues(selected.evidence, ['network_object_id', 'object_id', 'placeholder_parent'])} />
            </ConflictDetailSection>
            <ConflictDetailSection title="Ownership">
              <DetailList
                label="Accounts"
                values={selectedAccounts.map((account, index) => account ? `${account.name} (${account.id})` : String(selected.account_ids?.[index]))}
              />
              <DetailList label="Provider" values={selected.provider ? [selected.provider] : []} />
              <DetailList label="Regions" values={selected.regions ?? []} />
              <DetailList label="Object types" values={selected.object_types ?? []} />
            </ConflictDetailSection>
            <ConflictDetailSection title="Pools and parent chain">
              <DetailList
                label="Pools"
                values={selectedPools.map((pool, index) => pool ? `${pool.name} (${pool.id}, ${pool.cidr})` : String(selected.pool_ids?.[index]))}
              />
              <DetailList label="Parent evidence" values={evidenceValues(selected.evidence, ['parent_resource_id', 'parent_id', 'placeholder_parent'])} />
            </ConflictDetailSection>
            <ConflictDetailSection title="CIDR/IP evidence">
              <DetailList label="Conflict CIDR" values={selected.cidr ? [selected.cidr] : []} />
              <DetailList label="Evidence" values={selected.evidence ?? []} />
            </ConflictDetailSection>
            <ConflictDetailSection title="Relationships">
              {(selected.relationships ?? []).length > 0 ? (
                <div className="space-y-1">
                  {selected.relationships!.map((relationship) => (
                    <div key={relationship.id} className="rounded bg-gray-50 p-2 text-xs text-gray-600 dark:bg-gray-900/40 dark:text-gray-300">
                      <div className="font-medium text-gray-800 dark:text-gray-200">{relationship.type.replace(/_/g, ' ')} · {relationship.resolution_state}</div>
                      <div className="font-mono">{relationship.source_kind}:{relationship.source_id} {'->'} {relationship.target_kind}:{relationship.target_id}</div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-xs text-gray-500 dark:text-gray-400">No attached relationships returned.</div>
              )}
            </ConflictDetailSection>
            <div className="border-t border-gray-100 pt-3 dark:border-gray-700">
              <div className="text-xs font-semibold uppercase text-gray-500 dark:text-gray-400">Review</div>
              <div className="mt-2 flex flex-wrap gap-2">
                {reviewDecisions.map((decision) => {
                  const disabled = availableDecisionSet !== null && !availableDecisionSet.has(decision)
                  const reason = disabled
                    ? `${decision} is unavailable because this conflict did not advertise that review decision.`
                    : `${decision} is available for this conflict.`
                  return (
                    <button
                      key={decision}
                      type="button"
                      disabled={disabled}
                      title={reason}
                      onClick={() => onResolve(selected, decision)}
                      className="rounded border border-gray-300 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                    >
                      {decision}
                    </button>
                  )
                })}
              </div>
              <div className="mt-2 space-y-1 text-xs text-gray-500 dark:text-gray-400">
                {reviewDecisions.map((decision) => {
                  const disabled = availableDecisionSet !== null && !availableDecisionSet.has(decision)
                  return (
                    <div key={decision}>
                      <span className="font-medium text-gray-700 dark:text-gray-300">{decision}:</span>{' '}
                      {disabled
                        ? 'Unavailable because the backend did not offer this decision.'
                        : 'Available as a passive review decision.'}
                    </div>
                  )
                })}
              </div>
            </div>
            <div className="border-t border-gray-100 pt-3 dark:border-gray-700">
              <div className="text-xs font-semibold uppercase text-gray-500 dark:text-gray-400">Actions</div>
              <div className="mt-2 flex flex-wrap gap-2">
                <button
                  type="button"
                  disabled={!selectedAvailability?.link.enabled}
                  title={selectedAvailability?.link.reason}
                  onClick={() => setActionMode(actionMode === 'link' ? null : 'link')}
                  className="inline-flex items-center gap-1 rounded border border-gray-300 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                >
                  <Link2 className="h-3.5 w-3.5" />
                  {isRelinkCandidate ? 'Relink to pool' : 'Link to pool'}
                </button>
                <button
                  type="button"
                  disabled={!selectedAvailability?.import.enabled}
                  title={selectedAvailability?.import.reason}
                  onClick={() => setActionMode(actionMode === 'import' ? null : 'import')}
                  className="inline-flex items-center gap-1 rounded border border-gray-300 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                >
                  <UploadCloud className="h-3.5 w-3.5" />
                  Import as pool
                </button>
                <button
                  type="button"
                  disabled={!selectedAvailability?.placeholder.enabled}
                  title={selectedAvailability?.placeholder.reason}
                  onClick={() => setActionMode(actionMode === 'placeholder_parent' ? null : 'placeholder_parent')}
                  className="inline-flex items-center gap-1 rounded border border-gray-300 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                >
                  <Plus className="h-3.5 w-3.5" />
                  Placeholder parent
                </button>
              </div>
              {selectedAvailability && (
                <div className="mt-2 space-y-1 text-xs text-gray-500 dark:text-gray-400">
                  <div><span className="font-medium text-gray-700 dark:text-gray-300">{isRelinkCandidate ? 'Relink' : 'Link'}:</span> {selectedAvailability.link.reason}</div>
                  <div><span className="font-medium text-gray-700 dark:text-gray-300">Import:</span> {selectedAvailability.import.reason}</div>
                  <div><span className="font-medium text-gray-700 dark:text-gray-300">Placeholder parent:</span> {selectedAvailability.placeholder.reason}</div>
                </div>
              )}
              {actionError && <div className="mt-2 text-xs text-red-600 dark:text-red-400">{actionError}</div>}
              {actionResult && (
                <NetworkActionResultSummary
                  result={actionResult}
                  onViewFlat={onViewFlat}
                  onShowRelationships={onShowRelationships}
                />
              )}
              {actionMode === 'link' && (
                <div className="mt-3 space-y-2">
                  {isRelinkCandidate && (
                    <div className="rounded bg-amber-50 p-2 text-xs text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
                      This updates the discovered resource pool association to the selected alternate match.
                    </div>
                  )}
                  <select
                    value={linkDiscoveredID}
                    onChange={(e) => setLinkDiscoveredID(e.target.value)}
                    className="w-full rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                  >
                    {(selected.discovered_ids ?? []).map((id) => <option key={id} value={id}>{id}</option>)}
                  </select>
                  <select
                    value={linkPoolID}
                    onChange={(e) => setLinkPoolID(e.target.value)}
                    className="w-full rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                  >
                    <option value="">Select pool</option>
                    {allPoolOptions.map((pool) => <option key={pool.id} value={pool.id}>{pool.name} ({pool.cidr})</option>)}
                  </select>
                  <input
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    placeholder="Reason"
                    className="w-full rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                  />
                  <label className="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-300">
                    <input type="checkbox" checked={override} onChange={(e) => setOverride(e.target.checked)} />
                    Override validation
                  </label>
                  <button
                    type="button"
                    disabled={!linkDiscoveredID || !linkPoolID}
                    onClick={() => selected && void runAction(() => onLink(selected, linkDiscoveredID, Number(linkPoolID), reason, override))}
                    className="rounded bg-blue-600 px-3 py-1 text-xs font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {isRelinkCandidate ? 'Confirm relink' : 'Confirm link'}
                  </button>
                </div>
              )}
              {actionMode === 'import' && (
                <div className="mt-3 space-y-2">
                  <div className="space-y-1">
                    {(selected.discovered_ids ?? []).map((id) => (
                      <label key={id} className="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-300">
                        <input
                          type="checkbox"
                          checked={importResourceIDs.includes(id)}
                          onChange={(e) => {
                            setPreview(null)
                            setImportResourceIDs((current) => e.target.checked ? [...current, id] : current.filter((value) => value !== id))
                          }}
                        />
                        <span className="font-mono">{id}</span>
                      </label>
                    ))}
                  </div>
                  <select
                    value={importPoolID}
                    onChange={(e) => {
                      setPreview(null)
                      setImportPoolID(e.target.value)
                    }}
                    className="w-full rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                  >
                    <option value="">No parent pool</option>
                    {pools.map((pool) => <option key={pool.id} value={pool.id}>{pool.name} ({pool.cidr})</option>)}
                  </select>
                  <input
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    placeholder="Reason"
                    className="w-full rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                  />
                  <label className="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-300">
                    <input type="checkbox" checked={override} onChange={(e) => setOverride(e.target.checked)} />
                    Override conflict rows
                  </label>
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      disabled={importResourceIDs.length === 0 || previewLoading}
                      onClick={() => void previewImport()}
                      className="rounded border border-gray-300 px-3 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                    >
                      {previewLoading ? 'Previewing...' : 'Preview'}
                    </button>
                    <button
                      type="button"
                      disabled={importResourceIDs.length === 0 || !preview}
                      onClick={() => selected && void runAction(() => onImport(selected, importResourceIDs, importPoolID ? Number(importPoolID) : undefined, reason, override))}
                      className="rounded bg-blue-600 px-3 py-1 text-xs font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      Apply import
                    </button>
                  </div>
                  {preview && (
                    <div className="rounded bg-gray-50 p-2 text-xs text-gray-600 dark:bg-gray-900/40 dark:text-gray-300">
                      {preview.importable} importable, {preview.blocked} blocked, {preview.conflict_count} conflicts
                    </div>
                  )}
                </div>
              )}
              {actionMode === 'placeholder_parent' && canCreatePlaceholderParent && (
                <div className="mt-3 space-y-2">
                  <select
                    value={placeholderDiscoveredID}
                    onChange={(e) => setPlaceholderDiscoveredID(e.target.value)}
                    className="w-full rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                  >
                    {(selected.discovered_ids ?? []).map((id) => <option key={id} value={id}>{id}</option>)}
                  </select>
                  <input
                    value={placeholderName}
                    onChange={(e) => setPlaceholderName(e.target.value)}
                    placeholder="Placeholder name"
                    className="w-full rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                  />
                  <input
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    placeholder="Reason"
                    className="w-full rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                  />
                  <button
                    type="button"
                    disabled={!placeholderDiscoveredID}
                    onClick={() => selected && void runAction(() => onPlaceholderParent(selected, placeholderDiscoveredID, placeholderName, reason))}
                    className="rounded bg-blue-600 px-3 py-1 text-xs font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    Create placeholder
                  </button>
                </div>
              )}
            </div>
          </div>
        ) : (
          <div className="text-sm text-gray-500 dark:text-gray-400">Select a conflict to review evidence.</div>
        )}
      </div>
    </div>
  )
}

function ConflictOperatorNote({ conflict }: { conflict: NetworkConflict }) {
  const note = operatorNoteForConflict(conflict)
  return (
    <div className="rounded border border-gray-200 bg-gray-50 p-2 text-xs text-gray-700 dark:border-gray-700 dark:bg-gray-900/40 dark:text-gray-300">
      <div className="font-semibold text-gray-800 dark:text-gray-100">{note.title}</div>
      <div className="mt-1">{note.body}</div>
    </div>
  )
}

function operatorNoteForConflict(conflict: NetworkConflict) {
  switch (conflict.type) {
    case 'unlinked_exact_pool':
      return {
        title: 'Operator note: link candidate',
        body: 'Use Link to pool when the managed pool is the authoritative record. Use Import as pool only if this should be tracked as a separate discovered-source pool.',
      }
    case 'alternate_exact_pool':
      return {
        title: 'Operator note: merge candidate',
        body: 'Use Relink to pool to move the discovery association to the exact matching pool. This is the normal merge action for a wrong soft link.',
      }
    case 'linked_pool_mismatch':
      return {
        title: 'Operator note: link mismatch',
        body: 'Check whether cloud CIDR or IPAM intent is authoritative. Relink to the right pool, import the cloud object as a new pool, or ignore intentional drift.',
      }
    case 'outside_pool':
      return {
        title: 'Operator note: outside pool',
        body: 'Treat this as stronger drift than a simple mismatch. Verify the parent pool first, then override only when the placement is intentionally outside the modeled hierarchy.',
      }
    case 'duplicate_cidr':
      return {
        title: 'Operator note: duplicate address space',
        body: 'Repeated CIDRs can be normal when they cross the selected policy boundary. Mark expected reuse ignored or defer it unless two records should point at the same managed pool.',
      }
    case 'missing_parent':
      return {
        title: 'Operator note: missing parent',
        body: 'Rediscover first if the parent should exist. Use Placeholder parent when the child must be tracked before the parent VPC/network is available.',
      }
    case 'invalid_nesting':
      return {
        title: 'Operator note: invalid nesting',
        body: 'Do not import until the parent-child relationship is corrected or the provider data is verified. This usually indicates a bad parent reference or stale discovery.',
      }
    case 'managed_overlap':
      return {
        title: 'Operator note: managed overlap',
        body: 'Decide which pool owns the address space. Import separately only when the overlap is intentional and the operator model allows it.',
      }
    case 'invalid_cidr':
      return {
        title: 'Operator note: invalid CIDR',
        body: 'Skip this item until discovery sends a valid CIDR. Import and link actions should wait for corrected provider data.',
      }
    default:
      return {
        title: 'Operator note: review evidence',
        body: 'Use the evidence and relationships to decide whether to link, import, ignore expected state, or defer for later investigation.',
      }
  }
}

function NetworkObjectIcon({ node }: { node: NetworkNode }) {
  const color = node.kind === 'pool' ? 'text-emerald-600' : node.object_type === 'elastic_ip' ? 'text-orange-500' : 'text-blue-600'
  return node.kind === 'pool' ? <Table2 className={`h-4 w-4 ${color}`} /> : <Network className={`h-4 w-4 ${color}`} />
}

function NetworkIssueBadges({ issues }: { issues: NetworkIssue[] }) {
  if (issues.length === 0) return null
  const grouped = groupedIssueBadges(issues)
  return (
    <div className="mt-1 flex flex-wrap gap-1">
      {grouped.map((issue) => (
        <span key={issue.type} className="rounded bg-yellow-50 px-1.5 py-0.5 text-[11px] text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300">
          {networkIssueLabel(issue.type)}
          {issue.count > 1 && <span className="ml-1 font-semibold">x{issue.count}</span>}
        </span>
      ))}
    </div>
  )
}

function groupedIssueBadges(issues: NetworkIssue[]) {
  const counts = new Map<string, number>()
  for (const issue of issues) {
    counts.set(issue.type, (counts.get(issue.type) ?? 0) + 1)
  }
  return Array.from(counts.entries())
    .map(([type, count]) => ({ type, count }))
    .sort((a, b) => issueSortRank(a.type) - issueSortRank(b.type) || a.type.localeCompare(b.type))
}

function issueSortRank(type: string) {
  switch (type) {
    case 'invalid_cidr':
    case 'invalid_nesting':
    case 'outside_pool':
      return 0
    case 'linked_pool_mismatch':
    case 'alternate_exact_pool':
    case 'unlinked_exact_pool':
      return 1
    case 'missing_parent':
    case 'managed_overlap':
      return 2
    case 'duplicate_cidr':
      return 3
    default:
      return 4
  }
}

function networkIssueLabel(type: string) {
  switch (type) {
    case 'unlinked_exact_pool':
      return 'link candidate'
    case 'alternate_exact_pool':
      return 'relink candidate'
    case 'linked_pool_mismatch':
      return 'link mismatch'
    case 'duplicate_cidr':
      return 'duplicate CIDR'
    case 'missing_parent':
      return 'missing parent'
    case 'managed_overlap':
      return 'managed overlap'
    case 'invalid_nesting':
      return 'invalid nesting'
    case 'outside_pool':
      return 'outside pool'
    case 'invalid_cidr':
      return 'invalid CIDR'
    default:
      return type.replace(/_/g, ' ')
  }
}

function RelationshipBadges({ relationships }: { relationships: NetworkRelationship[] }) {
  if (relationships.length === 0) return null
  return (
    <div className="mt-1 flex flex-wrap gap-1">
      {relationships.slice(0, 4).map((relationship) => (
        <span key={relationship.id} className="inline-flex items-center gap-1 rounded bg-blue-50 px-1.5 py-0.5 text-[11px] text-blue-800 dark:bg-blue-900/30 dark:text-blue-300">
          <GitBranch className="h-3 w-3" />
          {relationship.type.replace(/_/g, ' ')}
        </span>
      ))}
      {relationships.length > 4 && (
        <span className="rounded bg-gray-100 px-1.5 py-0.5 text-[11px] text-gray-600 dark:bg-gray-700 dark:text-gray-300">
          +{relationships.length - 4}
        </span>
      )}
    </div>
  )
}

export function ResourcesTab({
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
  accounts,
  selectedAccountId,
  pools,
  selectedResourceIds,
  onToggleSelection,
  onSetVisibleSelection,
  onClearSelection,
  onBulkLink,
  bulkLinking = false,
  total,
  page,
  pageSize,
  onPageChange,
  onPageSizeChange,
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
  accounts: Account[]
  selectedAccountId: number | null
  pools: Pool[]
  selectedResourceIds: string[]
  onToggleSelection: (r: DiscoveredResource) => void
  onSetVisibleSelection: (resources: DiscoveredResource[], selected: boolean) => void
  onClearSelection: () => void
  onBulkLink: (resourceIds: string[], poolId: number) => void
  bulkLinking?: boolean
  total: number
  page: number
  pageSize: number
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
}) {
  const [bulkLinkPoolId, setBulkLinkPoolId] = useState('')
  const [showColumns, setShowColumns] = useState(false)
  const [visibleColumnKeys, setVisibleColumnKeys] = useState<ResourceColumnKey[]>(DEFAULT_RESOURCE_COLUMNS)
  const bulkLinkPool = Number(bulkLinkPoolId)
  const visibleColumnSet = useMemo(() => new Set(visibleColumnKeys), [visibleColumnKeys])
  const bulkLinkPools = useMemo(
    () => pools.filter((pool) => pool.account_id == null || pool.account_id === selectedAccountId),
    [pools, selectedAccountId],
  )
  const selectableResources = resources.filter(isSelectableDiscoveryResource)
  const selectableVisibleIds = selectableResources.map((resource) => resource.id)
  const selectedVisibleResources = resources.filter((resource) => selectedResourceIds.includes(resource.id))
  const selectedStaleResources = selectedVisibleResources.filter((resource) => resource.status !== 'active')
  const staleSelectionBlocksLink = selectedStaleResources.length > 0
  const selectedVisibleCount = selectableVisibleIds.filter((id) => selectedResourceIds.includes(id)).length
  const allVisibleSelected = selectableVisibleIds.length > 0 && selectedVisibleCount === selectableVisibleIds.length
  const someVisibleSelected = selectedVisibleCount > 0 && selectedVisibleCount < selectableVisibleIds.length
  const pageCount = Math.max(1, Math.ceil(total / pageSize))
  const boundedPage = Math.min(Math.max(page, 1), pageCount)
  const pageStart = total === 0 ? 0 : (boundedPage - 1) * pageSize + 1
  const pageEnd = Math.min(total, boundedPage * pageSize)

  function toggleColumn(key: ResourceColumnKey) {
    setVisibleColumnKeys((current) => {
      if (current.includes(key)) {
        return current.length === 1 ? current : current.filter((columnKey) => columnKey !== key)
      }
      const next = new Set([...current, key])
      return RESOURCE_COLUMNS
        .map((column) => column.key)
        .filter((columnKey) => next.has(columnKey))
    })
  }

  function toggleVisibleSelection() {
    onSetVisibleSelection(resources, !allVisibleSelected)
  }

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
        <div className="flex items-center gap-2 text-sm">
          <label className="flex items-center gap-2 rounded border border-gray-300 px-3 py-1.5 text-gray-700 dark:border-gray-600 dark:text-gray-200 md:hidden">
            <SelectAllCheckbox
              checked={allVisibleSelected}
              indeterminate={someVisibleSelected}
              disabled={selectableVisibleIds.length === 0}
              onChange={toggleVisibleSelection}
              label="Select all visible resources on this page"
            />
            Select visible
          </label>
          <button
            type="button"
            onClick={onClearSelection}
            disabled={selectedResourceIds.length === 0}
            className="rounded border border-gray-300 px-3 py-1.5 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
          >
            Clear
          </button>
          <span className="text-gray-500 dark:text-gray-400">
            {selectedResourceIds.length} selected
          </span>
        </div>
        <div className="relative">
          <button
            type="button"
            onClick={() => setShowColumns((open) => !open)}
            className="inline-flex items-center gap-2 rounded border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
            aria-expanded={showColumns}
          >
            <Table2 className="h-4 w-4" />
            Columns
          </button>
          {showColumns && (
            <div className="absolute right-0 z-20 mt-2 w-56 rounded border border-gray-200 bg-white p-2 shadow-lg dark:border-gray-700 dark:bg-gray-900">
              {RESOURCE_COLUMNS.map((column) => (
                <label
                  key={column.key}
                  className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-gray-800"
                >
                  <input
                    type="checkbox"
                    checked={visibleColumnSet.has(column.key)}
                    onChange={() => toggleColumn(column.key)}
                    className="h-4 w-4 rounded border-gray-300 text-blue-600"
                  />
                  {column.label}
                </label>
              ))}
            </div>
          )}
        </div>
        <div className="flex min-w-[280px] flex-wrap items-center gap-2 text-sm">
          <select
            value={bulkLinkPoolId}
            onChange={(e) => setBulkLinkPoolId(e.target.value)}
            disabled={bulkLinkPools.length === 0}
            aria-label="Pool for selected resources"
            className="min-w-[190px] rounded border border-gray-300 bg-white px-3 py-1.5 text-gray-900 disabled:opacity-50 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
          >
            <option value="">Select pool</option>
            {bulkLinkPools.map((pool) => (
              <option key={pool.id} value={pool.id}>
                {pool.name} ({pool.cidr})
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={() => bulkLinkPool > 0 && onBulkLink(selectedResourceIds, bulkLinkPool)}
            disabled={bulkLinking || selectedResourceIds.length === 0 || bulkLinkPool <= 0 || staleSelectionBlocksLink}
            title={staleSelectionBlocksLink ? 'Stale resources require fresh discovery before linking' : undefined}
            className="inline-flex items-center gap-1.5 rounded bg-blue-600 px-3 py-1.5 text-white hover:bg-blue-700 disabled:opacity-50"
          >
            {bulkLinking ? <Loader2 className="h-4 w-4 animate-spin" /> : <Link2 className="h-4 w-4" />}
            Link selected
          </button>
          {staleSelectionBlocksLink && (
            <div className="basis-full text-xs text-yellow-700 dark:text-yellow-300">
              {selectedStaleResources.length} stale selected; run discovery again before linking.
            </div>
          )}
        </div>
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
        <>
        <div className="hidden md:block bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900">
                <th className="w-10 px-4 py-2 text-left">
                  <SelectAllCheckbox
                    checked={allVisibleSelected}
                    indeterminate={someVisibleSelected}
                    disabled={selectableVisibleIds.length === 0}
                    onChange={toggleVisibleSelection}
                  />
                </th>
                {RESOURCE_COLUMNS.filter((column) => visibleColumnSet.has(column.key)).map((column) => (
                  <th key={column.key} className="px-4 py-2 text-left text-gray-600 dark:text-gray-400 font-medium">
                    {column.label}
                  </th>
                ))}
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
                    <input
                      type="checkbox"
                      checked={selectedResourceIds.includes(r.id)}
                      disabled={!isSelectableDiscoveryResource(r)}
                      onChange={() => onToggleSelection(r)}
                      aria-label={`Select ${r.name || r.resource_id}`}
                      className="h-4 w-4 rounded border-gray-300 text-blue-600 disabled:opacity-40"
                    />
                  </td>
                  {RESOURCE_COLUMNS.filter((column) => visibleColumnSet.has(column.key)).map((column) => (
                    <ResourceTableCell
                      key={column.key}
                      column={column.key}
                      resource={r}
                      accounts={accounts}
                      pools={pools}
                    />
                  ))}
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
        <div className="grid gap-3 md:hidden">
          {resources.map((r) => (
            <ResourceCard
              key={r.id}
              resource={r}
              pools={pools}
              onLink={onLink}
              onUnlink={onUnlink}
              selected={selectedResourceIds.includes(r.id)}
              selectable={isSelectableDiscoveryResource(r)}
              onToggleSelection={onToggleSelection}
              accountLabel={resourceAccountLabel(r, accounts)}
            />
          ))}
        </div>
        <ResourcePagination
          total={total}
          pageSize={pageSize}
          pageStart={pageStart}
          pageEnd={pageEnd}
          pageCount={pageCount}
          boundedPage={boundedPage}
          onPageChange={onPageChange}
          onPageSizeChange={onPageSizeChange}
        />
        </>
      )}
    </>
  )
}

function SelectAllCheckbox({
  checked,
  indeterminate,
  disabled,
  onChange,
  label = 'Select all visible resources',
}: {
  checked: boolean
  indeterminate: boolean
  disabled: boolean
  onChange: () => void
  label?: string
}) {
  const ref = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (ref.current) {
      ref.current.indeterminate = indeterminate
    }
  }, [indeterminate])

  return (
    <input
      ref={ref}
      type="checkbox"
      checked={checked}
      disabled={disabled}
      onChange={onChange}
      aria-label={label}
      className="h-4 w-4 rounded border-gray-300 text-blue-600 disabled:opacity-40"
    />
  )
}

function ResourceTableCell({
  column,
  resource,
  accounts,
  pools,
}: {
  column: ResourceColumnKey
  resource: DiscoveredResource
  accounts: Account[]
  pools: Pool[]
}) {
  switch (column) {
    case 'type':
      return (
        <td className="px-4 py-2">
          <ResourceTypeBadge type={resource.resource_type} />
        </td>
      )
    case 'name':
      return (
        <td className="px-4 py-2">
          <div className="text-gray-900 dark:text-gray-100 font-medium">
            {resource.name || resource.resource_id}
          </div>
          {resource.name && (
            <div className="text-xs text-gray-500 dark:text-gray-400">
              {resource.resource_id}
            </div>
          )}
        </td>
      )
    case 'account':
      return (
        <td className="px-4 py-2 text-gray-600 dark:text-gray-400">
          <ResourceAccountLabel resource={resource} accounts={accounts} />
        </td>
      )
    case 'cidr':
      return (
        <td className="px-4 py-2 font-mono text-gray-700 dark:text-gray-300">
          {resource.cidr || '-'}
        </td>
      )
    case 'region':
      return (
        <td className="px-4 py-2 text-gray-600 dark:text-gray-400">
          {resource.region || '-'}
        </td>
      )
    case 'status':
      return (
        <td className="px-4 py-2">
          <StatusBadge label={resource.status} />
        </td>
      )
    case 'pool':
      return (
        <td className="px-4 py-2 text-gray-600 dark:text-gray-400">
          {resource.pool_id ? (
            <PoolLabel poolId={resource.pool_id} pools={pools} />
          ) : (
            <span className="text-gray-400 dark:text-gray-500">
              unlinked
            </span>
          )}
        </td>
      )
    case 'last_seen':
      return (
        <td className="px-4 py-2 text-gray-500 dark:text-gray-400">
          {formatTimeAgo(resource.last_seen_at)}
        </td>
      )
  }
}

function ResourceAccountLabel({ resource, accounts }: { resource: DiscoveredResource; accounts: Account[] }) {
  const account = accounts.find((candidate) => candidate.id === resource.account_id)
  const label = resourceAccountLabel(resource, accounts)
  const detail = account?.external_id || account?.key || account?.provider || resource.provider

  return (
    <div>
      <div className="font-medium text-gray-800 dark:text-gray-200">{label}</div>
      {detail && detail !== label && (
        <div className="text-xs text-gray-500 dark:text-gray-400">{detail}</div>
      )}
    </div>
  )
}

function resourceAccountLabel(resource: DiscoveredResource, accounts: Account[]) {
  const account = accounts.find((candidate) => candidate.id === resource.account_id)
  if (account?.name) return account.name
  if (account?.key) return account.key
  return `Account #${resource.account_id}`
}

function ResourcePagination({
  total,
  pageSize,
  pageStart,
  pageEnd,
  pageCount,
  boundedPage,
  onPageChange,
  onPageSizeChange,
}: {
  total: number
  pageSize: number
  pageStart: number
  pageEnd: number
  pageCount: number
  boundedPage: number
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
}) {
  return (
    <div className="mt-3 flex flex-col gap-3 text-sm text-gray-600 dark:text-gray-400 sm:flex-row sm:items-center sm:justify-between">
      <div>
        Showing {pageStart}-{pageEnd} of {total}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <label className="flex items-center gap-2">
          <span>Rows</span>
          <select
            value={pageSize}
            onChange={(event) => onPageSizeChange(Number(event.target.value))}
            aria-label="Rows per page"
            className="rounded border border-gray-300 bg-white px-2 py-1 text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
          >
            {[10, 25, 50, 100].map((size) => (
              <option key={size} value={size}>{size}</option>
            ))}
          </select>
        </label>
        <span>
          Page {boundedPage} of {pageCount}
        </span>
        <button
          type="button"
          onClick={() => onPageChange(Math.max(1, boundedPage - 1))}
          disabled={boundedPage <= 1}
          className="rounded border border-gray-300 px-3 py-1 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
        >
          Previous
        </button>
        <button
          type="button"
          onClick={() => onPageChange(Math.min(pageCount, boundedPage + 1))}
          disabled={boundedPage >= pageCount}
          className="rounded border border-gray-300 px-3 py-1 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
        >
          Next
        </button>
      </div>
    </div>
  )
}

function PoolLabel({ poolId, pools }: { poolId: number; pools: Pool[] }) {
  const pool = pools.find((p) => p.id === poolId)
  if (!pool) {
    return <span className="text-blue-600 dark:text-blue-400">Pool #{poolId}</span>
  }
  return (
    <span className="text-blue-600 dark:text-blue-400">
      {pool.name} <span className="font-mono text-xs text-gray-500 dark:text-gray-400">{pool.cidr}</span>
    </span>
  )
}

export function ImportPreviewModal({
  preview,
  pools,
  loading,
  onClose,
  onApply,
}: {
  preview: DiscoveryImportPreviewResponse
  pools: Pool[]
  loading: boolean
  onClose: () => void
  onApply: () => void
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-end bg-black/35 p-0 sm:items-center sm:justify-center sm:p-4">
      <div className="max-h-[92vh] w-full overflow-auto rounded-t-lg bg-white shadow-xl dark:bg-gray-900 sm:max-w-5xl sm:rounded-lg">
        <div className="sticky top-0 z-10 flex items-start justify-between gap-4 border-b border-gray-200 bg-white px-4 py-3 dark:border-gray-700 dark:bg-gray-900">
          <div>
            <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">Import preview</h2>
            <div className="mt-1 flex flex-wrap gap-2 text-xs text-gray-500 dark:text-gray-400">
              <span>{preview.importable} importable</span>
              <span>{preview.conflict_count} conflicts</span>
              <span>{preview.blocked} blocked</span>
              <span>{preview.linked_only} link-only</span>
            </div>
          </div>
          <button
            onClick={onClose}
            className="rounded p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-gray-800 dark:hover:text-gray-200"
            title="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="p-4">
          <div className="hidden overflow-hidden rounded border border-gray-200 dark:border-gray-700 md:block">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-800">
                  <th className="px-3 py-2 text-left font-medium text-gray-600 dark:text-gray-400">Resource</th>
                  <th className="px-3 py-2 text-left font-medium text-gray-600 dark:text-gray-400">CIDR</th>
                  <th className="px-3 py-2 text-left font-medium text-gray-600 dark:text-gray-400">Action</th>
                  <th className="px-3 py-2 text-left font-medium text-gray-600 dark:text-gray-400">Status</th>
                  <th className="px-3 py-2 text-left font-medium text-gray-600 dark:text-gray-400">Evidence</th>
                </tr>
              </thead>
              <tbody>
                {preview.items.map((item) => (
                  <ImportPreviewRow key={item.resource_id} item={item} pools={pools} />
                ))}
              </tbody>
            </table>
          </div>

          <div className="grid gap-3 md:hidden">
            {preview.items.map((item) => (
              <ImportPreviewCard key={item.resource_id} item={item} pools={pools} />
            ))}
          </div>
        </div>

        <div className="sticky bottom-0 flex justify-end gap-2 border-t border-gray-200 bg-white px-4 py-3 dark:border-gray-700 dark:bg-gray-900">
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onApply}
            disabled={loading || preview.importable === 0}
            className="inline-flex items-center gap-2 rounded bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
          >
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <UploadCloud className="h-4 w-4" />}
            Apply import
          </button>
        </div>
      </div>
    </div>
  )
}

function ImportPreviewRow({ item, pools }: { item: DiscoveryImportPreviewItem; pools: Pool[] }) {
  return (
    <tr className="border-b border-gray-100 last:border-b-0 dark:border-gray-800">
      <td className="px-3 py-2">
        <div className="flex flex-wrap items-center gap-2">
          {item.resource_type && <ResourceTypeBadge type={item.resource_type} />}
          <span className="font-medium text-gray-900 dark:text-gray-100">{item.name || item.provider_resource_id || item.resource_id}</span>
        </div>
        {item.provider_resource_id && item.name && (
          <div className="mt-1 text-xs text-gray-500 dark:text-gray-400">{item.provider_resource_id}</div>
        )}
      </td>
      <td className="px-3 py-2 font-mono text-gray-700 dark:text-gray-300">{item.cidr || '-'}</td>
      <td className="px-3 py-2 text-gray-700 dark:text-gray-300">
        {importActionLabel(item)}
        {item.proposed_pool_id ? (
          <div className="mt-1 text-xs"><PoolLabel poolId={item.proposed_pool_id} pools={pools} /></div>
        ) : null}
      </td>
      <td className="px-3 py-2">
        <StatusBadge label={item.status} />
        <IssueBadges issues={item.issues} />
      </td>
      <td className="max-w-sm px-3 py-2 text-xs text-gray-500 dark:text-gray-400">
        {(item.evidence ?? []).slice(0, 3).join('; ') || '-'}
      </td>
    </tr>
  )
}

function ImportPreviewCard({ item, pools }: { item: DiscoveryImportPreviewItem; pools: Pool[] }) {
  return (
    <div className="rounded border border-gray-200 p-3 text-sm dark:border-gray-700">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        {item.resource_type && <ResourceTypeBadge type={item.resource_type} />}
        <StatusBadge label={item.status} />
      </div>
      <div className="font-medium text-gray-900 dark:text-gray-100">{item.name || item.provider_resource_id || item.resource_id}</div>
      <div className="mt-1 font-mono text-xs text-gray-500 dark:text-gray-400">{item.cidr || '-'}</div>
      <div className="mt-2 text-gray-700 dark:text-gray-300">{importActionLabel(item)}</div>
      {item.proposed_pool_id ? <div className="mt-1 text-xs"><PoolLabel poolId={item.proposed_pool_id} pools={pools} /></div> : null}
      <IssueBadges issues={item.issues} />
      {(item.evidence ?? []).length > 0 && (
        <div className="mt-2 text-xs text-gray-500 dark:text-gray-400">{item.evidence?.slice(0, 3).join('; ')}</div>
      )}
    </div>
  )
}

function IssueBadges({ issues }: { issues: string[] }) {
  if (issues.length === 0) return null
  return (
    <div className="mt-1 flex flex-wrap gap-1">
      {issues.map((issue) => (
        <span key={issue} className="rounded bg-yellow-50 px-1.5 py-0.5 text-[11px] text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300">
          {issueLabel(issue)}
        </span>
      ))}
    </div>
  )
}

function importActionLabel(item: DiscoveryImportPreviewItem) {
  if (item.proposed_action === 'create_pool') return 'Create discovered pool'
  if (item.proposed_action === 'link_pool') return 'Link existing pool'
  if (item.proposed_action === 'link_only') return 'Keep as network object'
  return 'No change'
}

function issueLabel(issue: string) {
  if (issue === 'stale_resource') return 'stale resource requires fresh discovery'
  return issue.replace(/_/g, ' ')
}

function ResourceCard({
  resource,
  pools,
  onLink,
  onUnlink,
  selected,
  selectable,
  onToggleSelection,
  accountLabel,
}: {
  resource: DiscoveredResource
  pools: Pool[]
  onLink: (r: DiscoveredResource) => void
  onUnlink: (r: DiscoveredResource) => void
  selected: boolean
  selectable: boolean
  onToggleSelection: (r: DiscoveredResource) => void
  accountLabel: string
}) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-3 text-sm dark:border-gray-700 dark:bg-gray-800">
      <div className="mb-2 flex items-start justify-between gap-3">
        <div className="flex min-w-0 gap-3">
          <input
            type="checkbox"
            checked={selected}
            disabled={!selectable}
            onChange={() => onToggleSelection(resource)}
            aria-label={`Select ${resource.name || resource.resource_id}`}
            className="mt-1 h-4 w-4 shrink-0 rounded border-gray-300 text-blue-600 disabled:opacity-40"
          />
          <div className="min-w-0">
          <div className="flex items-center gap-2">
            <ResourceTypeBadge type={resource.resource_type} />
            <StatusBadge label={resource.status} />
          </div>
          <div className="mt-2 truncate font-medium text-gray-900 dark:text-gray-100">
            {resource.name || resource.resource_id}
          </div>
          {resource.name && (
            <div className="truncate text-xs text-gray-500 dark:text-gray-400">
              {resource.resource_id}
            </div>
          )}
          </div>
        </div>
        {resource.pool_id ? (
          <button
            onClick={() => onUnlink(resource)}
            title="Unlink from pool"
            className="shrink-0 rounded p-1.5 text-gray-400 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
          >
            <Unlink className="h-4 w-4" />
          </button>
        ) : (
          <button
            onClick={() => onLink(resource)}
            title="Link to pool"
            className="shrink-0 rounded p-1.5 text-gray-400 hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400"
          >
            <Link2 className="h-4 w-4" />
          </button>
        )}
      </div>
      <div className="grid grid-cols-2 gap-2 text-xs">
        <div>
          <div className="text-gray-500 dark:text-gray-400">CIDR</div>
          <div className="font-mono text-gray-800 dark:text-gray-200">{resource.cidr || '-'}</div>
        </div>
        <div>
          <div className="text-gray-500 dark:text-gray-400">Region</div>
          <div className="text-gray-800 dark:text-gray-200">{resource.region || '-'}</div>
        </div>
        <div className="col-span-2">
          <div className="text-gray-500 dark:text-gray-400">Account / Project</div>
          <div className="text-gray-800 dark:text-gray-200">{accountLabel}</div>
        </div>
        <div className="col-span-2">
          <div className="text-gray-500 dark:text-gray-400">Pool</div>
          {resource.pool_id ? (
            <PoolLabel poolId={resource.pool_id} pools={pools} />
          ) : (
            <span className="text-gray-400 dark:text-gray-500">unlinked</span>
          )}
        </div>
      </div>
    </div>
  )
}

function ResourceLinkModal({
  resource,
  pools,
  loading,
  onClose,
  onLink,
  onCreateAndLink,
}: {
  resource: DiscoveredResource
  pools: Pool[]
  loading: boolean
  onClose: () => void
  onLink: (poolId: number) => void
  onCreateAndLink: (data: CreatePoolRequest) => void
}) {
  const candidates = useMemo(() => rankPoolCandidates(resource, pools), [resource, pools])
  const parentCandidates = useMemo(
    () => candidates.filter((c) => c.reason === 'contains'),
    [candidates],
  )
  const [selectedPoolId, setSelectedPoolId] = useState<number | null>(candidates[0]?.pool.id ?? null)
  const [mode, setMode] = useState<'link' | 'create'>(candidates.length > 0 ? 'link' : 'create')
  const [poolName, setPoolName] = useState(defaultPoolName(resource))
  const [poolType, setPoolType] = useState<CreatePoolRequest['type']>(poolTypeForResource(resource))
  const [parentId, setParentId] = useState<number | ''>(parentCandidates[0]?.pool.id ?? '')

  useEffect(() => {
    setSelectedPoolId(candidates[0]?.pool.id ?? null)
    setMode(candidates.length > 0 ? 'link' : 'create')
    setPoolName(defaultPoolName(resource))
    setPoolType(poolTypeForResource(resource))
    setParentId(parentCandidates[0]?.pool.id ?? '')
  }, [candidates, parentCandidates, resource])

  const linkablePools = candidates.length > 0 ? candidates : pools.map((pool) => ({ pool, reason: 'manual' as const, score: 0 }))
  const canCreate = Boolean(resource.cidr && (resource.resource_type === 'vpc' || resource.resource_type === 'subnet'))
  const modeButton = (active: boolean) =>
    'rounded px-3 py-1.5 ' +
    (active
      ? 'bg-white text-gray-900 shadow-sm dark:bg-gray-700 dark:text-gray-100'
      : 'text-gray-600 dark:text-gray-300')

  return (
    <div className="fixed inset-0 z-50 flex items-end bg-black/35 p-0 sm:items-center sm:justify-center sm:p-4">
      <div className="max-h-[92vh] w-full overflow-auto rounded-t-lg bg-white shadow-xl dark:bg-gray-900 sm:max-w-3xl sm:rounded-lg">
        <div className="sticky top-0 z-10 flex items-start justify-between gap-4 border-b border-gray-200 bg-white px-4 py-3 dark:border-gray-700 dark:bg-gray-900">
          <div className="min-w-0">
            <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">Link discovered resource</h2>
            <div className="mt-1 truncate text-sm text-gray-500 dark:text-gray-400">
              {resource.name || resource.resource_id} {resource.cidr ? <span className="font-mono">{resource.cidr}</span> : null}
            </div>
          </div>
          <button
            onClick={onClose}
            className="rounded p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-gray-800 dark:hover:text-gray-200"
            title="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="space-y-4 p-4">
          <div className="inline-flex rounded border border-gray-200 bg-gray-50 p-1 text-sm dark:border-gray-700 dark:bg-gray-800">
            <button type="button" onClick={() => setMode('link')} className={modeButton(mode === 'link')}>
              Link existing
            </button>
            <button
              type="button"
              onClick={() => setMode('create')}
              disabled={!canCreate}
              className={modeButton(mode === 'create') + ' disabled:cursor-not-allowed disabled:opacity-50'}
            >
              Build from discovery
            </button>
          </div>

          {mode === 'link' ? (
            <div className="space-y-3">
              <div className="rounded border border-gray-200 dark:border-gray-700">
                {linkablePools.length === 0 ? (
                  <div className="p-4 text-sm text-gray-500 dark:text-gray-400">No pools exist yet.</div>
                ) : (
                  linkablePools.slice(0, 12).map(({ pool, reason }) => (
                    <label
                      key={pool.id}
                      className="flex cursor-pointer items-start gap-3 border-b border-gray-100 p-3 last:border-b-0 hover:bg-gray-50 dark:border-gray-800 dark:hover:bg-gray-800/60"
                    >
                      <input
                        type="radio"
                        checked={selectedPoolId === pool.id}
                        onChange={() => setSelectedPoolId(pool.id)}
                        className="mt-1"
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-medium text-gray-900 dark:text-gray-100">{pool.name}</span>
                          <span className="font-mono text-xs text-gray-500 dark:text-gray-400">{pool.cidr}</span>
                          <StatusBadge label={pool.type} variant="type" />
                        </div>
                        <div className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                          {candidateReasonLabel(reason)}
                        </div>
                      </div>
                    </label>
                  ))
                )}
              </div>
              <div className="flex justify-end gap-2">
                <button
                  type="button"
                  onClick={onClose}
                  className="rounded border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  disabled={loading || !selectedPoolId}
                  onClick={() => selectedPoolId && onLink(selectedPoolId)}
                  className="inline-flex items-center gap-2 rounded bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
                >
                  {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Link2 className="h-4 w-4" />}
                  Link
                </button>
              </div>
            </div>
          ) : (
            <form
              className="space-y-3"
              onSubmit={(e) => {
                e.preventDefault()
                if (!resource.cidr) return
                onCreateAndLink({
                  name: poolName.trim(),
                  cidr: resource.cidr,
                  account_id: resource.account_id,
                  parent_id: parentId === '' ? undefined : parentId,
                  type: poolType,
                  status: 'active',
                  source: 'discovered',
                  description: 'Imported from ' + resource.provider + ' ' + resource.resource_type + ' ' + resource.resource_id + ' in ' + resource.region,
                  tags: {
                    discovery_resource_id: resource.resource_id,
                    discovery_provider: resource.provider,
                    discovery_region: resource.region,
                    discovery_type: resource.resource_type,
                  },
                })
              }}
            >
              <div className="grid gap-3 sm:grid-cols-2">
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">Name</span>
                  <input
                    required
                    value={poolName}
                    onChange={(e) => setPoolName(e.target.value)}
                    className="w-full rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                  />
                </label>
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">CIDR</span>
                  <input
                    disabled
                    value={resource.cidr || ''}
                    className="w-full rounded border border-gray-300 bg-gray-50 px-3 py-1.5 font-mono text-sm text-gray-600 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-300"
                  />
                </label>
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">Type</span>
                  <select
                    value={poolType}
                    onChange={(e) => setPoolType(e.target.value as CreatePoolRequest['type'])}
                    className="w-full rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                  >
                    <option value="vpc">VPC</option>
                    <option value="subnet">Subnet</option>
                    <option value="environment">Environment</option>
                    <option value="region">Region</option>
                    <option value="supernet">Supernet</option>
                  </select>
                </label>
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">Parent</span>
                  <select
                    value={parentId}
                    onChange={(e) => setParentId(e.target.value ? Number(e.target.value) : '')}
                    className="w-full rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                  >
                    <option value="">None</option>
                    {parentCandidates.map(({ pool }) => (
                      <option key={pool.id} value={pool.id}>
                        {pool.name} ({pool.cidr})
                      </option>
                    ))}
                  </select>
                </label>
              </div>
              <div className="flex justify-end gap-2">
                <button
                  type="button"
                  onClick={onClose}
                  className="rounded border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={loading || !canCreate || poolName.trim().length === 0}
                  className="inline-flex items-center gap-2 rounded bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
                >
                  {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
                  Create and link
                </button>
              </div>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}

type CandidateReason = 'exact' | 'contains' | 'same-account' | 'manual'

function rankPoolCandidates(resource: DiscoveredResource, pools: Pool[]): Array<{ pool: Pool; reason: CandidateReason; score: number }> {
  return pools
    .map((pool) => {
      let score = 0
      let reason: CandidateReason = 'manual'
      if (resource.cidr && cidrEqual(pool.cidr, resource.cidr)) {
        score = 100
        reason = 'exact'
      } else if (resource.cidr && cidrContains(pool.cidr, resource.cidr)) {
        score = 80 + prefixLength(pool.cidr)
        reason = 'contains'
      } else if (pool.account_id === resource.account_id) {
        score = 20
        reason = 'same-account'
      }
      if (pool.source === 'discovered') score += 5
      return { pool, reason, score }
    })
    .filter(({ score }) => score > 0)
    .sort((a, b) => b.score - a.score || a.pool.name.localeCompare(b.pool.name))
}

function candidateReasonLabel(reason: CandidateReason) {
  switch (reason) {
    case 'exact':
      return 'Exact CIDR match'
    case 'contains':
      return 'Contains this discovered CIDR'
    case 'same-account':
      return 'Same account'
    default:
      return 'Available pool'
  }
}

function defaultPoolName(resource: DiscoveredResource) {
  return (resource.name || resource.resource_id || 'discovered-pool').trim()
}

function poolTypeForResource(resource: DiscoveredResource): CreatePoolRequest['type'] {
  if (resource.resource_type === 'vpc') return 'vpc'
  if (resource.resource_type === 'subnet') return 'subnet'
  return 'subnet'
}

function cidrEqual(a: string, b: string) {
  const pa = parseIPv4CIDR(a)
  const pb = parseIPv4CIDR(b)
  return Boolean(pa && pb && pa.base === pb.base && pa.prefix === pb.prefix)
}

function cidrContains(parent: string, child: string) {
  const p = parseIPv4CIDR(parent)
  const c = parseIPv4CIDR(child)
  if (!p || !c || p.prefix > c.prefix) return false
  return (c.base & p.mask) === p.base
}

function prefixLength(cidr: string) {
  return parseIPv4CIDR(cidr)?.prefix ?? 0
}

function parseIPv4CIDR(cidr: string): { base: number; prefix: number; mask: number } | null {
  const [ip, prefixText] = cidr.split('/')
  const prefix = Number(prefixText)
  if (!ip || !Number.isInteger(prefix) || prefix < 0 || prefix > 32) return null
  const octets = ip.split('.').map((part) => Number(part))
  if (octets.length !== 4 || octets.some((n) => !Number.isInteger(n) || n < 0 || n > 255)) return null
  const raw = ((octets[0] << 24) | (octets[1] << 16) | (octets[2] << 8) | octets[3]) >>> 0
  const mask = prefix === 0 ? 0 : (0xffffffff << (32 - prefix)) >>> 0
  return { base: raw & mask, prefix, mask }
}

function SyncTab({
  jobs,
  agents,
  loading,
  error,
}: {
  jobs: SyncJob[]
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

  if (jobs.length === 0) {
    return (
      <div className="text-center py-8 text-gray-500 dark:text-gray-400">
        No sync jobs yet. Click "Sync Now" to request a connected agent scan, or fall back to server-side discovery when no agent is healthy.
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
              Source
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
              <td className="px-4 py-2 text-gray-600 dark:text-gray-400">
                {j.source === 'agent' ? (
                  agents.find((agent) => agent.id === j.agent_id)?.name || 'agent'
                ) : (
                  'server'
                )}
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
  onScan,
  onDelete,
}: {
  agents: DiscoveryAgent[]
  loading: boolean
  error: string | null
  onScan: (agentId: string) => void
  onDelete: (agent: DiscoveryAgent) => void
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
CLOUDPAM_AGENT_ID_FILE=/var/lib/cloudpam-agent/agent-id \\
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
            <th className="px-4 py-2 text-right text-gray-600 dark:text-gray-400 font-medium">
              Actions
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
              <td className="px-4 py-2 text-right">
                <div className="inline-flex items-center gap-2">
                  <button
                    onClick={() => onScan(agent.id)}
                    disabled={agent.status !== 'healthy'}
                    className="inline-flex items-center gap-1.5 rounded bg-blue-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-blue-300 dark:disabled:bg-blue-900"
                  >
                    <RefreshCw className="w-3.5 h-3.5" />
                    Scan
                  </button>
                  <button
                    onClick={() => onDelete(agent)}
                    className="inline-flex h-7 w-7 items-center justify-center rounded border border-red-200 text-red-600 hover:bg-red-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-900/20"
                    title="Delete agent"
                    aria-label={`Delete ${agent.name}`}
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </div>
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
              <strong>Sync Now</strong>. If an agent is healthy, CloudPAM asks that agent to scan instead of using server-local AWS credentials.
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
