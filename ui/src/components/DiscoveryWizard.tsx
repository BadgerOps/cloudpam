import { useState, useCallback, useEffect, useRef } from 'react'
import { X, Check, Copy, Download, ChevronLeft, ChevronRight, AlertTriangle, Loader2, Activity } from 'lucide-react'
import { useAgentProvisioning, useDiscoveryAgents } from '../hooks/useDiscovery'
import { useAccounts } from '../hooks/useAccounts'
import { useToast } from '../hooks/useToast'
import ConnectivityBadge from './ConnectivityBadge'
import { safeFilename } from '../utils/escape'
import type { Account, CreateAccountRequest, AgentProvisionResponse, DiscoveryAgent } from '../api/types'
import {
  GCP_PARENT_PATTERN,
  availableTabs as tabsFor,
  buildAgentConfigs,
  gcpProjectFor,
  resolveConfigTab,
  splitList,
  validImpersonationTemplate,
  type ConfigTab,
  type DiscoveryMode,
  type WizardProvider,
} from './discoveryWizardConfig'

export { resolveConfigTab, type ConfigTab }

interface DiscoveryWizardProps {
  accounts: Account[]
  onAccountCreated: () => void
  onClose: () => void
  onComplete?: () => void
}

export default function DiscoveryWizard({ accounts, onAccountCreated, onClose, onComplete }: DiscoveryWizardProps) {
  const [step, setStep] = useState(1)
  const [discoveryMode, setDiscoveryMode] = useState<DiscoveryMode>('single')
  const [provider, setProvider] = useState<WizardProvider>('aws')
  const providerAccounts = accounts.filter(a => a.provider === provider)
  const [selectedAccountId, setSelectedAccountId] = useState<number | null>(
    accounts.find(a => a.provider === 'aws')?.id ?? null,
  )
  const [showNewAccount, setShowNewAccount] = useState(false)
  const [newAccount, setNewAccount] = useState<Pick<CreateAccountRequest, 'key' | 'name'>>({ key: '', name: '' })
  const [createdAccount, setCreatedAccount] = useState<Account | null>(null)
  const [regionsInput, setRegionsInput] = useState('')
  const [agentName, setAgentName] = useState('')
  const [provisionResult, setProvisionResult] = useState<AgentProvisionResponse | null>(null)
  const [configTab, setConfigTab] = useState<ConfigTab>('shell')
  const [copied, setCopied] = useState(false)
  const [keyWarningDismissed, setKeyWarningDismissed] = useState(false)

  // Org mode fields
  const [orgRoleName, setOrgRoleName] = useState('CloudPAMDiscoveryRole')
  const [orgExternalId, setOrgExternalId] = useState('')
  const [orgRegionsInput, setOrgRegionsInput] = useState('us-east-1, us-west-2')
  const [orgExcludeInput, setOrgExcludeInput] = useState('')

  // GCP fields
  const [gcpProjectInput, setGcpProjectInput] = useState('')
  const [gcpOrgParent, setGcpOrgParent] = useState('')
  const [gcpOrgRegionsInput, setGcpOrgRegionsInput] = useState('')
  const [gcpOrgExcludeInput, setGcpOrgExcludeInput] = useState('')
  const [gcpOrgImpersonate, setGcpOrgImpersonate] = useState('')

  // Agent status tracking
  const [connectedAgent, setConnectedAgent] = useState<DiscoveryAgent | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const { createAccount } = useAccounts()
  const { provision, loading: provisionLoading, error: provisionError } = useAgentProvisioning()
  const { agents, fetch: fetchAgents } = useDiscoveryAgents()
  const { showToast } = useToast()

  // A just-created account may not be in `accounts` until the parent refetches.
  const selectedAccount = accounts.find(a => a.id === selectedAccountId) ??
    (createdAccount?.id === selectedAccountId && createdAccount.provider === provider ? createdAccount : null)

  // Poll for agent connection when on step 3 with a provisioned agent
  useEffect(() => {
    if (step === 3 && provisionResult && !connectedAgent) {
      // Start polling for agent connection
      const poll = () => {
        fetchAgents()
      }
      poll() // immediate
      pollRef.current = setInterval(poll, 5000)

      return () => {
        if (pollRef.current) {
          clearInterval(pollRef.current)
          pollRef.current = null
        }
      }
    }
    return () => {
      if (pollRef.current) {
        clearInterval(pollRef.current)
        pollRef.current = null
      }
    }
  }, [step, provisionResult, connectedAgent, fetchAgents])

  // Check if the provisioned agent has connected
  useEffect(() => {
    if (provisionResult && agents.length > 0) {
      const match =
        agents.find(a => a.api_key_id === provisionResult.api_key_id) ??
        agents.find(a => a.name === provisionResult.agent_name && a.status === 'healthy')
      if (match) {
        setConnectedAgent(match)
        if (pollRef.current) {
          clearInterval(pollRef.current)
          pollRef.current = null
        }
      }
    }
  }, [agents, provisionResult])

  const copyToClipboard = useCallback(async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      showToast('Failed to copy', 'error')
    }
  }, [showToast])

  const downloadFile = useCallback((content: string, filename: string) => {
    const blob = new Blob([content], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    a.click()
    URL.revokeObjectURL(url)
  }, [])

  async function handleCreateAccount() {
    try {
      const regions = regionsInput.split(',').map(r => r.trim()).filter(Boolean)
      const account = await createAccount({ ...newAccount, provider, regions })
      setCreatedAccount(account)
      selectAccount(account.id)
      setShowNewAccount(false)
      onAccountCreated()
      showToast(`Account "${account.name}" created`, 'success')
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to create account', 'error')
    }
  }

  async function handleProvision() {
    const name = agentName.trim()
    if (!name) return
    try {
      const result = await provision(name)
      setProvisionResult(result)
    } catch {
      // error is handled in the hook
    }
  }

  function handleBack() {
    if (step === 2 && provisionResult) {
      if (!keyWarningDismissed) {
        setKeyWarningDismissed(true)
        return
      }
    }
    setStep(s => Math.max(1, s - 1))
    setKeyWarningDismissed(false)
  }

  function handleNext() {
    setStep(s => Math.min(3, s + 1))
    setKeyWarningDismissed(false)
  }

  // Pre-fill agent name when moving to step 2
  function goToStep2() {
    if (discoveryMode === 'single' && selectedAccount && !agentName) {
      setAgentName(`${selectedAccount.name.toLowerCase().replace(/\s+/g, '-')}-agent`)
    } else if (discoveryMode === 'organization' && !agentName) {
      setAgentName(provider === 'gcp' ? 'gcp-org-discovery-agent' : 'org-discovery-agent')
    }
    handleNext()
  }

  // The project override belongs to one account, so it never survives a change of account.
  function selectAccount(id: number | null) {
    setSelectedAccountId(id)
    setGcpProjectInput('')
  }

  function chooseProvider(next: WizardProvider) {
    setProvider(next)
    selectAccount(accounts.find(a => a.provider === next)?.id ?? null)
  }

  // Generate config content
  const token = provisionResult?.token ?? '<token>'
  const accountId = selectedAccountId ?? 0
  const accountRegions = selectedAccount?.regions ?? []
  const agentNameFinal = provisionResult?.agent_name ?? agentName
  const serverUrl = provisionResult?.server_url ?? window.location.origin
  const isOrg = discoveryMode === 'organization'

  const orgRegions = splitList(orgRegionsInput)
  const orgExclude = splitList(orgExcludeInput)

  const derivedGcpProject = gcpProjectFor(selectedAccount)
  const gcpProject = gcpProjectInput.trim() || derivedGcpProject
  const impersonateValid = validImpersonationTemplate(gcpOrgImpersonate)
  const step1Valid = selectedAccount?.provider === provider && (
    provider !== 'gcp' ||
    (isOrg ? GCP_PARENT_PATTERN.test(gcpOrgParent) && impersonateValid : gcpProject !== '')
  )

  const generated = buildAgentConfigs({
    provider,
    mode: discoveryMode,
    token,
    serverUrl,
    agentName: agentNameFinal,
    accountId,
    accountRegions,
    awsOrg: { roleName: orgRoleName, externalId: orgExternalId, regions: orgRegions, exclude: orgExclude },
    gcpProject,
    gcpOrg: {
      parent: gcpOrgParent,
      regions: splitList(gcpOrgRegionsInput),
      exclude: splitList(gcpOrgExcludeInput),
      impersonateServiceAccount: gcpOrgImpersonate,
    },
  })
  const availableTabs = tabsFor(provider, discoveryMode)

  // The selected tab is derived rather than trusted: 'iam' only exists in org
  // mode, so switching back to single-account mode would otherwise keep
  // rendering the org IAM snippet under a tab strip that no longer offers it.
  const activeConfigTab = resolveConfigTab(configTab, availableTabs)

  const filenameStem = safeFilename(agentNameFinal)

  const tabMeta: Record<ConfigTab, { filename: string; label: string }> = {
    shell: { filename: `${filenameStem}.sh`, label: 'Shell' },
    yaml: { filename: `${filenameStem}.yaml`, label: 'YAML' },
    terraform: { filename: `${filenameStem}.tf`, label: 'Terraform' },
    docker: { filename: `${filenameStem}-docker.sh`, label: 'Docker' },
    iam: { filename: provider === 'gcp' ? `${filenameStem}-iam.sh` : `${filenameStem}-iam.tf`, label: 'IAM Setup' },
  }
  const configs = Object.fromEntries(
    availableTabs.map(tab => [tab, { content: generated[tab] ?? '', ...tabMeta[tab] }]),
  ) as Record<ConfigTab, { content: string; filename: string; label: string }>

  const canGoNext = (step === 1 && step1Valid) ||
    (step === 2 && provisionResult !== null)

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div
        data-testid="discovery-wizard-panel"
        className="bg-white dark:bg-gray-800 shadow-xl w-full h-full max-h-full flex flex-col md:h-auto md:mx-4 md:max-w-2xl md:max-h-[90vh] md:rounded-lg"
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b dark:border-gray-700">
          <div>
            <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">Set Up Discovery</h2>
            <div className="flex items-center gap-2 mt-1">
              {[1, 2, 3].map(s => (
                <div key={s} className="flex items-center gap-1">
                  <div className={`w-6 h-6 rounded-full flex items-center justify-center text-xs font-medium ${
                    s < step ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400' :
                    s === step ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400' :
                    'bg-gray-100 text-gray-400 dark:bg-gray-700 dark:text-gray-500'
                  }`}>
                    {s < step ? <Check className="w-3.5 h-3.5" /> : s}
                  </div>
                  {s < 3 && <div className={`w-8 h-0.5 ${s < step ? 'bg-green-300 dark:bg-green-700' : 'bg-gray-200 dark:bg-gray-700'}`} />}
                </div>
              ))}
            </div>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-auto px-6 py-4">
          {/* Step 1: Select Mode & Account */}
          {step === 1 && (
            <div className="space-y-4">
              <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">Cloud Provider</h3>
              <div className="flex gap-3" role="radiogroup" aria-label="Cloud provider">
                {(['aws', 'gcp'] as const).map(p => (
                  <label
                    key={p}
                    className={`flex-1 cursor-pointer rounded-lg border-2 p-3 text-sm font-medium transition-colors ${
                      provider === p
                        ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20 dark:border-blue-400 text-gray-900 dark:text-gray-100'
                        : 'border-gray-200 dark:border-gray-600 hover:border-gray-300 dark:hover:border-gray-500 text-gray-700 dark:text-gray-300'
                    }`}
                  >
                    <input
                      type="radio"
                      name="provider"
                      value={p}
                      checked={provider === p}
                      onChange={() => chooseProvider(p)}
                      className="sr-only"
                    />
                    {p.toUpperCase()}
                  </label>
                ))}
              </div>

              <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">Discovery Mode</h3>

              {/* Mode toggle */}
              <div className="flex gap-3">
                <label className={`flex-1 cursor-pointer rounded-lg border-2 p-3 transition-colors ${
                  discoveryMode === 'single'
                    ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20 dark:border-blue-400'
                    : 'border-gray-200 dark:border-gray-600 hover:border-gray-300 dark:hover:border-gray-500'
                }`}>
                  <input
                    type="radio"
                    name="mode"
                    value="single"
                    checked={discoveryMode === 'single'}
                    onChange={() => setDiscoveryMode('single')}
                    className="sr-only"
                  />
                  <div className="text-sm font-medium text-gray-900 dark:text-gray-100">{provider === 'gcp' ? 'Single Project' : 'Single Account'}</div>
                  <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    {provider === 'gcp' ? 'Discover networks in one GCP project' : 'Discover resources in one AWS account'}
                  </div>
                </label>
                <label className={`flex-1 cursor-pointer rounded-lg border-2 p-3 transition-colors ${
                  discoveryMode === 'organization'
                    ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20 dark:border-blue-400'
                    : 'border-gray-200 dark:border-gray-600 hover:border-gray-300 dark:hover:border-gray-500'
                }`}>
                  <input
                    type="radio"
                    name="mode"
                    value="organization"
                    checked={discoveryMode === 'organization'}
                    onChange={() => setDiscoveryMode('organization')}
                    className="sr-only"
                  />
                  <div className="text-sm font-medium text-gray-900 dark:text-gray-100">{provider === 'gcp' ? 'GCP Organization or Folder' : 'AWS Organization'}</div>
                  <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    {provider === 'gcp' ? 'Discover every active project under an organization or folder' : 'Discover across all member accounts via AssumeRole'}
                  </div>
                </label>
              </div>

              {/* Account selection */}
              {(discoveryMode === 'single' || discoveryMode === 'organization') && (
                <div className="space-y-3">
                  <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300">
                    {discoveryMode === 'organization'
                      ? (provider === 'gcp' ? 'Select the Account This Agent Reports As' : 'Select Management Account')
                      : (provider === 'gcp' ? 'Select GCP Project Account' : 'Select Cloud Account')}
                  </h4>
                  {!showNewAccount ? (
                    <>
                      <select
                        value={selectedAccountId ?? ''}
                        onChange={e => selectAccount(Number(e.target.value))}
                        className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
                      >
                        <option value="" disabled>Choose an account...</option>
                        {providerAccounts.map(a => (
                          <option key={a.id} value={a.id}>
                            {a.name} ({a.provider || 'unknown'})
                          </option>
                        ))}
                      </select>
                      {provider === 'gcp' && discoveryMode === 'single' && selectedAccount && (
                        <div>
                          <label htmlFor="gcp-project" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">GCP project ID</label>
                          <input
                            id="gcp-project"
                            value={gcpProjectInput}
                            onChange={e => setGcpProjectInput(e.target.value)}
                            placeholder={derivedGcpProject || 'my-project-123'}
                            className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                          />
                          <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                            Defaults to the account&apos;s external ID, else its key without <code>gcp:</code>.
                          </p>
                        </div>
                      )}
                      {selectedAccount?.provider && (
                        <div className="flex flex-col gap-1 text-xs text-gray-500 dark:text-gray-400">
                          <span className="font-medium text-gray-700 dark:text-gray-300">Server connectivity</span>
                          <ConnectivityBadge accountId={selectedAccount.id} accountName={selectedAccount.name} showDetail />
                          <span>Tests from this server as the identity a server-side sync uses. The agent you provision next runs with its own credentials.</span>
                        </div>
                      )}
                      <button
                        onClick={() => setShowNewAccount(true)}
                        className="text-sm text-blue-600 dark:text-blue-400 hover:underline"
                      >
                        + Create new account
                      </button>
                    </>
                  ) : (
                    <div className="space-y-3 bg-gray-50 dark:bg-gray-900 rounded-lg p-4">
                      <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300">New Account</h4>
                      <div className="grid grid-cols-2 gap-3">
                        <div>
                          <label htmlFor="new-account-key" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Key</label>
                          <input
                            id="new-account-key"
                            value={newAccount.key}
                            onChange={e => setNewAccount(prev => ({ ...prev, key: e.target.value }))}
                            placeholder={provider === 'gcp' ? 'gcp:my-project' : 'aws-prod'}
                            className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                          />
                        </div>
                        <div>
                          <label htmlFor="new-account-name" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Name</label>
                          <input
                            id="new-account-name"
                            value={newAccount.name}
                            onChange={e => setNewAccount(prev => ({ ...prev, name: e.target.value }))}
                            placeholder={provider === 'gcp' ? 'GCP Production' : 'AWS Production'}
                            className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                          />
                        </div>
                        <div>
                          <span className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Provider</span>
                          <span
                            data-testid="new-account-provider"
                            className="block px-3 py-1.5 text-sm text-gray-700 dark:text-gray-300"
                          >
                            {provider.toUpperCase()}
                          </span>
                        </div>
                        <div>
                          <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Regions (comma-separated)</label>
                          <input
                            value={regionsInput}
                            onChange={e => setRegionsInput(e.target.value)}
                            placeholder="us-east-1, us-west-2"
                            className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                          />
                        </div>
                      </div>
                      <div className="flex gap-2">
                        <button
                          onClick={handleCreateAccount}
                          disabled={!newAccount.key || !newAccount.name}
                          className="px-3 py-1.5 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded disabled:opacity-50"
                        >
                          Create Account
                        </button>
                        <button
                          onClick={() => setShowNewAccount(false)}
                          className="px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200"
                        >
                          Cancel
                        </button>
                      </div>
                    </div>
                  )}
                </div>
              )}

              {/* Organization mode */}
              {discoveryMode === 'organization' && provider === 'aws' && (
                <div className="space-y-3">
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    The agent will run in the management account and discover resources across all member accounts
                    using <code className="text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">sts:AssumeRole</code>.
                    Accounts are auto-created in CloudPAM.
                  </p>
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Role Name</label>
                      <input
                        value={orgRoleName}
                        onChange={e => setOrgRoleName(e.target.value)}
                        placeholder="CloudPAMDiscoveryRole"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                    </div>
                    <div>
                      <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">External ID (optional)</label>
                      <input
                        value={orgExternalId}
                        onChange={e => setOrgExternalId(e.target.value)}
                        placeholder="cloudpam-discovery"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                    </div>
                    <div>
                      <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Regions (comma-separated)</label>
                      <input
                        value={orgRegionsInput}
                        onChange={e => setOrgRegionsInput(e.target.value)}
                        placeholder="us-east-1, us-west-2"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                    </div>
                    <div>
                      <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Exclude Accounts (optional)</label>
                      <input
                        value={orgExcludeInput}
                        onChange={e => setOrgExcludeInput(e.target.value)}
                        placeholder="123456789012, 987654321098"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                    </div>
                  </div>
                </div>
              )}

              {discoveryMode === 'organization' && provider === 'gcp' && (
                <div className="space-y-3">
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    The agent lists every active project under the organization or folder and discovers each one.
                    Projects are auto-created in CloudPAM as <code className="text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">gcp:&lt;project&gt;</code> accounts.
                  </p>
                  <div className="grid grid-cols-2 gap-3">
                    <div className="col-span-2">
                      <label htmlFor="gcp-org-parent" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Organization or folder</label>
                      <input
                        id="gcp-org-parent"
                        value={gcpOrgParent}
                        onChange={e => setGcpOrgParent(e.target.value.trim())}
                        placeholder="organizations/123456789 or folders/987654321"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                      {gcpOrgParent && !GCP_PARENT_PATTERN.test(gcpOrgParent) && (
                        <p className="mt-1 text-xs text-red-600 dark:text-red-400">Use organizations/&lt;id&gt; or folders/&lt;id&gt;.</p>
                      )}
                    </div>
                    <div>
                      <label htmlFor="gcp-org-regions" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Regions (optional, comma-separated)</label>
                      <input
                        id="gcp-org-regions"
                        value={gcpOrgRegionsInput}
                        onChange={e => setGcpOrgRegionsInput(e.target.value)}
                        placeholder="us-central1, europe-west1"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                    </div>
                    <div>
                      <label htmlFor="gcp-org-exclude" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Exclude projects (optional)</label>
                      <input
                        id="gcp-org-exclude"
                        value={gcpOrgExcludeInput}
                        onChange={e => setGcpOrgExcludeInput(e.target.value)}
                        placeholder="sandbox-project, legacy-project"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                    </div>
                    <div className="col-span-2">
                      <label htmlFor="gcp-org-impersonate" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Impersonate service account (optional)</label>
                      <input
                        id="gcp-org-impersonate"
                        value={gcpOrgImpersonate}
                        onChange={e => setGcpOrgImpersonate(e.target.value.trim())}
                        placeholder="cloudpam-discovery@{project}.iam.gserviceaccount.com"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                      {!impersonateValid && (
                        <p className="mt-1 text-xs text-red-600 dark:text-red-400">
                          Use a service account email. The part before @ must be 6 to 30 lowercase letters, digits or hyphens,
                          starting with a letter, even when <code>{'{project}'}</code> is a 30-character project ID.
                        </p>
                      )}
                      <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                        Empty: the agent reads every project as itself, through grants on the organization or folder.
                        Set: it impersonates this account in each project; <code>{'{project}'}</code> becomes the project ID.
                      </p>
                    </div>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Step 2: Provision Agent */}
          {step === 2 && (
            <div className="space-y-4">
              <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">Provision Agent</h3>
              <p className="text-sm text-gray-500 dark:text-gray-400">
                {isOrg
                  ? `Create a discovery agent for your ${provider === 'gcp' ? 'GCP organization or folder' : 'AWS Organization'}. This generates a bootstrap token and API key.`
                  : <>Create a discovery agent for <strong>{selectedAccount?.name}</strong>. This generates a bootstrap token and API key.</>
                }
              </p>

              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Agent Name</label>
                <input
                  value={agentName}
                  onChange={e => setAgentName(e.target.value)}
                  placeholder="my-agent"
                  disabled={!!provisionResult}
                  className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100 disabled:opacity-50"
                />
              </div>

              {!provisionResult ? (
                <>
                  <button
                    onClick={handleProvision}
                    disabled={provisionLoading || !agentName.trim()}
                    className="px-4 py-2 text-sm bg-green-600 hover:bg-green-700 text-white rounded-lg disabled:opacity-50"
                  >
                    {provisionLoading ? 'Provisioning...' : 'Provision Agent'}
                  </button>
                  {provisionError && (
                    <div className="text-sm text-red-600 dark:text-red-400">{provisionError}</div>
                  )}
                </>
              ) : (
                <div className="space-y-3">
                  <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg p-4">
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-sm font-medium text-green-800 dark:text-green-300">API Key</span>
                      <button
                        onClick={() => copyToClipboard(provisionResult.api_key)}
                        className="flex items-center gap-1 text-xs text-green-700 dark:text-green-400 hover:underline"
                      >
                        <Copy className="w-3.5 h-3.5" />
                        {copied ? 'Copied!' : 'Copy'}
                      </button>
                    </div>
                    <code className="block text-sm font-mono text-green-900 dark:text-green-200 break-all bg-green-100 dark:bg-green-900/40 px-3 py-2 rounded">
                      {provisionResult.api_key}
                    </code>
                    <p className="mt-2 text-xs text-green-700 dark:text-green-400">
                      <strong>This key is shown only once.</strong> Copy it now. You will not be able to retrieve it later.
                    </p>
                  </div>

                  {keyWarningDismissed && (
                    <div className="flex items-start gap-2 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-lg p-3">
                      <AlertTriangle className="w-4 h-4 text-yellow-600 dark:text-yellow-400 mt-0.5 flex-shrink-0" />
                      <div className="text-sm text-yellow-700 dark:text-yellow-300">
                        Going back will not discard the provisioned agent, but the API key above will be lost if you haven&apos;t copied it.
                        <button
                          onClick={() => { setKeyWarningDismissed(false); setStep(1) }}
                          className="block mt-1 text-yellow-800 dark:text-yellow-200 font-medium hover:underline"
                        >
                          Go back anyway
                        </button>
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          {/* Step 3: Deployment Config */}
          {step === 3 && (
            <div className="space-y-4">
              <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">Deployment Configuration</h3>
              <p className="text-sm text-gray-500 dark:text-gray-400">
                Choose a deployment method and use the generated configuration to start the agent.
                {provider === 'gcp'
                  ? ' The "IAM Setup" tab shows the gcloud commands that grant the agent read access.'
                  : isOrg && ' The "IAM Setup" tab shows the Terraform snippet for member role and management policy.'}
              </p>

              {/* Tabs */}
              <div className="flex gap-1 border-b border-gray-200 dark:border-gray-700">
                {availableTabs.map(tab => (
                  <button
                    key={tab}
                    onClick={() => setConfigTab(tab)}
                    className={`px-3 py-2 text-sm font-medium border-b-2 -mb-px ${
                      activeConfigTab === tab
                        ? 'border-blue-600 text-blue-600 dark:text-blue-400'
                        : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
                    }`}
                  >
                    {configs[tab].label}
                  </button>
                ))}
              </div>

              {/* Config content */}
              <div className="relative">
                <pre className="bg-gray-50 dark:bg-gray-900 border dark:border-gray-700 rounded-lg p-4 text-sm font-mono text-gray-800 dark:text-gray-200 overflow-x-auto whitespace-pre">
                  {configs[activeConfigTab].content}
                </pre>
                <div className="absolute top-2 right-2 flex gap-1">
                  <button
                    onClick={() => copyToClipboard(configs[activeConfigTab].content)}
                    title="Copy to clipboard"
                    className="p-1.5 bg-white dark:bg-gray-800 border dark:border-gray-600 rounded text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 shadow-sm"
                  >
                    <Copy className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => downloadFile(configs[activeConfigTab].content, configs[activeConfigTab].filename)}
                    title="Download file"
                    className="p-1.5 bg-white dark:bg-gray-800 border dark:border-gray-600 rounded text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 shadow-sm"
                  >
                    <Download className="w-4 h-4" />
                  </button>
                </div>
              </div>

              {/* Agent connection status */}
              {provisionResult && (
                <div className={`flex items-center gap-3 rounded-lg border p-3 ${
                  connectedAgent
                    ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800'
                    : 'bg-yellow-50 dark:bg-yellow-900/20 border-yellow-200 dark:border-yellow-800'
                }`}>
                  {connectedAgent ? (
                    <>
                      <Activity className="w-5 h-5 text-green-600 dark:text-green-400 flex-shrink-0" />
                      <div>
                        <div className="text-sm font-medium text-green-800 dark:text-green-300">
                          Agent connected
                        </div>
                        <div className="text-xs text-green-700 dark:text-green-400">
                          <strong>{connectedAgent.name}</strong> is {connectedAgent.status} &mdash; {connectedAgent.hostname || 'unknown host'}, version {connectedAgent.version || 'unknown'}
                        </div>
                      </div>
                    </>
                  ) : (
                    <>
                      <Loader2 className="w-5 h-5 text-yellow-600 dark:text-yellow-400 flex-shrink-0 animate-spin" />
                      <div>
                        <div className="text-sm font-medium text-yellow-800 dark:text-yellow-300">
                          Waiting for agent to connect...
                        </div>
                        <div className="text-xs text-yellow-700 dark:text-yellow-400">
                          Deploy the agent using the configuration above. It will appear here once it sends its first heartbeat.
                        </div>
                      </div>
                    </>
                  )}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-6 py-4 border-t dark:border-gray-700">
          <div>
            {step > 1 && (
              <button
                onClick={handleBack}
                className="flex items-center gap-1 px-3 py-2 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200"
              >
                <ChevronLeft className="w-4 h-4" />
                Back
              </button>
            )}
          </div>
          <div className="flex gap-3">
            <button
              onClick={() => { if (step === 3 && onComplete) onComplete(); onClose() }}
              className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 border dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700"
            >
              {step === 3 ? 'Done' : 'Cancel'}
            </button>
            {step < 3 && (
              <button
                onClick={step === 1 ? goToStep2 : handleNext}
                disabled={!canGoNext}
                className="flex items-center gap-1 px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded-lg disabled:opacity-50"
              >
                Next
                <ChevronRight className="w-4 h-4" />
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
