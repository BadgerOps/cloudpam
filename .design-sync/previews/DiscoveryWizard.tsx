import { DiscoveryWizard } from 'cloudpam-ui'
import type { Account } from 'cloudpam-ui'

const accounts = [
  {
    id: 1,
    key: 'prod-payments',
    name: 'Production — Payments',
    provider: 'aws',
    external_id: '123456789012',
    tier: 'prd',
    environment: 'production',
    regions: ['us-east-1', 'us-west-2'],
    created_at: '2026-01-20T10:00:00Z',
  },
  {
    id: 2,
    key: 'staging-core',
    name: 'Staging — Core',
    provider: 'aws',
    external_id: '210987654321',
    tier: 'stg',
    environment: 'staging',
    regions: ['us-east-1'],
    created_at: '2026-02-14T10:00:00Z',
  },
  {
    id: 3,
    key: 'analytics-gcp',
    name: 'Analytics Platform',
    provider: 'gcp',
    external_id: 'analytics-prod-8842',
    tier: 'prd',
    environment: 'production',
    regions: ['europe-west1'],
    created_at: '2026-04-02T10:00:00Z',
  },
] as unknown as Account[]

// The wizard owns its step state, so a card shows step 1 — account selection —
// against a realistic multi-cloud account list.
export function AccountSelection() {
  return (
    <div style={{ position: 'relative', height: 700, width: '100%', transform: 'translateZ(0)' }}>
      <DiscoveryWizard
        accounts={accounts}
        onAccountCreated={() => {}}
        onClose={() => {}}
        onComplete={() => {}}
      />
    </div>
  )
}

export function NoAccountsYet() {
  return (
    <div style={{ position: 'relative', height: 700, width: '100%', transform: 'translateZ(0)' }}>
      <DiscoveryWizard
        accounts={[]}
        onAccountCreated={() => {}}
        onClose={() => {}}
        onComplete={() => {}}
      />
    </div>
  )
}
