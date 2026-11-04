import { describe, expect, it } from 'vitest'
import {
  availableTabs,
  buildAgentConfigs,
  splitList,
  type AgentConfigInput,
} from '../components/discoveryWizardConfig'

function awsInput(overrides: Partial<AgentConfigInput> = {}): AgentConfigInput {
  return {
    provider: 'aws',
    mode: 'single',
    token: 'tok',
    serverUrl: 'https://pam.example.com',
    agentName: 'prod-agent',
    accountId: 7,
    accountRegions: ['us-east-1', 'us-west-2'],
    awsOrg: { roleName: 'CloudPAMDiscoveryRole', externalId: '', regions: ['us-east-1'], exclude: [] },
    gcpProject: '',
    gcpOrg: { parent: '', regions: [], exclude: [], impersonateServiceAccount: '' },
    ...overrides,
  }
}

describe('splitList', () => {
  it('trims and drops empty entries', () => {
    expect(splitList(' a, ,b ,')).toEqual(['a', 'b'])
  })
})

describe('availableTabs for aws', () => {
  it('offers iam only in org mode', () => {
    expect(availableTabs('aws', 'single')).toEqual(['shell', 'yaml', 'terraform', 'docker'])
    expect(availableTabs('aws', 'organization')).toEqual(['shell', 'yaml', 'terraform', 'docker', 'iam'])
  })
})

describe('buildAgentConfigs for aws', () => {
  it('always states the provider', () => {
    for (const mode of ['single', 'organization'] as const) {
      const c = buildAgentConfigs(awsInput({ mode }))
      expect(c.shell).toContain("export CLOUDPAM_PROVIDER='aws'")
      expect(c.docker).toContain("-e CLOUDPAM_PROVIDER='aws'")
      expect(c.terraform).toContain("CLOUDPAM_PROVIDER='aws'")
      expect(c.yaml).toContain('provider: "aws"')
    }
  })

  it('renders single-account settings', () => {
    const c = buildAgentConfigs(awsInput())
    expect(c.shell).toContain('export CLOUDPAM_ACCOUNT_ID=7')
    expect(c.shell).toContain("export CLOUDPAM_AWS_REGIONS='us-east-1,us-west-2'")
    expect(c.yaml).toContain('aws_regions: ["us-east-1", "us-west-2"]')
    expect(c.iam).toBeUndefined()
  })

  it('renders org settings and the iam snippet', () => {
    const c = buildAgentConfigs(awsInput({
      mode: 'organization',
      awsOrg: { roleName: 'Role', externalId: 'ext', regions: ['eu-west-1'], exclude: ['111'] },
    }))
    expect(c.shell).toContain('export CLOUDPAM_AWS_ORG_ENABLED=true')
    expect(c.shell).toContain("export CLOUDPAM_AWS_ORG_EXTERNAL_ID='ext'")
    expect(c.shell).toContain("export CLOUDPAM_AWS_ORG_EXCLUDE_ACCOUNTS='111'")
    expect(c.shell).toContain("export AWS_REGION='eu-west-1'")
    expect(c.yaml).toContain('aws_org:\n  enabled: true\n  role_name: "Role"')
    expect(c.iam).toContain('sts:AssumeRole')
    expect(c.iam).toContain('"sts:ExternalId" = "ext"')
  })

  it('quotes hostile values', () => {
    const c = buildAgentConfigs(awsInput({ token: `x'; rm -rf /; '` }))
    expect(c.shell).toContain(`export CLOUDPAM_BOOTSTRAP_TOKEN='x'\\''; rm -rf /; '\\'''`)
  })
})
