import { PoolDetailPanel } from 'cloudpam-ui'
import type { PoolWithStats } from 'cloudpam-ui'

function pool(over: Record<string, unknown>): PoolWithStats {
  return {
    id: 42,
    name: 'production',
    cidr: '10.0.0.0/16',
    parent_id: 2,
    account_id: 7,
    type: 'environment',
    status: 'active',
    source: 'manual',
    description: 'Primary production environment for the us-east-1 region.',
    created_at: '2026-02-11T09:00:00Z',
    updated_at: '2026-07-28T14:35:00Z',
    stats: {
      total_ips: 65536,
      used_ips: 58982,
      available_ips: 6554,
      utilization: 90.0,
      child_count: 12,
      direct_children: 4,
    },
    ...over,
  } as unknown as PoolWithStats
}

const Frame = ({ children }: { children: React.ReactNode }) => (
  <div className="max-w-md">{children}</div>
)

export function NearCapacity() {
  return (
    <Frame>
      <PoolDetailPanel pool={pool({})} onClose={() => {}} onEdit={() => {}} />
    </Frame>
  )
}

export function HealthyUtilization() {
  return (
    <Frame>
      <PoolDetailPanel
        pool={pool({
          id: 43,
          name: 'staging',
          cidr: '10.1.0.0/16',
          description: 'Pre-production staging environment, refreshed nightly.',
          stats: {
            total_ips: 65536,
            used_ips: 19660,
            available_ips: 45876,
            utilization: 30.0,
            child_count: 5,
            direct_children: 2,
          },
        })}
        onClose={() => {}}
        onEdit={() => {}}
      />
    </Frame>
  )
}

export function PlannedPool() {
  return (
    <Frame>
      <PoolDetailPanel
        pool={pool({
          id: 44,
          name: 'eu-west-1 supernet',
          cidr: '10.16.0.0/12',
          type: 'region',
          status: 'planned',
          description: 'Reserved for the upcoming Frankfurt expansion.',
          stats: {
            total_ips: 1048576,
            used_ips: 0,
            available_ips: 1048576,
            utilization: 0,
            child_count: 0,
            direct_children: 0,
          },
        })}
        onClose={() => {}}
        onEdit={() => {}}
      />
    </Frame>
  )
}

// onEdit is optional: omitting it swaps the inline "Edit Pool" action for a
// "Manage Pool" link that routes to the pool's own page.
export function WithoutEditHandler() {
  return (
    <Frame>
      <PoolDetailPanel
        pool={pool({
          id: 45,
          name: 'legacy-dc1',
          cidr: '172.16.0.0/16',
          status: 'deprecated',
          description: 'Retired datacenter range. Scheduled for reclamation in Q4.',
          stats: {
            total_ips: 65536,
            used_ips: 3277,
            available_ips: 62259,
            utilization: 5.0,
            child_count: 2,
            direct_children: 2,
          },
        })}
        onClose={() => {}}
      />
    </Frame>
  )
}
