import { useState, useCallback } from 'react'
import { get, post, del } from '../api/client'
import type {
  DiscoveryResourcesResponse,
  DiscoveryAgent,
  DiscoveryAgentsResponse,
  SyncJob,
  SyncJobsResponse,
  AgentProvisionResponse,
} from '../api/types'

export function useDiscoveryResources() {
  const [data, setData] = useState<DiscoveryResourcesResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetch = useCallback(
    async (
      accountId: number,
      filters?: {
        provider?: string
        region?: string
        resource_type?: string
        status?: string
        linked?: string
        page?: number
        page_size?: number
      },
    ) => {
      setLoading(true)
      setError(null)
      try {
        const params = new URLSearchParams({ account_id: String(accountId) })
        if (filters?.provider) params.set('provider', filters.provider)
        if (filters?.region) params.set('region', filters.region)
        if (filters?.resource_type)
          params.set('resource_type', filters.resource_type)
        if (filters?.status) params.set('status', filters.status)
        if (filters?.linked) params.set('linked', filters.linked)
        if (filters?.page) params.set('page', String(filters.page))
        if (filters?.page_size)
          params.set('page_size', String(filters.page_size))

        const resp = await get<DiscoveryResourcesResponse>(
          `/api/v1/discovery/resources?${params}`,
        )
        setData(resp)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load resources')
      } finally {
        setLoading(false)
      }
    },
    [],
  )

  const linkToPool = useCallback(
    async (resourceId: string, poolId: number) => {
      await post(`/api/v1/discovery/resources/${resourceId}/link`, {
        pool_id: poolId,
      })
    },
    [],
  )

  const unlinkFromPool = useCallback(async (resourceId: string) => {
    await del(`/api/v1/discovery/resources/${resourceId}/link`)
  }, [])

  return { data, loading, error, fetch, linkToPool, unlinkFromPool }
}

export function useSyncJobs() {
  const [jobs, setJobs] = useState<SyncJob[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetch = useCallback(async (accountId: number) => {
    setLoading(true)
    setError(null)
    try {
      const resp = await get<SyncJobsResponse>(
        `/api/v1/discovery/sync?account_id=${accountId}`,
      )
      setJobs(resp.items)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load sync jobs')
    } finally {
      setLoading(false)
    }
  }, [])

  const triggerSync = useCallback(async (accountId: number) => {
    const job = await post<SyncJob>('/api/v1/discovery/sync', {
      account_id: accountId,
    })
    return job
  }, [])

  return { jobs, loading, error, fetch, triggerSync }
}

export function useAgentProvisioning() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<AgentProvisionResponse | null>(null)

  const provision = useCallback(async (name: string) => {
    setLoading(true)
    setError(null)
    try {
      const resp = await post<AgentProvisionResponse>(
        '/api/v1/discovery/agents/provision',
        { name },
      )
      setResult(resp)
      return resp
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to provision agent'
      setError(msg)
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { result, loading, error, provision }
}

export function useDiscoveryAgents() {
  const [agents, setAgents] = useState<DiscoveryAgent[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetch = useCallback(async (accountId?: number) => {
    setLoading(true)
    setError(null)
    try {
      const params = accountId
        ? new URLSearchParams({ account_id: String(accountId) })
        : ''
      const resp = await get<DiscoveryAgentsResponse>(
        `/api/v1/discovery/agents${params ? '?' + params : ''}`,
      )
      setAgents(resp.items)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load agents')
    } finally {
      setLoading(false)
    }
  }, [])

  return { agents, loading, error, fetch }
}
