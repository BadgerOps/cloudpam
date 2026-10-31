import { describe, expect, it } from 'vitest'
import { flattenForApply } from '../wizard/hooks/useApplySchema'
import { flattenSchema } from '../wizard/hooks/useConflictChecker'
import type { SchemaNode } from '../wizard/utils/cidr'

const schema: SchemaNode = {
  id: 'root',
  name: 'Root',
  cidr: '10.0.0.0/8',
  type: 'supernet',
  children: [
    {
      id: 'child',
      name: 'Child',
      cidr: '10.0.0.0/16',
      type: 'region',
      children: [],
    },
  ],
}

describe('schema flattening', () => {
  it('omits parent_ref for roots and includes it for children', () => {
    for (const entries of [flattenSchema(schema), flattenForApply(schema, null)]) {
      expect(entries).toHaveLength(2)
      expect(entries[0]).not.toHaveProperty('parent_ref')
      expect(entries[1]).toHaveProperty('parent_ref', 'root')
    }
  })
})
