import { describe, expect, it } from 'vitest'
import { POOL_TYPES, UNKNOWN_POOL_TYPE_DOT, poolTypeDot } from '../utils/poolTypes'

describe('POOL_TYPES', () => {
  it('matches the five pool types the Go domain accepts', () => {
    // internal/domain/types.go ValidPoolTypes is authoritative.
    const ids = POOL_TYPES.map((t) => t.id)
    expect(ids).toEqual(['supernet', 'region', 'environment', 'vpc', 'subnet'])
  })

  it('does not resurrect the dead root/account keys', () => {
    // 'root' is a node id (useSchemaGenerator) and 'account' is a search-result
    // kind (SearchModal) — neither is a pool type.
    expect(poolTypeDot('root')).toBe(UNKNOWN_POOL_TYPE_DOT)
    expect(poolTypeDot('account')).toBe(UNKNOWN_POOL_TYPE_DOT)
  })

  it('never assigns the unknown-type grey to a known type', () => {
    // Grey must mean exactly one thing: "type not recognised". If a known
    // type also renders grey, an unrecognised type becomes invisible.
    const greys = POOL_TYPES.filter((t) => t.dot === UNKNOWN_POOL_TYPE_DOT)
    expect(greys).toEqual([])
  })

  it('gives every type a non-empty label', () => {
    for (const t of POOL_TYPES) {
      expect(t.label.length).toBeGreaterThan(0)
    }
  })

  it('resolves a known type to its dot class', () => {
    expect(poolTypeDot('subnet')).toBe('bg-orange-500')
    expect(poolTypeDot('supernet')).toBe('bg-purple-500')
  })

  it('falls back to grey for an unrecognised type', () => {
    expect(poolTypeDot('nonsense')).toBe(UNKNOWN_POOL_TYPE_DOT)
  })
})
