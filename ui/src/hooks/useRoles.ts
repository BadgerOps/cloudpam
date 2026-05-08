import { useCallback, useEffect, useState } from 'react'
import { del, get, patch, post } from '../api/client'
import type { PermissionsListResponse, PermissionInfo, RoleInfo, RoleSaveRequest, RolesListResponse } from '../api/types'

export function permissionID(permission: { resource: string; action: string }) {
  return `${permission.resource}:${permission.action}`
}

export function useRoles() {
  const [roles, setRoles] = useState<RoleInfo[]>([])
  const [permissions, setPermissions] = useState<PermissionInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [roleData, permissionData] = await Promise.all([
        get<RolesListResponse>('/api/v1/auth/roles'),
        get<PermissionsListResponse>('/api/v1/auth/permissions'),
      ])
      setRoles(roleData.roles)
      setPermissions(permissionData.permissions)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load roles')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  const create = useCallback(async (payload: RoleSaveRequest) => {
    const role = await post<RoleInfo>('/api/v1/auth/roles', payload)
    await refresh()
    return role
  }, [refresh])

  const update = useCallback(async (name: string, payload: RoleSaveRequest) => {
    const role = await patch<RoleInfo>(`/api/v1/auth/roles/${encodeURIComponent(name)}`, payload)
    await refresh()
    return role
  }, [refresh])

  const remove = useCallback(async (name: string) => {
    await del(`/api/v1/auth/roles/${encodeURIComponent(name)}`)
    await refresh()
  }, [refresh])

  return { roles, permissions, loading, error, refresh, create, update, remove }
}
