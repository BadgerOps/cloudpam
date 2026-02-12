import type { LucideIcon } from 'lucide-react'
import { Globe, Server, Building } from 'lucide-react'

export interface AllocationStrategy {
  id: string
  name: string
  description: string
  example: string
  best_for: string
  icon: LucideIcon
}

export const ALLOCATION_STRATEGIES: AllocationStrategy[] = [
  {
    id: 'region-first',
    name: 'Region-First',
    description: 'Top-level allocation by geographic region, then environment within each region',
    example: '10.0.0.0/8 → us-east/10.0.0.0/12, eu-west/10.16.0.0/12',
    best_for: 'Multi-region deployments with data sovereignty requirements',
    icon: Globe,
  },
  {
    id: 'environment-first',
    name: 'Environment-First',
    description: 'Top-level allocation by environment (prod/staging/dev), regions within each',
    example: '10.0.0.0/8 → prod/10.0.0.0/10, dev/10.64.0.0/10',
    best_for: 'Single-region or environment-isolated architectures',
    icon: Server,
  },
  {
    id: 'account-first',
    name: 'Account-First',
    description: 'Each AWS/GCP account gets a dedicated CIDR block',
    example: '10.0.0.0/8 → acct-001/10.0.0.0/16, acct-002/10.1.0.0/16',
    best_for: 'AWS Organizations, strong account isolation requirements',
    icon: Building,
  },
]
