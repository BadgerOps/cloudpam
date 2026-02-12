import type { LucideIcon } from 'lucide-react'
import { Globe, Building, Server, Network } from 'lucide-react'

export interface HierarchyLevel {
  level: string
  prefixSize: number
  description: string
}

export interface Blueprint {
  id: string
  name: string
  description: string
  icon: LucideIcon
  rootCidr: string
  hierarchy: HierarchyLevel[]
  recommended: string[]
}

export const BLUEPRINTS: Blueprint[] = [
  {
    id: 'enterprise-multi-region',
    name: 'Enterprise Multi-Region',
    description: 'For organizations with 3+ regions and 50+ accounts',
    icon: Globe,
    rootCidr: '10.0.0.0/8',
    hierarchy: [
      { level: 'region', prefixSize: 12, description: '16 regions, 1M IPs each' },
      { level: 'environment', prefixSize: 16, description: '16 envs/region, 65K IPs' },
      { level: 'vpc', prefixSize: 20, description: '16 accounts/env, 4K IPs' },
      { level: 'subnet', prefixSize: 24, description: '16 subnets/account, 254 hosts' },
    ],
    recommended: [
      'AWS Organizations with 50+ accounts',
      'Multi-region active-active',
      'Large Kubernetes deployments',
    ],
  },
  {
    id: 'medium-organization',
    name: 'Medium Organization',
    description: 'For organizations with 1-2 regions and 10-50 accounts',
    icon: Building,
    rootCidr: '10.0.0.0/12',
    hierarchy: [
      { level: 'environment', prefixSize: 16, description: '16 environments, 65K IPs each' },
      { level: 'vpc', prefixSize: 20, description: '16 accounts/env, 4K IPs' },
      { level: 'subnet', prefixSize: 24, description: '16 subnets/account, 254 hosts' },
    ],
    recommended: [
      'Single or dual region deployments',
      'Growing startups',
      'Department-level cloud adoption',
    ],
  },
  {
    id: 'startup-simple',
    name: 'Startup / Simple',
    description: 'For small teams with straightforward needs',
    icon: Server,
    rootCidr: '10.0.0.0/16',
    hierarchy: [
      { level: 'environment', prefixSize: 20, description: '16 environments, 4K IPs each' },
      { level: 'subnet', prefixSize: 24, description: '16 subnets/env, 254 hosts' },
    ],
    recommended: [
      'Single account deployments',
      'Development/POC environments',
      'Small production workloads',
    ],
  },
  {
    id: 'custom',
    name: 'Custom Schema',
    description: 'Define your own hierarchy from scratch',
    icon: Network,
    rootCidr: '',
    hierarchy: [],
    recommended: [
      'Specific compliance requirements',
      'Existing IP schema migration',
      'Non-standard topologies',
    ],
  },
]
