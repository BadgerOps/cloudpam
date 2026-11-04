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

const AGENT_ID_FILE = '/var/lib/cloudpam-agent/agent-id'
const AGENT_IMAGE = 'ghcr.io/badgerops/cloudpam/agent:latest'

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

// Task 9 replaces this with the real GCP variables.
function gcpEnvVars(_input: AgentConfigInput): EnvVar[] {
  return []
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

function dockerConfig(vars: EnvVar[]): string {
  const lines = [
    'docker run -d \\',
    '  --name cloudpam-agent \\',
    '  -v /var/lib/cloudpam-agent:/var/lib/cloudpam-agent \\',
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

// Task 9 replaces this with the real GCP YAML.
function gcpYamlLines(_input: AgentConfigInput): string[] {
  return []
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

// Task 9 replaces this with the gcloud commands.
function gcpIamConfig(_input: AgentConfigInput): string {
  return ''
}

/** Builds the content of every tab the provider and mode offer. */
export function buildAgentConfigs(input: AgentConfigInput): Partial<Record<ConfigTab, string>> {
  const vars = envVars(input)
  const out: Partial<Record<ConfigTab, string>> = {}
  for (const tab of availableTabs(input.provider, input.mode)) {
    switch (tab) {
      case 'shell': out.shell = shellConfig(vars); break
      case 'yaml': out.yaml = yamlConfig(input); break
      case 'docker': out.docker = dockerConfig(vars); break
      case 'terraform': out.terraform = terraformConfig(input, vars); break
      case 'iam': out.iam = input.provider === 'gcp' ? gcpIamConfig(input) : awsIamConfig(input); break
    }
  }
  return out
}
