import { describe, expect, it } from 'vitest'
import {
  GCP_PARENT_PATTERN,
  availableTabs,
  buildAgentConfigs,
  gcpProjectFor,
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

  it('prepares a state directory the non-root agent can write before docker run', () => {
    for (const mode of ['single', 'organization'] as const) {
      const lines = buildAgentConfigs(awsInput({ mode })).docker!.split('\n')
      const run = lines.findIndex(l => l.startsWith('docker run'))
      expect(lines.slice(0, run)).toContain('sudo install -d -o 65532 -g 65532 /var/lib/cloudpam-agent')
      expect(lines.slice(run)).toContain('  -v /var/lib/cloudpam-agent:/var/lib/cloudpam-agent \\')
      expect(lines.slice(run).some(l => l.trimStart().startsWith('#'))).toBe(false)
      expect(lines.slice(run, -1).every(l => l.endsWith(' \\'))).toBe(true)
    }
  })

  it('quotes hostile values', () => {
    const c = buildAgentConfigs(awsInput({ token: `x'; rm -rf /; '` }))
    expect(c.shell).toContain(`export CLOUDPAM_BOOTSTRAP_TOKEN='x'\\''; rm -rf /; '\\'''`)
  })
})

function gcpInput(overrides: Partial<AgentConfigInput> = {}): AgentConfigInput {
  return awsInput({
    provider: 'gcp',
    gcpProject: 'my-project',
    accountRegions: ['us-central1'],
    gcpOrg: { parent: 'organizations/123', regions: ['us-central1'], exclude: ['sandbox'], impersonateServiceAccount: '' },
    ...overrides,
  })
}

describe('gcp helpers', () => {
  it('validates parents', () => {
    expect(GCP_PARENT_PATTERN.test('organizations/1')).toBe(true)
    expect(GCP_PARENT_PATTERN.test('folders/22')).toBe(true)
    expect(GCP_PARENT_PATTERN.test('projects/1')).toBe(false)
    expect(GCP_PARENT_PATTERN.test('folders/x')).toBe(false)
  })

  it('derives the project from external_id, then key', () => {
    expect(gcpProjectFor({ external_id: 'ext', key: 'gcp:k' })).toBe('ext')
    expect(gcpProjectFor({ external_id: '', key: 'gcp:k' })).toBe('k')
    expect(gcpProjectFor({ key: 'bare' })).toBe('bare')
    expect(gcpProjectFor(null)).toBe('')
  })
})

describe('availableTabs for gcp', () => {
  it('has no terraform tab and always an iam tab', () => {
    expect(availableTabs('gcp', 'single')).toEqual(['shell', 'yaml', 'docker', 'iam'])
    expect(availableTabs('gcp', 'organization')).toEqual(['shell', 'yaml', 'docker', 'iam'])
  })
})

describe('buildAgentConfigs for gcp', () => {
  it('renders single-project settings', () => {
    const c = buildAgentConfigs(gcpInput())
    expect(c.terraform).toBeUndefined()
    expect(c.shell).toContain("export CLOUDPAM_PROVIDER='gcp'")
    expect(c.shell).toContain("export CLOUDPAM_GCP_PROJECT='my-project'")
    expect(c.shell).toContain("export CLOUDPAM_GCP_REGIONS='us-central1'")
    expect(c.shell).not.toContain('AWS')
    expect(c.yaml).toContain('provider: "gcp"\n')
    expect(c.yaml).toContain('gcp_project: "my-project"')
    expect(c.docker).toContain('GOOGLE_APPLICATION_CREDENTIALS')
    expect(c.iam).toContain('gcloud projects add-iam-policy-binding \'my-project\'')
    expect(c.iam).toContain('roles/compute.networkViewer')
    expect(c.iam).not.toContain('roles/browser')
  })

  it('keeps the docker note above the command it describes', () => {
    const lines = buildAgentConfigs(gcpInput()).docker!.split('\n')
    const run = lines.findIndex(l => l.startsWith('docker run'))
    expect(run).toBeGreaterThan(0)
    expect(lines.slice(run).some(l => l.trimStart().startsWith('#'))).toBe(false)
    expect(lines.slice(run, -1).every(l => l.endsWith(' \\'))).toBe(true)
  })

  it('hands the key to the non-root agent without making it world-readable', () => {
    const lines = buildAgentConfigs(gcpInput()).docker!.split('\n')
    const run = lines.findIndex(l => l.startsWith('docker run'))
    expect(lines.slice(0, run)).toContain('sudo install -d -o 65532 -g 65532 /var/lib/cloudpam-agent')
    expect(lines.slice(0, run)).toContain('sudo install -D -o 65532 -g 65532 -m 0400 key.json /etc/cloudpam-agent/gcp-key.json')
    expect(lines.slice(run)).toContain('  -v /etc/cloudpam-agent/gcp-key.json:/var/run/secrets/cloudpam/gcp/key.json:ro \\')
    expect(lines.join('\n')).not.toContain('$PWD/key.json')
  })

  it('renders org settings with direct grants', () => {
    const c = buildAgentConfigs(gcpInput({ mode: 'organization' }))
    expect(c.shell).toContain('export CLOUDPAM_GCP_ORG_ENABLED=true')
    expect(c.shell).toContain("export CLOUDPAM_GCP_ORG_PARENT='organizations/123'")
    expect(c.shell).toContain("export CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS='sandbox'")
    expect(c.shell).not.toContain('CLOUDPAM_GCP_PROJECT=')
    expect(c.shell).not.toContain('IMPERSONATE')
    expect(c.yaml).toContain('gcp_org:\n  enabled: true\n  parent: "organizations/123"')
    expect(c.iam).toContain("gcloud organizations add-iam-policy-binding '123'")
    expect(c.iam).toContain('roles/browser')
    expect(c.iam).toContain('roles/compute.networkViewer')
    expect(c.iam).not.toContain('serviceAccountTokenCreator')
  })

  it('renders folder grants and impersonation', () => {
    const c = buildAgentConfigs(gcpInput({
      mode: 'organization',
      gcpOrg: { parent: 'folders/9', regions: [], exclude: [], impersonateServiceAccount: 'cloudpam-discovery@{project}.iam.gserviceaccount.com' },
    }))
    expect(c.shell).toContain("export CLOUDPAM_GCP_ORG_IMPERSONATE_SA='cloudpam-discovery@{project}.iam.gserviceaccount.com'")
    expect(c.yaml).toContain('  impersonate_service_account: "cloudpam-discovery@{project}.iam.gserviceaccount.com"')
    expect(c.iam).toContain("gcloud resource-manager folders add-iam-policy-binding '9'")
    expect(c.iam).toContain('roles/iam.serviceAccountTokenCreator')
    expect(c.iam).toContain('PROJECT_ID')
    // With impersonation the agent itself gets no compute role on the folder:
    // every networkViewer grant goes to the per-project service account.
    const viewerGrants = c.iam!.split('\n').filter(l => l.includes('compute.networkViewer'))
    expect(viewerGrants.length).toBeGreaterThan(0)
    expect(viewerGrants.every(l => l.includes('cloudpam-discovery@'))).toBe(true)
  })

  it('creates a service account in the project its email names', () => {
    const perProject = buildAgentConfigs(gcpInput({
      mode: 'organization',
      gcpOrg: { parent: 'folders/9', regions: [], exclude: [], impersonateServiceAccount: 'disc-{project}@hub.iam.gserviceaccount.com' },
    })).iam!
    expect(perProject).toContain(`gcloud iam service-accounts create 'disc-'"\${PROJECT_ID}" --project='hub'`)
    expect(perProject).toContain(`gcloud iam service-accounts add-iam-policy-binding 'disc-'"\${PROJECT_ID}"'@hub.iam.gserviceaccount.com' --project='hub'`)

    const shared = buildAgentConfigs(gcpInput({
      mode: 'organization',
      gcpOrg: { parent: 'folders/9', regions: [], exclude: [], impersonateServiceAccount: 'cloudpam@hub.iam.gserviceaccount.com' },
    })).iam!
    expect(shared).toContain('one shared service account')
    expect(shared).toContain("gcloud iam service-accounts create 'cloudpam' --project='hub'")
    expect(shared).toContain("gcloud iam service-accounts add-iam-policy-binding 'cloudpam@hub.iam.gserviceaccount.com' --project='hub'")
  })

  it('enables the IAM Credentials API before the impersonation bindings', () => {
    const iam = buildAgentConfigs(gcpInput({
      mode: 'organization',
      gcpOrg: { parent: 'folders/9', regions: [], exclude: [], impersonateServiceAccount: 'disc@hub.iam.gserviceaccount.com' },
    })).iam!
    const enable = iam.indexOf("gcloud services enable 'iamcredentials.googleapis.com' --project='hub'")
    expect(enable).toBeGreaterThan(0)
    expect(enable).toBeLessThan(iam.indexOf('gcloud iam service-accounts add-iam-policy-binding'))
    expect(enable).toBeLessThan(iam.indexOf('gcloud projects add-iam-policy-binding'))

    const direct = buildAgentConfigs(gcpInput({ mode: 'organization' })).iam!
    expect(direct).not.toContain('iamcredentials')
  })

  it('says compute.googleapis.com must be enabled in each discovered project', () => {
    for (const mode of ['single', 'organization'] as const) {
      expect(buildAgentConfigs(gcpInput({ mode })).iam).toContain('compute.googleapis.com')
    }
  })

  it('quotes hostile project ids in every output', () => {
    const project = `p'$(id)"x`
    const c = buildAgentConfigs(gcpInput({ gcpProject: project }))
    const shellLit = `'p'\\''$(id)"x'`
    expect(c.shell).toContain(`export CLOUDPAM_GCP_PROJECT=${shellLit}\n`)
    expect(c.docker).toContain(`  -e CLOUDPAM_GCP_PROJECT=${shellLit} \\`)
    expect(c.yaml).toContain(`gcp_project: "p'$(id)\\"x"\n`)
    expect(c.iam).toContain(`gcloud projects add-iam-policy-binding ${shellLit} \\`)
  })

  it('quotes hostile impersonation templates in every output', () => {
    const template = `x'$(id)"@{project}.iam.gserviceaccount.com`
    const c = buildAgentConfigs(gcpInput({
      mode: 'organization',
      gcpOrg: { parent: 'organizations/1', regions: [], exclude: [], impersonateServiceAccount: template },
    }))
    expect(c.shell).toContain(`export CLOUDPAM_GCP_ORG_IMPERSONATE_SA='x'\\''$(id)"@{project}.iam.gserviceaccount.com'`)
    expect(c.docker).toContain(`  -e CLOUDPAM_GCP_ORG_IMPERSONATE_SA='x'\\''$(id)"@{project}.iam.gserviceaccount.com' \\`)
    expect(c.yaml).toContain(`  impersonate_service_account: "x'$(id)\\"@{project}.iam.gserviceaccount.com"`)
    expect(c.iam).toContain(`gcloud iam service-accounts create 'x'\\''$(id)"' --project="\${PROJECT_ID}"`)
    expect(c.iam).toContain(`--member='serviceAccount:x'\\''$(id)"@'"\${PROJECT_ID}"'.iam.gserviceaccount.com'`)
  })
})
