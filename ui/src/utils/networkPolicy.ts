import type { NetworkSchemaPolicy } from '../api/types'

export type NetworkSchemaPolicyName = 'account_level' | 'region_level' | 'global' | 'manual'
export type ManualDuplicateScope = 'manual' | 'global'

export interface NetworkSchemaPolicyOption {
  name: NetworkSchemaPolicyName
  label: string
  description: string
}

export const NETWORK_SCHEMA_POLICIES: readonly NetworkSchemaPolicyOption[] = [
  {
    name: 'account_level',
    label: 'Account policy',
    description: 'Each account owns its address space. Repeated CIDRs inside one account are flagged; the same CIDR in two accounts is expected reuse.',
  },
  {
    name: 'region_level',
    label: 'Region policy',
    description: 'Each account region owns its address space. Repeated CIDRs inside one region of one account are flagged; the same CIDR in another account or region is expected reuse.',
  },
  {
    name: 'global',
    label: 'Global policy',
    description: 'One shared address space. Any repeated CIDR is flagged, whichever account or region it is in.',
  },
  {
    name: 'manual',
    label: 'Manual policy',
    description: 'Ownership and hierarchy come from the relationships you record. Duplicate CIDRs are not flagged unless you turn on network-wide detection below.',
  },
]

export const DEFAULT_NETWORK_SCHEMA_POLICY: NetworkSchemaPolicyName = 'account_level'

// The server accepts "custom" as an alias of "manual" and normalizes unknown
// names to the account policy, so the UI shows the same choice the server applies.
export function networkSchemaPolicyName(name: string | undefined | null): NetworkSchemaPolicyName {
  const lower = (name ?? '').trim().toLowerCase()
  if (lower === 'custom') return 'manual'
  return NETWORK_SCHEMA_POLICIES.some((option) => option.name === lower)
    ? (lower as NetworkSchemaPolicyName)
    : DEFAULT_NETWORK_SCHEMA_POLICY
}

export function networkSchemaPolicyLabel(name: string | undefined | null): string {
  const resolved = networkSchemaPolicyName(name)
  return NETWORK_SCHEMA_POLICIES.find((option) => option.name === resolved)?.label ?? resolved
}

export function manualDuplicateScope(policy: Pick<NetworkSchemaPolicy, 'duplicate_scope'> | null | undefined): ManualDuplicateScope {
  return policy?.duplicate_scope?.trim().toLowerCase() === 'global' ? 'global' : 'manual'
}

// The server derives the other fields from the name; only the manual policy
// keeps a chosen duplicate scope.
export function networkSchemaPolicyRequest(name: NetworkSchemaPolicyName, duplicateScope: ManualDuplicateScope): Partial<NetworkSchemaPolicy> {
  if (name === 'manual') return { name, duplicate_scope: duplicateScope }
  return { name }
}
