// Single source of truth for pool-type presentation.
//
// Grey is reserved: it means "type not recognised". Nothing in POOL_TYPES may
// use UNKNOWN_POOL_TYPE_DOT, or an unknown type becomes indistinguishable from
// a known one. Dot classes are written as full literal strings because
// Tailwind v4 only compiles classes it can find literally in ui/src.
// The five ids mirror internal/domain/types.go ValidPoolTypes, which the API
// enforces via IsValidPoolType. Do not add 'root' or 'account': the former is a
// node id in the schema generator, the latter a search-result kind.
import type { PoolType } from '../api/types'

export interface PoolTypeMeta {
  id: PoolType
  label: string
  dot: string
}

export const UNKNOWN_POOL_TYPE_DOT = 'bg-gray-400'

export const POOL_TYPES: readonly PoolTypeMeta[] = [
  { id: 'supernet', label: 'Supernet', dot: 'bg-purple-500' },
  { id: 'region', label: 'Region', dot: 'bg-blue-500' },
  { id: 'environment', label: 'Environment', dot: 'bg-green-500' },
  { id: 'vpc', label: 'VPC', dot: 'bg-amber-500' },
  { id: 'subnet', label: 'Subnet', dot: 'bg-orange-500' },
]

const BY_ID = new Map(POOL_TYPES.map((t) => [t.id, t]))

export function poolTypeDot(type: string): string {
  return BY_ID.get(type as PoolType)?.dot ?? UNKNOWN_POOL_TYPE_DOT
}
