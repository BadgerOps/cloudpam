import { hclHeredocShellQuote, hclIdent, hclQuote, shellQuote, yamlQuote } from '../utils/escape'

export type WizardProvider = 'aws' | 'gcp'
export type DiscoveryMode = 'single' | 'organization'
export type ConfigTab = 'shell' | 'yaml' | 'terraform' | 'docker' | 'iam'

export interface AwsOrgInput {
  roleName: string
  externalId: string
  regions: string[]
  exclude: string[]
}

export interface GcpOrgInput {
  parent: string
  regions: string[]
  exclude: string[]
  impersonateServiceAccount: string
}

export interface AgentConfigInput {
  provider: WizardProvider
  mode: DiscoveryMode
  token: string
  serverUrl: string
  agentName: string
  accountId: number
  accountRegions: string[]
  awsOrg: AwsOrgInput
  gcpProject: string
  gcpOrg: GcpOrgInput
}

const AGENT_STATE_DIR = '/var/lib/cloudpam-agent'
const AGENT_ID_FILE = `${AGENT_STATE_DIR}/agent-id`
// The nonroot user of the cgr.dev/chainguard/static agent images.
const AGENT_UID = 65532
const AGENT_IMAGE = 'ghcr.io/badgerops/cloudpam/agent:latest'
const GCP_KEY_FILE = '/var/run/secrets/cloudpam/gcp/key.json'
const HOST_GCP_KEY_DIR = '/etc/cloudpam-agent'
const HOST_GCP_KEY_FILE = `${HOST_GCP_KEY_DIR}/gcp-key.json`

export const GCP_PARENT_PATTERN = /^(organizations|folders)\/[0-9]+$/

const SERVICE_ACCOUNT_ID = /^[a-z][a-z0-9-]{5,29}$/
const SERVICE_ACCOUNT_EMAIL = /^[^@\s{}]+@[^@\s{}]+\.[^@\s{}]+$/
// The sample CheckServiceAccountTemplate substitutes in the agent.
const SAMPLE_PROJECT_ID = 'project-id'
const LONGEST_PROJECT_ID = 'p'.repeat(30)

export type ImpersonationTemplateCheck = 'ok' | 'long' | 'invalid'

function serviceAccountFor(template: string, project: string): string {
  return template.split('{project}').join(project)
}

function validServiceAccount(email: string): boolean {
  const at = email.indexOf('@')
  return at > 0 && SERVICE_ACCOUNT_ID.test(email.slice(0, at)) && SERVICE_ACCOUNT_EMAIL.test(email)
}

/**
 * 'invalid' when the agent would reject the template, 'long' when some
 * project IDs (up to 30 characters) would push the account ID past 30
 * characters. Empty means no impersonation and is 'ok'.
 */
export function checkImpersonationTemplate(template: string): ImpersonationTemplateCheck {
  if (!template) return 'ok'
  if (!validServiceAccount(serviceAccountFor(template, SAMPLE_PROJECT_ID))) return 'invalid'
  return validServiceAccount(serviceAccountFor(template, LONGEST_PROJECT_ID)) ? 'ok' : 'long'
}

/** The GCP project an account maps to: external ID, else key without "gcp:". */
export function gcpProjectFor(account: { external_id?: string; key: string } | null | undefined): string {
  if (!account) return ''
  if (account.external_id) return account.external_id
  return account.key.startsWith('gcp:') ? account.key.slice(4) : account.key
}

/**
 * Resolves which config tab to render. The selection is kept in state, but the
 * set of available tabs depends on provider and mode, so a stale selection
 * must fall back instead of rendering a tab the strip no longer offers.
 */
export function resolveConfigTab(selected: ConfigTab, available: ConfigTab[]): ConfigTab {
  return available.includes(selected) ? selected : available[0]
}

export function availableTabs(provider: WizardProvider, mode: DiscoveryMode): ConfigTab[] {
  if (provider === 'gcp') return ['shell', 'yaml', 'docker', 'iam']
  return mode === 'organization'
    ? ['shell', 'yaml', 'terraform', 'docker', 'iam']
    : ['shell', 'yaml', 'terraform', 'docker']
}

export function splitList(value: string): string[] {
  return value.split(',').map(v => v.trim()).filter(Boolean)
}

/** A literal number is rendered bare; everything else is quoted per format. */
type EnvValue = string | number | true
type EnvVar = [name: string, value: EnvValue]

function envVars(input: AgentConfigInput): EnvVar[] {
  const vars: EnvVar[] = [
    ['CLOUDPAM_BOOTSTRAP_TOKEN', input.token],
    ['CLOUDPAM_AGENT_ID_FILE', AGENT_ID_FILE],
    ['CLOUDPAM_ACCOUNT_ID', input.accountId],
    ['CLOUDPAM_PROVIDER', input.provider],
  ]
  return vars.concat(input.provider === 'gcp' ? gcpEnvVars(input) : awsEnvVars(input))
}

function awsEnvVars(input: AgentConfigInput): EnvVar[] {
  if (input.mode === 'single') {
    return [['CLOUDPAM_AWS_REGIONS', input.accountRegions.join(',')]]
  }
  const org = input.awsOrg
  const primary = org.regions[0] ?? 'us-east-1'
  const vars: EnvVar[] = [
    ['AWS_REGION', primary],
    ['AWS_DEFAULT_REGION', primary],
    ['CLOUDPAM_AWS_ORG_ENABLED', true],
    ['CLOUDPAM_AWS_ORG_ROLE_NAME', org.roleName],
  ]
  if (org.externalId) vars.push(['CLOUDPAM_AWS_ORG_EXTERNAL_ID', org.externalId])
  vars.push(['CLOUDPAM_AWS_ORG_REGIONS', org.regions.join(',')])
  if (org.exclude.length > 0) vars.push(['CLOUDPAM_AWS_ORG_EXCLUDE_ACCOUNTS', org.exclude.join(',')])
  return vars
}

function gcpEnvVars(input: AgentConfigInput): EnvVar[] {
  if (input.mode === 'single') {
    const vars: EnvVar[] = [['CLOUDPAM_GCP_PROJECT', input.gcpProject]]
    if (input.accountRegions.length > 0) vars.push(['CLOUDPAM_GCP_REGIONS', input.accountRegions.join(',')])
    return vars
  }
  const org = input.gcpOrg
  const vars: EnvVar[] = [
    ['CLOUDPAM_GCP_ORG_ENABLED', true],
    ['CLOUDPAM_GCP_ORG_PARENT', org.parent],
  ]
  if (org.regions.length > 0) vars.push(['CLOUDPAM_GCP_ORG_REGIONS', org.regions.join(',')])
  if (org.exclude.length > 0) vars.push(['CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS', org.exclude.join(',')])
  if (org.impersonateServiceAccount) vars.push(['CLOUDPAM_GCP_ORG_IMPERSONATE_SA', org.impersonateServiceAccount])
  return vars
}

function renderShellValue(value: EnvValue, quote: (v: unknown) => string): string {
  if (value === true) return 'true'
  if (typeof value === 'number') return String(value)
  return quote(value)
}

function shellConfig(vars: EnvVar[]): string {
  const lines = vars.map(([name, value]) => `export ${name}=${renderShellValue(value, shellQuote)}`)
  return ['#!/bin/bash', ...lines, './cloudpam-agent'].join('\n')
}

function dockerConfig(input: AgentConfigInput, vars: EnvVar[]): string {
  const isGcp = input.provider === 'gcp'
  // Preparation goes above `docker run`: a comment line inside a
  // backslash-continued command would end the command. The image runs as
  // nonroot, and Docker creates a missing bind-mount source owned by root.
  const prepare = [
    `# Assumes a Linux host with rootful Docker, run as root or with sudo. With rootless Docker or userns-remap, use the host UID that container UID ${AGENT_UID} maps to.`,
    `# The agent runs as UID ${AGENT_UID} and writes its ID under this directory.`,
    `sudo install -d -o ${AGENT_UID} -g ${AGENT_UID} ${AGENT_STATE_DIR}`,
    ...(isGcp
      ? [
          '# Copy the service account key so only the agent user can read it. Omit these two lines, the key mount and',
          '# GOOGLE_APPLICATION_CREDENTIALS when the host has Application Default Credentials (GCE metadata, Workload Identity).',
          `sudo install -d -o ${AGENT_UID} -g ${AGENT_UID} -m 0700 ${HOST_GCP_KEY_DIR}`,
          `sudo install -o ${AGENT_UID} -g ${AGENT_UID} -m 0400 key.json ${HOST_GCP_KEY_FILE}`,
        ]
      : []),
  ]
  const gcpKey = isGcp
    ? [
        `  -v ${HOST_GCP_KEY_FILE}:${GCP_KEY_FILE}:ro \\`,
        `  -e GOOGLE_APPLICATION_CREDENTIALS=${shellQuote(GCP_KEY_FILE)} \\`,
      ]
    : []
  const lines = [
    ...prepare,
    'docker run -d \\',
    '  --name cloudpam-agent \\',
    `  -v ${AGENT_STATE_DIR}:${AGENT_STATE_DIR} \\`,
    ...gcpKey,
    ...vars.map(([name, value]) => `  -e ${name}=${renderShellValue(value, shellQuote)} \\`),
    `  ${AGENT_IMAGE}`,
  ]
  return lines.join('\n')
}

function terraformConfig(input: AgentConfigInput, vars: EnvVar[]): string {
  const envLines = vars.map(([name, value]) => name === 'CLOUDPAM_BOOTSTRAP_TOKEN'
    ? `      CLOUDPAM_BOOTSTRAP_TOKEN=\${var.cloudpam_bootstrap_token} \\`
    : `      ${name}=${renderShellValue(value, hclHeredocShellQuote)} \\`)
  return `variable "cloudpam_bootstrap_token" {
  default   = ${hclQuote(input.token)}
  sensitive = true
}

resource "null_resource" "cloudpam_agent" {
  provisioner "local-exec" {
    command = <<-EOT
${envLines.join('\n')}
      ./cloudpam-agent
    EOT
  }
}`
}

function yamlList(values: string[]): string {
  return `[${values.map(yamlQuote).join(', ')}]`
}

function yamlConfig(input: AgentConfigInput): string {
  const lines = [
    `server_url: ${yamlQuote(input.serverUrl)}`,
    `bootstrap_token: ${yamlQuote(input.token)}`,
    `agent_id_file: ${yamlQuote(AGENT_ID_FILE)}`,
    `account_id: ${input.accountId}`,
    `agent_name: ${yamlQuote(input.agentName)}`,
    `provider: ${yamlQuote(input.provider)}`,
    'sync_interval: "15m"',
    'heartbeat_interval: "1m"',
  ]
  return lines.concat(input.provider === 'gcp' ? gcpYamlLines(input) : awsYamlLines(input)).join('\n')
}

function awsYamlLines(input: AgentConfigInput): string[] {
  if (input.mode === 'single') return [`aws_regions: ${yamlList(input.accountRegions)}`]
  const org = input.awsOrg
  const lines = ['aws_org:', '  enabled: true', `  role_name: ${yamlQuote(org.roleName)}`]
  if (org.externalId) lines.push(`  external_id: ${yamlQuote(org.externalId)}`)
  lines.push(`  regions: ${yamlList(org.regions)}`)
  if (org.exclude.length > 0) lines.push(`  exclude_accounts: ${yamlList(org.exclude)}`)
  return lines
}

function gcpYamlLines(input: AgentConfigInput): string[] {
  if (input.mode === 'single') {
    const lines = [`gcp_project: ${yamlQuote(input.gcpProject)}`]
    if (input.accountRegions.length > 0) lines.push(`gcp_regions: ${yamlList(input.accountRegions)}`)
    return lines
  }
  const org = input.gcpOrg
  const lines = ['gcp_org:', '  enabled: true', `  parent: ${yamlQuote(org.parent)}`]
  if (org.regions.length > 0) lines.push(`  regions: ${yamlList(org.regions)}`)
  if (org.exclude.length > 0) lines.push(`  exclude_projects: ${yamlList(org.exclude)}`)
  if (org.impersonateServiceAccount) lines.push(`  impersonate_service_account: ${yamlQuote(org.impersonateServiceAccount)}`)
  return lines
}

function awsIamConfig(input: AgentConfigInput): string {
  const { roleName, externalId } = input.awsOrg
  const roleIdent = hclIdent(roleName, 'cloudpam_discovery_role')
  return `# IAM Setup for AWS Organizations Discovery
#
# 1. Deploy the member role to all accounts via CloudFormation StackSet
#    or Terraform (deploy/terraform/aws-org-discovery/member-role/)
#
# 2. Attach the management policy to the agent's IAM role
#    (deploy/terraform/aws-org-discovery/management-policy/)

# --- Member Account Role (Terraform) ---
# Deploy to each member account:

resource "aws_iam_role" "${roleIdent}" {
  name = ${hclQuote(roleName)}

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { AWS = "arn:aws:iam::<MANAGEMENT_ACCOUNT_ID>:root" }
      Action    = "sts:AssumeRole"${externalId ? `
      Condition = { StringEquals = { "sts:ExternalId" = ${hclQuote(externalId)} } }` : ''}
    }]
  })
}

resource "aws_iam_role_policy" "discovery" {
  role = aws_iam_role.${roleIdent}.id
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
        Resource = ${hclQuote(`arn:aws:iam::*:role/${roleName}`)}
      }
    ]
  })
}`
}

const PROJECT_PLACEHOLDER = '{project}'
const SERVICE_ACCOUNT_DOMAIN = '.iam.gserviceaccount.com'

/**
 * One shell word for a template: every literal part single-quoted, every
 * {project} replaced by a double-quoted ${PROJECT_ID} expansion.
 */
function shellTemplate(template: string): string {
  const word = template
    .split(PROJECT_PLACEHOLDER)
    .map(part => (part ? shellQuote(part) : ''))
    .join('"${PROJECT_ID}"')
  return word || "''"
}

function gcpBindCommand(parent: string): string {
  const [kind, id] = parent.split('/')
  const group = kind === 'folders' ? 'resource-manager folders' : 'organizations'
  return `gcloud ${group} add-iam-policy-binding ${shellQuote(id ?? '')}`
}

function gcpIamConfig(input: AgentConfigInput): string {
  const header = `#!/bin/bash
# IAM setup for CloudPAM GCP discovery.
# Replace AGENT_SA_EMAIL with the service account the agent runs as
# (its Workload Identity service account, VM service account, or key's account).
# Every discovered project must have the Compute Engine API (compute.googleapis.com) enabled.
AGENT_SA_EMAIL='cloudpam-agent@YOUR_PROJECT.iam.gserviceaccount.com'
`
  if (input.mode === 'single') {
    return `${header}
gcloud projects add-iam-policy-binding ${shellQuote(input.gcpProject)} \\
  --member="serviceAccount:\${AGENT_SA_EMAIL}" --role='roles/compute.networkViewer'`
  }

  const org = input.gcpOrg
  const bind = gcpBindCommand(org.parent)
  const listing = `# List the projects under the organization or folder (needed in every org mode):
${bind} \\
  --member="serviceAccount:\${AGENT_SA_EMAIL}" --role='roles/browser'`

  const template = org.impersonateServiceAccount
  if (!template) {
    return `${header}
${listing}

# Read every project's networks through inheritance:
${bind} \\
  --member="serviceAccount:\${AGENT_SA_EMAIL}" --role='roles/compute.networkViewer'`
  }

  const at = template.lastIndexOf('@')
  const name = at < 0 ? template : template.slice(0, at)
  const domain = at < 0 ? '' : template.slice(at + 1)
  // A service account lives in the project its email names, which is not
  // necessarily the project it reads.
  const homeProject = domain.endsWith(SERVICE_ACCOUNT_DOMAIN)
    ? shellTemplate(domain.slice(0, -SERVICE_ACCOUNT_DOMAIN.length))
    : '"${PROJECT_ID}"'
  const email = shellTemplate(template)
  const member = shellTemplate(`serviceAccount:${template}`)
  const shared = template.includes(PROJECT_PLACEHOLDER)
    ? ''
    : '\n# The template names one shared service account: create it and grant token creator once, then grant roles/compute.networkViewer in each project.'
  return `${header}
${listing}

# generateAccessToken is checked against the caller's project, so enable the IAM Credentials API in the
# agent's own project. Enabling it in the discovery service accounts' home project is not required.
# If the agent runs as the default compute service account (NNN-compute@developer.gserviceaccount.com),
# set AGENT_PROJECT to the agent's project ID by hand instead.
AGENT_PROJECT=\${AGENT_SA_EMAIL#*@}
AGENT_PROJECT=\${AGENT_PROJECT%%.iam.gserviceaccount.com}
gcloud services enable ${shellQuote('iamcredentials.googleapis.com')} --project="\${AGENT_PROJECT}"

# Per project: create the discovery service account, let it read networks, and
# let the agent impersonate it. Run once per project ID.${shared}
PROJECT_ID='my-project'
gcloud iam service-accounts create ${shellTemplate(name)} --project=${homeProject} \\
  --display-name='CloudPAM discovery'
gcloud projects add-iam-policy-binding "\${PROJECT_ID}" \\
  --member=${member} --role='roles/compute.networkViewer'
gcloud iam service-accounts add-iam-policy-binding ${email} --project=${homeProject} \\
  --member="serviceAccount:\${AGENT_SA_EMAIL}" --role='roles/iam.serviceAccountTokenCreator'`
}

/** Builds the content of every tab the provider and mode offer. */
export function buildAgentConfigs(input: AgentConfigInput): Partial<Record<ConfigTab, string>> {
  const vars = envVars(input)
  const out: Partial<Record<ConfigTab, string>> = {}
  for (const tab of availableTabs(input.provider, input.mode)) {
    switch (tab) {
      case 'shell': out.shell = shellConfig(vars); break
      case 'yaml': out.yaml = yamlConfig(input); break
      case 'docker': out.docker = dockerConfig(input, vars); break
      case 'terraform': out.terraform = terraformConfig(input, vars); break
      case 'iam': out.iam = input.provider === 'gcp' ? gcpIamConfig(input) : awsIamConfig(input); break
    }
  }
  return out
}
