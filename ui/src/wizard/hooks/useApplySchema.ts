import { useState, useCallback } from 'react'
import { post } from '../../api/client'
import type { SchemaApplyResponse } from '../../api/types'
import type { SchemaNode } from '../utils/cidr'
import { buildApplyRequest } from '../utils/applyRequest'

export function useApplySchema() {
  const [result, setResult] = useState<SchemaApplyResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const apply = useCallback(async (schema: SchemaNode, skipConflicts = false): Promise<boolean> => {
    setLoading(true)
    setError(null)
    setResult(null)

    try {
      const res = await post<SchemaApplyResponse>('/api/v1/schema/apply', buildApplyRequest(schema, skipConflicts))
      setResult(res)
      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to apply schema')
      return false
    } finally {
      setLoading(false)
    }
  }, [])

  return { result, loading, error, apply }
}
