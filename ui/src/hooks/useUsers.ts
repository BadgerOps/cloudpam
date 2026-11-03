import { useState, useEffect, useCallback } from 'react'
import { get, post, patch, del } from '../api/client'
import { useAuth } from './useAuth'
import { useToast } from './useToast'
import type {
  UserInfo,
  CreateUserRequest,
  UpdateUserRequest,
  ChangePasswordRequest,
  UsersListResponse,
} from '../api/types'

/**
 * useChangePassword exposes only the self-service password change call.
 *
 * PATCH /api/v1/auth/users/{id}/password is allowed for the account owner,
 * while listing users requires users:list. Screens that only change a password
 * must not pull useUsers, or every visit fires a privileged list request that
 * 403s for non-admins.
 */
export function useChangePassword() {
  return useCallback(async (id: string, req: ChangePasswordRequest) => {
    await patch(`/api/v1/auth/users/${id}/password`, req)
  }, [])
}

/**
 * useSignOutEverywhere revokes every session of one user after confirmation
 * and reports the outcome as a toast.
 *
 * POST /api/v1/auth/users/{id}/revoke-sessions is allowed for admins and for
 * the account owner, so, like useChangePassword, it stays separate from the
 * privileged list in useUsers. Revoking your own sessions also ends this one,
 * so the hook signs this browser out instead of leaving a dead session that
 * only fails on the next request.
 */
export function useSignOutEverywhere() {
  const { currentUser, logout } = useAuth()
  const { showToast } = useToast()
  return useCallback(async (user: Pick<UserInfo, 'id' | 'username'>) => {
    const self = currentUser?.id === user.id
    const prompt = self
      ? 'Sign out everywhere? Every active session for your account is revoked, including this one.'
      : `Sign ${user.username} out everywhere? Every active session for this user is revoked and they must sign in again.`
    if (!window.confirm(prompt)) return
    try {
      await post(`/api/v1/auth/users/${user.id}/revoke-sessions`, {})
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to revoke sessions', 'error')
      return
    }
    if (self) {
      showToast('Signed out everywhere. Sign in again to continue.', 'success')
      await logout()
      return
    }
    showToast(`Signed ${user.username} out everywhere`, 'success')
  }, [currentUser?.id, logout, showToast])
}

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

  const unlock = useCallback(async (id: string): Promise<UserInfo> => {
    const res = await post<UserInfo>(`/api/v1/auth/users/${id}/unlock`, {})
    await refresh()
    return res
  }, [refresh])

  const changePassword = useCallback(async (id: string, req: ChangePasswordRequest) => {
    await patch(`/api/v1/auth/users/${id}/password`, req)
  }, [])

  return { users, loading, error, create, update, deactivate, unlock, changePassword, refresh }
}
