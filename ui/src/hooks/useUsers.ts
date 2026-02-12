import { useState, useEffect, useCallback } from 'react'
import { get, post, patch, del } from '../api/client'
import type {
  UserInfo,
  CreateUserRequest,
  UpdateUserRequest,
  ChangePasswordRequest,
  UsersListResponse,
} from '../api/types'

export function useUsers() {
  const [users, setUsers] = useState<UserInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await get<UsersListResponse>('/api/v1/auth/users')
      setUsers(data.users ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load users')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { refresh() }, [refresh])

  const create = useCallback(async (req: CreateUserRequest): Promise<UserInfo> => {
    const res = await post<UserInfo>('/api/v1/auth/users', req)
    await refresh()
    return res
  }, [refresh])

  const update = useCallback(async (id: string, req: UpdateUserRequest): Promise<UserInfo> => {
    const res = await patch<UserInfo>(`/api/v1/auth/users/${id}`, req)
    await refresh()
    return res
  }, [refresh])

  const deactivate = useCallback(async (id: string) => {
    await del(`/api/v1/auth/users/${id}`)
    await refresh()
  }, [refresh])

  const changePassword = useCallback(async (id: string, req: ChangePasswordRequest) => {
    await patch(`/api/v1/auth/users/${id}/password`, req)
  }, [])

  return { users, loading, error, create, update, deactivate, changePassword, refresh }
}
