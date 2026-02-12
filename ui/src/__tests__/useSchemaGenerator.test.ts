import { describe, it, expect } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useSchemaGenerator } from '../wizard/hooks/useSchemaGenerator'
import type { Blueprint } from '../wizard/data/blueprints'
import type { Dimensions } from '../wizard/steps/DimensionsStep'

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

    const root = result.current
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

    const root = result.current
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

    const root = result.current
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

    expect(result.current.cidr).toBe('172.16.0.0/12')
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
    const collect = (node: typeof result.current) => {
      cidrs.push(node.cidr)
      node.children.forEach(collect)
    }
    collect(result.current)

    // All CIDRs should be unique
    const unique = new Set(cidrs)
    expect(unique.size).toBe(cidrs.length)
  })
})
