import { describe, it, expect } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useSchemaGenerator } from '../wizard/hooks/useSchemaGenerator'
import type { Blueprint } from '../wizard/data/blueprints'
import { BLUEPRINTS } from '../wizard/data/blueprints'
import type { SchemaNode } from '../wizard/utils/cidr'
import { isWithin } from '../wizard/utils/cidr'
import type { Dimensions } from '../wizard/steps/DimensionsStep'
import { POOL_TYPES } from '../utils/poolTypes'

const baseDimensions: Dimensions = {
  regions: ['us-east-1', 'us-west-2'],
  environments: ['prod', 'dev'],
  accountTiers: ['core', 'workload'],
  accountsPerEnv: 2,
  growthYears: 3,
}

const enterpriseBlueprint: Blueprint = {
  id: 'enterprise',
  name: 'Enterprise',
  description: 'Large org',
  rootCidr: '10.0.0.0/8',
  icon: 'Building' as unknown as Blueprint['icon'],
  hierarchy: [],
  recommended: [],
}

describe('useSchemaGenerator', () => {
  it('generates region-first schema', () => {
    const { result } = renderHook(() =>
      useSchemaGenerator({
        selectedBlueprint: enterpriseBlueprint,
        customCidr: '',
        strategy: 'region-first',
        dimensions: baseDimensions,
      }),
    )

    const root = result.current.schema
    expect(root.id).toBe('root')
    expect(root.type).toBe('supernet')
    expect(root.cidr).toBe('10.0.0.0/8')

    // Should have 2 regions
    expect(root.children.length).toBe(2)
    expect(root.children[0].name).toBe('us-east-1')
    expect(root.children[0].type).toBe('region')
    expect(root.children[1].name).toBe('us-west-2')

    // Each region should have 2 environments
    expect(root.children[0].children.length).toBe(2)
    expect(root.children[0].children[0].name).toBe('prod')
    expect(root.children[0].children[0].type).toBe('environment')

    // Each environment should have accountsPerEnv accounts
    expect(root.children[0].children[0].children.length).toBe(2)
    expect(root.children[0].children[0].children[0].type).toBe('vpc')
  })

  it('generates environment-first schema', () => {
    const { result } = renderHook(() =>
      useSchemaGenerator({
        selectedBlueprint: enterpriseBlueprint,
        customCidr: '',
        strategy: 'environment-first',
        dimensions: baseDimensions,
      }),
    )

    const root = result.current.schema
    // Should have environments at top level
    expect(root.children.length).toBe(2)
    expect(root.children[0].name).toBe('prod')
    expect(root.children[0].type).toBe('environment')

    // Environments have accounts directly
    expect(root.children[0].children.length).toBe(4) // 2 accounts * 2 regions
    expect(root.children[0].children[0].type).toBe('vpc')
  })

  it('generates account-first schema', () => {
    const { result } = renderHook(() =>
      useSchemaGenerator({
        selectedBlueprint: enterpriseBlueprint,
        customCidr: '',
        strategy: 'account-first',
        dimensions: baseDimensions,
      }),
    )

    const root = result.current.schema
    // Should have flat accounts
    expect(root.children.length).toBe(4) // 2 envs * 2 accounts
    expect(root.children[0].type).toBe('vpc')
  })

  it('uses custom CIDR when blueprint is custom', () => {
    const customBlueprint: Blueprint = {
      id: 'custom',
      name: 'Custom',
      description: 'Custom CIDR',
      rootCidr: '',
      icon: 'Edit' as unknown as Blueprint['icon'],
      hierarchy: [],
      recommended: [],
    }

    const { result } = renderHook(() =>
      useSchemaGenerator({
        selectedBlueprint: customBlueprint,
        customCidr: '172.16.0.0/12',
        strategy: 'region-first',
        dimensions: baseDimensions,
      }),
    )

    expect(result.current.schema.cidr).toBe('172.16.0.0/12')
  })

  it('generates valid CIDR blocks (no overlaps)', () => {
    const { result } = renderHook(() =>
      useSchemaGenerator({
        selectedBlueprint: enterpriseBlueprint,
        customCidr: '',
        strategy: 'region-first',
        dimensions: baseDimensions,
      }),
    )

    // Collect all CIDRs
    const cidrs: string[] = []
    const collect = (node: SchemaNode) => {
      cidrs.push(node.cidr)
      node.children.forEach(collect)
    }
    collect(result.current.schema)

    // All CIDRs should be unique
    const unique = new Set(cidrs)
    expect(unique.size).toBe(cidrs.length)
  })

  it.each(['region-first', 'account-first'])(
    'only emits real pool types in the %s strategy',
    (strategy) => {
      // The wizard models a per-account allocation as type 'vpc', because
      // 'account' is a search-result kind and not a pool type -- the Schema
      // Planner legend renders from POOL_TYPES, so a node carrying anything
      // outside it would render as an unrecognised grey dot.
      const { result } = renderHook(() =>
        useSchemaGenerator({
          selectedBlueprint: enterpriseBlueprint,
          customCidr: '',
          strategy,
          dimensions: baseDimensions,
        }),
      )

      const valid = new Set(POOL_TYPES.map((t) => t.id))
      const seen: string[] = []
      const collect = (node: SchemaNode) => {
        seen.push(node.type)
        node.children.forEach(collect)
      }
      collect(result.current.schema)

      expect(seen.length).toBeGreaterThan(1)
      expect(seen.filter((t) => !valid.has(t as never))).toEqual([])
    },
  )

  const plannerDimensions: Dimensions = {
    regions: ['us-east-1', 'us-west-2', 'eu-west-1'],
    environments: ['prod', 'staging', 'dev'],
    accountTiers: ['core', 'workload'],
    accountsPerEnv: 5,
    growthYears: 3,
  }
  const customBlueprint = BLUEPRINTS.find((b) => b.id === 'custom')!
  const generate = (selectedBlueprint: Blueprint, customCidr: string, strategy: string) =>
    renderHook(() => useSchemaGenerator({ selectedBlueprint, customCidr, strategy, dimensions: plannerDimensions })).result
      .current
  const escapes = (node: SchemaNode): string[] =>
    node.children.flatMap((child) => [
      ...(isWithin(child.cidr, node.cidr) ? [] : [`${child.name} ${child.cidr} outside ${node.name} ${node.cidr}`]),
      ...escapes(child),
    ])
  const topLevelCount: Record<string, number> = {
    'region-first': plannerDimensions.regions.length,
    'environment-first': plannerDimensions.environments.length,
    'account-first': plannerDimensions.environments.length * plannerDimensions.accountsPerEnv,
  }
  const cases = BLUEPRINTS.filter((b) => b.id !== 'custom').flatMap((bp) =>
    Object.keys(topLevelCount).map((strategy) => [bp.id, bp.rootCidr, strategy, bp] as const),
  )

  it.each(cases)('%s (%s) %s keeps every child inside its parent', (_id, _root, strategy, bp) => {
    const { schema, error } = generate(bp, '', strategy)
    expect(error).toBeNull()
    expect(escapes(schema)).toEqual([])
    expect(schema.children.length).toBe(topLevelCount[strategy])
  })

  it('keeps children inside a custom root typed with host bits set', () => {
    const { schema } = generate(customBlueprint, '10.255.0.0/8', 'region-first')
    expect(escapes(schema)).toEqual([])
  })

  it('keeps the documented level sizes when they fit', () => {
    const { schema } = generate(enterpriseBlueprint, '', 'region-first')
    const region = schema.children[0]
    expect([region.cidr, region.children[0].cidr, region.children[0].children[0].cidr]).toEqual([
      '10.0.0.0/12',
      '10.0.0.0/16',
      '10.0.0.0/20',
    ])
  })

  it('reports a root too small for the strategy instead of emitting children', () => {
    const { schema, error } = generate(customBlueprint, '10.0.0.0/24', 'region-first')
    expect(error).toMatch('10.0.0.0/24')
    expect(schema.children).toEqual([])
  })
})
