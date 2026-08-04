import { useState } from 'react'
import { TemplateStep } from 'cloudpam-ui'
import type { Blueprint } from 'cloudpam-ui'

// TemplateStep renders the built-in BLUEPRINTS list itself (and pulls each
// card's icon from there), so a preview only has to supply the fields the
// *selected* summary panel reads: rootCidr, hierarchy and recommended. The
// 'custom' branch reads none of them, hence the id-only stand-in for it.
const asBlueprint = (id: string) => ({ id }) as Blueprint

const enterpriseBlueprint = {
  id: 'enterprise-multi-region',
  name: 'Enterprise Multi-Region',
  description: 'For organizations with 3+ regions and 50+ accounts',
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
} as unknown as Blueprint

export function Unselected() {
  return (
    <TemplateStep
      selectedBlueprint={null}
      setSelectedBlueprint={() => {}}
      customCidr="10.0.0.0/8"
      setCustomCidr={() => {}}
    />
  )
}

export function EnterpriseSelected() {
  return (
    <TemplateStep
      selectedBlueprint={enterpriseBlueprint}
      setSelectedBlueprint={() => {}}
      customCidr="10.0.0.0/8"
      setCustomCidr={() => {}}
    />
  )
}

export function CustomSchema() {
  const [cidr, setCidr] = useState('172.16.0.0/12')
  return (
    <TemplateStep
      selectedBlueprint={asBlueprint('custom')}
      setSelectedBlueprint={() => {}}
      customCidr={cidr}
      setCustomCidr={setCidr}
    />
  )
}
