import { useState, useEffect, useCallback } from 'react'
import { post } from '../../api/client'
import type { SchemaPoolEntry, SchemaCheckRequest, SchemaCheckResponse, SchemaConflict } from '../../api/types'
import type { SchemaNode } from '../utils/cidr'

function flattenSchema(node: SchemaNode, parentRef: string | null = null): SchemaPoolEntry[] {
  const entry: SchemaPoolEntry = {
    ref: node.id,
    name: node.name,
    cidr: node.cidr,
    type: node.type,
    parent_ref: parentRef,
  }
  const result: SchemaPoolEntry[] = [entry]
  for (const child of node.children) {
    result.push(...flattenSchema(child, node.id))
  }
  return result
}

export function useConflictChecker(schema: SchemaNode | null, enabled: boolean) {
  const [conflicts, setConflicts] = useState<SchemaConflict[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const check = useCallback(async () => {
    if (!schema || !enabled) return

    setLoading(true)
    setError(null)

    try {
      const pools = flattenSchema(schema)
      const req: SchemaCheckRequest = { pools }
      const res = await post<SchemaCheckResponse>('/api/v1/schema/check', req)
      setConflicts(res.conflicts ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to check conflicts')
      setConflicts([])
    } finally {
      setLoading(false)
    }
  }, [schema, enabled])

  useEffect(() => {
    check()
  }, [check])

  return { conflicts, loading, error, recheck: check }
}
