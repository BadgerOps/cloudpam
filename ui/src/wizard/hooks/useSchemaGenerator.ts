import { useMemo } from 'react'
import type { Blueprint } from '../data/blueprints'
import type { Dimensions } from '../steps/DimensionsStep'
import type { SchemaNode } from '../utils/cidr'
import { parse, subdivide } from '../utils/cidr'

interface GeneratorInput {
  selectedBlueprint: Blueprint | null
  customCidr: string
  strategy: string
  dimensions: Dimensions
}

export interface GeneratedSchema {
  schema: SchemaNode
  error: string | null
}

const MAX_POOL_PREFIX = 30

const LEVEL_PREFIXES: Record<string, number[]> = {
  'region-first': [12, 16, 20],
  'environment-first': [12, 16],
  'account-first': [16],
}

function levelPrefixes(rootPrefix: number, strategy: string): number[] {
  let parent = rootPrefix
  return (LEVEL_PREFIXES[strategy] ?? LEVEL_PREFIXES['account-first']).map((preferred) => {
    parent = preferred > parent ? preferred : parent + 4
    return parent
  })
}

export function useSchemaGenerator({ selectedBlueprint, customCidr, strategy, dimensions }: GeneratorInput): GeneratedSchema {
  return useMemo(() => {
    const rootCidr =
      selectedBlueprint?.id === 'custom'
        ? customCidr || '10.0.0.0/8'
        : selectedBlueprint?.rootCidr || '10.0.0.0/8'

    const root: SchemaNode = {
      id: 'root',
      name: 'Root',
      type: 'supernet',
      cidr: rootCidr,
      children: [],
    }

    const levels = levelPrefixes(parse(rootCidr).prefixLen, strategy)
    const deepest = levels[levels.length - 1]
    if (deepest > MAX_POOL_PREFIX) {
      return {
        schema: root,
        error: `${rootCidr} is too small for this strategy. Its deepest level needs /${deepest} blocks, and a pool can be at most /${MAX_POOL_PREFIX}.`,
      }
    }

    if (strategy === 'region-first') {
      const regionBlocks = subdivide(rootCidr, levels[0])
      dimensions.regions.forEach((region, ri) => {
        if (ri >= regionBlocks.length) return
        const regionNode: SchemaNode = {
          id: `region-${ri}`,
          name: region,
          type: 'region',
          cidr: regionBlocks[ri],
          children: [],
        }

        const envBlocks = subdivide(regionBlocks[ri], levels[1])
        dimensions.environments.forEach((env, ei) => {
          if (ei >= envBlocks.length) return
          const envNode: SchemaNode = {
            id: `region-${ri}-env-${ei}`,
            name: env,
            type: 'environment',
            cidr: envBlocks[ei],
            children: [],
          }

          const accountBlocks = subdivide(envBlocks[ei], levels[2])
          for (let ai = 0; ai < Math.min(dimensions.accountsPerEnv, accountBlocks.length); ai++) {
            envNode.children.push({
              id: `region-${ri}-env-${ei}-acct-${ai}`,
              name: `${dimensions.accountTiers[ai % dimensions.accountTiers.length]}-${ai + 1}`,
              type: 'vpc',
              cidr: accountBlocks[ai],
              children: [],
            })
          }

          regionNode.children.push(envNode)
        })

        root.children.push(regionNode)
      })
    } else if (strategy === 'environment-first') {
      const envBlocks = subdivide(rootCidr, levels[0])
      dimensions.environments.forEach((env, ei) => {
        if (ei >= envBlocks.length) return
        const envNode: SchemaNode = {
          id: `env-${ei}`,
          name: env,
          type: 'environment',
          cidr: envBlocks[ei],
          children: [],
        }

        const accountBlocks = subdivide(envBlocks[ei], levels[1])
        for (
          let ai = 0;
          ai < Math.min(dimensions.accountsPerEnv * dimensions.regions.length, accountBlocks.length);
          ai++
        ) {
          envNode.children.push({
            id: `env-${ei}-acct-${ai}`,
            name: `${dimensions.accountTiers[ai % dimensions.accountTiers.length]}-${ai + 1}`,
            type: 'vpc',
            cidr: accountBlocks[ai],
            children: [],
          })
        }

        root.children.push(envNode)
      })
    } else {
      // account-first. Each block is one account's allocation, so the names say
      // "account" while the type says 'vpc': 'account' is a search-result kind,
      // not a pool type, and only POOL_TYPES ids render with a colour. The
      // region-first branch above does the same thing with tier-based names.
      const accountBlocks = subdivide(rootCidr, levels[0])
      const totalAccounts = dimensions.environments.length * dimensions.accountsPerEnv
      for (let ai = 0; ai < Math.min(totalAccounts, accountBlocks.length); ai++) {
        root.children.push({
          id: `acct-${ai}`,
          name: `account-${String(ai + 1).padStart(3, '0')}`,
          type: 'vpc',
          cidr: accountBlocks[ai],
          children: [],
        })
      }
    }

    return { schema: root, error: null }
  }, [selectedBlueprint, customCidr, strategy, dimensions])
}
