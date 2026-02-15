import { useState, useCallback, useEffect, useRef } from 'react'
import { X, Check, Copy, Download, ChevronLeft, ChevronRight, AlertTriangle, Loader2, Activity } from 'lucide-react'
import { useAgentProvisioning, useDiscoveryAgents } from '../hooks/useDiscovery'
import { useAccounts } from '../hooks/useAccounts'
import { useToast } from '../hooks/useToast'
import type { Account, CreateAccountRequest, AgentProvisionResponse, DiscoveryAgent } from '../api/types'

type ConfigTab = 'shell' | 'yaml' | 'terraform' | 'docker' | 'iam'
type DiscoveryMode = 'single' | 'organization'

interface DiscoveryWizardProps {
  accounts: Account[]
  onAccountCreated: () => void
  onClose: () => void
  onComplete?: () => void
}

export default function DiscoveryWizard({ accounts, onAccountCreated, onClose, onComplete }: DiscoveryWizardProps) {
  const [step, setStep] = useState(1)
  const [discoveryMode, setDiscoveryMode] = useState<DiscoveryMode>('single')
  const [selectedAccountId, setSelectedAccountId] = useState<number | null>(
    accounts.length > 0 ? accounts[0].id : null,
  )
  const [showNewAccount, setShowNewAccount] = useState(false)
  const [newAccount, setNewAccount] = useState<CreateAccountRequest>({
    key: '',
    name: '',
    provider: 'aws',
    regions: [],
  })
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

  // Agent status tracking
  const [connectedAgent, setConnectedAgent] = useState<DiscoveryAgent | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const { createAccount } = useAccounts()
  const { provision, loading: provisionLoading, error: provisionError } = useAgentProvisioning()
  const { agents, fetch: fetchAgents } = useDiscoveryAgents()
  const { showToast } = useToast()

  const selectedAccount = accounts.find(a => a.id === selectedAccountId) ?? null

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
      const match = agents.find(a => a.name === provisionResult.agent_name)
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
      const account = await createAccount({ ...newAccount, regions })
      setSelectedAccountId(account.id)
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
      setAgentName('org-discovery-agent')
    }
    handleNext()
  }

  // Generate config content
  const token = provisionResult?.token ?? '<token>'
  const accountId = selectedAccountId ?? 0
  const accountRegions = selectedAccount?.regions ?? []
  const agentNameFinal = provisionResult?.agent_name ?? agentName
  const serverUrl = provisionResult?.server_url ?? window.location.origin
  const isOrg = discoveryMode === 'organization'

  const orgRegions = orgRegionsInput.split(',').map(r => r.trim()).filter(Boolean)
  const orgExclude = orgExcludeInput.split(',').map(r => r.trim()).filter(Boolean)

  // Single-account configs
  const shellConfigSingle = `#!/bin/bash
export CLOUDPAM_BOOTSTRAP_TOKEN="${token}"
export CLOUDPAM_ACCOUNT_ID=${accountId}
./cloudpam-agent`

  const yamlConfigSingle = `server_url: "${serverUrl}"
bootstrap_token: "${token}"
account_id: ${accountId}
agent_name: "${agentNameFinal}"
interval: 5m
aws:
  regions: [${accountRegions.map(r => `"${r}"`).join(', ')}]`

  // Org-mode configs
  const shellConfigOrg = `#!/bin/bash
export CLOUDPAM_BOOTSTRAP_TOKEN="${token}"
export CLOUDPAM_AWS_ORG_ENABLED=true
export CLOUDPAM_AWS_ORG_ROLE_NAME="${orgRoleName}"${orgExternalId ? `
export CLOUDPAM_AWS_ORG_EXTERNAL_ID="${orgExternalId}"` : ''}
export CLOUDPAM_AWS_ORG_REGIONS="${orgRegions.join(',')}"${orgExclude.length > 0 ? `
export CLOUDPAM_AWS_ORG_EXCLUDE_ACCOUNTS="${orgExclude.join(',')}"` : ''}
./cloudpam-agent`

  const yamlConfigOrg = `server_url: "${serverUrl}"
bootstrap_token: "${token}"
agent_name: "${agentNameFinal}"
aws_org:
  enabled: true
  role_name: "${orgRoleName}"${orgExternalId ? `
  external_id: "${orgExternalId}"` : ''}
  regions: [${orgRegions.map(r => `"${r}"`).join(', ')}]${orgExclude.length > 0 ? `
  exclude_accounts: [${orgExclude.map(a => `"${a}"`).join(', ')}]` : ''}`

  const terraformConfigSingle = `variable "cloudpam_bootstrap_token" {
  default   = "${token}"
  sensitive = true
}

resource "null_resource" "cloudpam_agent" {
  provisioner "local-exec" {
    command = <<-EOT
      CLOUDPAM_BOOTSTRAP_TOKEN=\${var.cloudpam_bootstrap_token} \\
      CLOUDPAM_ACCOUNT_ID=${accountId} \\
      ./cloudpam-agent
    EOT
  }
}`

  const terraformConfigOrg = `variable "cloudpam_bootstrap_token" {
  default   = "${token}"
  sensitive = true
}

resource "null_resource" "cloudpam_agent" {
  provisioner "local-exec" {
    command = <<-EOT
      CLOUDPAM_BOOTSTRAP_TOKEN=\${var.cloudpam_bootstrap_token} \\
      CLOUDPAM_AWS_ORG_ENABLED=true \\
      CLOUDPAM_AWS_ORG_ROLE_NAME=${orgRoleName} \\${orgExternalId ? `
      CLOUDPAM_AWS_ORG_EXTERNAL_ID=${orgExternalId} \\` : ''}
      CLOUDPAM_AWS_ORG_REGIONS=${orgRegions.join(',')} \\
      ./cloudpam-agent
    EOT
  }
}`

  const dockerConfigSingle = `docker run -d \\
  --name cloudpam-agent \\
  -e CLOUDPAM_BOOTSTRAP_TOKEN="${token}" \\
  -e CLOUDPAM_ACCOUNT_ID=${accountId} \\
  cloudpam/agent:latest`

  const dockerConfigOrg = `docker run -d \\
  --name cloudpam-agent \\
  -e CLOUDPAM_BOOTSTRAP_TOKEN="${token}" \\
  -e CLOUDPAM_AWS_ORG_ENABLED=true \\
  -e CLOUDPAM_AWS_ORG_ROLE_NAME="${orgRoleName}" \\${orgExternalId ? `
  -e CLOUDPAM_AWS_ORG_EXTERNAL_ID="${orgExternalId}" \\` : ''}
  -e CLOUDPAM_AWS_ORG_REGIONS="${orgRegions.join(',')}" \\${orgExclude.length > 0 ? `
  -e CLOUDPAM_AWS_ORG_EXCLUDE_ACCOUNTS="${orgExclude.join(',')}" \\` : ''}
  cloudpam/agent:latest`

  const iamSetupContent = `# IAM Setup for AWS Organizations Discovery
#
# 1. Deploy the member role to all accounts via CloudFormation StackSet
#    or Terraform (deploy/terraform/aws-org-discovery/member-role/)
#
# 2. Attach the management policy to the agent's IAM role
#    (deploy/terraform/aws-org-discovery/management-policy/)

# --- Member Account Role (Terraform) ---
# Deploy to each member account:

resource "aws_iam_role" "${orgRoleName}" {
  name = "${orgRoleName}"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { AWS = "arn:aws:iam::<MANAGEMENT_ACCOUNT_ID>:root" }
      Action    = "sts:AssumeRole"${orgExternalId ? `
      Condition = { StringEquals = { "sts:ExternalId" = "${orgExternalId}" } }` : ''}
    }]
  })
}

resource "aws_iam_role_policy" "discovery" {
  role = aws_iam_role.${orgRoleName}.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = [
        "ec2:DescribeVpcs",
        "ec2:DescribeSubnets",
        "ec2:DescribeAddresses"
      ]
      Resource = "*"
    }]
  })
}

# --- Management Account Policy ---
# Attach to the IAM role running the CloudPAM agent:

resource "aws_iam_policy" "cloudpam_org" {
  name = "CloudPAMOrgDiscoveryPolicy"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["organizations:ListAccounts", "organizations:DescribeOrganization"]
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = "sts:AssumeRole"
        Resource = "arn:aws:iam::*:role/${orgRoleName}"
      }
    ]
  })
}`

  const shellConfig = isOrg ? shellConfigOrg : shellConfigSingle
  const yamlConfig = isOrg ? yamlConfigOrg : yamlConfigSingle
  const terraformConfig = isOrg ? terraformConfigOrg : terraformConfigSingle
  const dockerConfig = isOrg ? dockerConfigOrg : dockerConfigSingle

  const availableTabs: ConfigTab[] = isOrg
    ? ['shell', 'yaml', 'terraform', 'docker', 'iam']
    : ['shell', 'yaml', 'terraform', 'docker']

  const configs: Record<ConfigTab, { content: string; filename: string; label: string }> = {
    shell: { content: shellConfig, filename: `${agentNameFinal}.sh`, label: 'Shell' },
    yaml: { content: yamlConfig, filename: `${agentNameFinal}.yaml`, label: 'YAML' },
    terraform: { content: terraformConfig, filename: `${agentNameFinal}.tf`, label: 'Terraform' },
    docker: { content: dockerConfig, filename: `${agentNameFinal}-docker.sh`, label: 'Docker' },
    iam: { content: iamSetupContent, filename: `${agentNameFinal}-iam.tf`, label: 'IAM Setup' },
  }

  const canGoNext = (step === 1 && (discoveryMode === 'organization' || selectedAccountId !== null)) ||
    (step === 2 && provisionResult !== null)

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-2xl mx-4 max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b dark:border-gray-700">
          <div>
            <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">Plan Discovery</h2>
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
                  <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Single Account</div>
                  <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Discover resources in one AWS account
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
                  <div className="text-sm font-medium text-gray-900 dark:text-gray-100">AWS Organization</div>
                  <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Discover across all member accounts via AssumeRole
                  </div>
                </label>
              </div>

              {/* Single account mode */}
              {discoveryMode === 'single' && (
                <div className="space-y-3">
                  <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300">Select Cloud Account</h4>
                  {!showNewAccount ? (
                    <>
                      <select
                        value={selectedAccountId ?? ''}
                        onChange={e => setSelectedAccountId(Number(e.target.value))}
                        className="w-full px-3 py-2 border dark:border-gray-600 rounded-lg text-sm dark:bg-gray-700 dark:text-gray-100"
                      >
                        <option value="" disabled>Choose an account...</option>
                        {accounts.map(a => (
                          <option key={a.id} value={a.id}>
                            {a.name} ({a.provider || 'unknown'})
                          </option>
                        ))}
                      </select>
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
                          <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Key</label>
                          <input
                            value={newAccount.key}
                            onChange={e => setNewAccount(prev => ({ ...prev, key: e.target.value }))}
                            placeholder="aws-prod"
                            className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                          />
                        </div>
                        <div>
                          <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Name</label>
                          <input
                            value={newAccount.name}
                            onChange={e => setNewAccount(prev => ({ ...prev, name: e.target.value }))}
                            placeholder="AWS Production"
                            className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                          />
                        </div>
                        <div>
                          <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Provider</label>
                          <select
                            value={newAccount.provider}
                            onChange={e => setNewAccount(prev => ({ ...prev, provider: e.target.value }))}
                            className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                          >
                            <option value="aws">AWS</option>
                            <option value="gcp">GCP</option>
                            <option value="azure">Azure</option>
                          </select>
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
              {discoveryMode === 'organization' && (
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
            </div>
          )}

          {/* Step 2: Provision Agent */}
          {step === 2 && (
            <div className="space-y-4">
              <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">Provision Agent</h3>
              <p className="text-sm text-gray-500 dark:text-gray-400">
                {isOrg
                  ? 'Create a discovery agent for your AWS Organization. This generates a bootstrap token and API key.'
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
                {isOrg && ' The "IAM Setup" tab shows the Terraform snippet for member role and management policy.'}
              </p>

              {/* Tabs */}
              <div className="flex gap-1 border-b border-gray-200 dark:border-gray-700">
                {availableTabs.map(tab => (
                  <button
                    key={tab}
                    onClick={() => setConfigTab(tab)}
                    className={`px-3 py-2 text-sm font-medium border-b-2 -mb-px ${
                      configTab === tab
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
                  {configs[configTab].content}
                </pre>
                <div className="absolute top-2 right-2 flex gap-1">
                  <button
                    onClick={() => copyToClipboard(configs[configTab].content)}
                    title="Copy to clipboard"
                    className="p-1.5 bg-white dark:bg-gray-800 border dark:border-gray-600 rounded text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 shadow-sm"
                  >
                    <Copy className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => downloadFile(configs[configTab].content, configs[configTab].filename)}
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
