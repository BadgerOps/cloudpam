import { useState } from 'react'
import { PoolTree } from 'cloudpam-ui'
import type { PoolWithStats } from 'cloudpam-ui'

// Stats are derived from the CIDR so utilization colors are believable rather
// than arbitrary: a /8 really does hold 16.7M addresses.
function pool(
  id: number,
  name: string,
  cidr: string,
  type: string,
  total: number,
  used: number,
  children: PoolWithStats[] = [],
): PoolWithStats {
  return {
    id,
    name,
    cidr,
    type,
    status: 'active',
    source: 'manual',
    created_at: '2026-02-11T09:00:00Z',
    updated_at: '2026-07-28T14:35:00Z',
    children,
    stats: {
      total_ips: total,
      used_ips: used,
      available_ips: total - used,
      utilization: Math.round((used / total) * 1000) / 10,
      child_count: children.length,
      direct_children: children.length,
    },
  } as unknown as PoolWithStats
}

const corpNetwork: PoolWithStats[] = [
  pool(1, 'Corporate Supernet', '10.0.0.0/8', 'supernet', 16777216, 4194304, [
    pool(2, 'us-east-1', '10.0.0.0/12', 'region', 1048576, 733184, [
      pool(4, 'production', '10.0.0.0/16', 'environment', 65536, 58982),
      pool(5, 'staging', '10.1.0.0/16', 'environment', 65536, 19660),
    ]),
    pool(3, 'eu-west-1', '10.16.0.0/12', 'region', 1048576, 209715, [
      pool(6, 'production', '10.16.0.0/16', 'environment', 65536, 26214),
    ]),
  ]),
]

export function Hierarchy() {
  const [selected, setSelected] = useState<number | null>(null)
  return (
    <div className="max-w-2xl">
      <PoolTree nodes={corpNetwork} selectedId={selected} onSelect={(p) => setSelected(p.id)} />
    </div>
  )
}

export function WithSelection() {
  return (
    <div className="max-w-2xl">
      <PoolTree nodes={corpNetwork} selectedId={2} onSelect={() => {}} />
    </div>
  )
}

export function FlatList() {
  return (
    <div className="max-w-2xl">
      <PoolTree
        nodes={[
          pool(10, 'dmz', '192.168.10.0/24', 'subnet', 256, 231),
          pool(11, 'management', '192.168.20.0/24', 'subnet', 256, 64),
          pool(12, 'transit', '192.168.30.0/24', 'subnet', 256, 12),
        ]}
        onSelect={() => {}}
      />
    </div>
  )
}

