import { useMemo } from 'react'
import type { Blueprint } from '../data/blueprints'
import type { Dimensions } from '../steps/DimensionsStep'
import type { SchemaNode } from '../utils/cidr'
import { subdivide } from '../utils/cidr'

interface GeneratorInput {
  selectedBlueprint: Blueprint | null
  customCidr: string
  strategy: string
  dimensions: Dimensions
}

export function useSchemaGenerator({ selectedBlueprint, customCidr, strategy, dimensions }: GeneratorInput): SchemaNode {
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

    if (strategy === 'region-first') {
      const regionBlocks = subdivide(rootCidr, 12)
      dimensions.regions.forEach((region, ri) => {
        if (ri >= regionBlocks.length) return
        const regionNode: SchemaNode = {
          id: `region-${ri}`,
          name: region,
          type: 'region',
          cidr: regionBlocks[ri],
          children: [],
        }

        const envBlocks = subdivide(regionBlocks[ri], 16)
        dimensions.environments.forEach((env, ei) => {
          if (ei >= envBlocks.length) return
          const envNode: SchemaNode = {
            id: `region-${ri}-env-${ei}`,
            name: env,
            type: 'environment',
            cidr: envBlocks[ei],
            children: [],
          }

          const accountBlocks = subdivide(envBlocks[ei], 20)
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
      const envBlocks = subdivide(rootCidr, 12)
      dimensions.environments.forEach((env, ei) => {
        if (ei >= envBlocks.length) return
        const envNode: SchemaNode = {
          id: `env-${ei}`,
          name: env,
          type: 'environment',
          cidr: envBlocks[ei],
          children: [],
        }

        const accountBlocks = subdivide(envBlocks[ei], 16)
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
      // account-first
      const accountBlocks = subdivide(rootCidr, 16)
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

    return root
  }, [selectedBlueprint, customCidr, strategy, dimensions])
}
