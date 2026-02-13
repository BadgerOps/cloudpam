import { useState } from 'react'
import { Users, Plus, AlertCircle, UserCheck, UserX } from 'lucide-react'
import { useUsers } from '../hooks/useUsers'
import type { CreateUserRequest } from '../api/types'

const ROLE_OPTIONS = ['admin', 'operator', 'viewer', 'auditor']

export default function UsersPage() {
  const { users, loading, error, create, update, deactivate } = useUsers()
  const [showCreate, setShowCreate] = useState(false)
  const [form, setForm] = useState<CreateUserRequest>({
    username: '',
    email: '',
    display_name: '',
    role: 'viewer',
    password: '',
  })
  const [createError, setCreateError] = useState('')
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editRole, setEditRole] = useState('')

  async function handleCreate() {
    if (!form.username.trim() || !form.password) return
    setCreateError('')
    try {
      await create(form)
      setForm({ username: '', email: '', display_name: '', role: 'viewer', password: '' })
      setShowCreate(false)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : 'Failed to create user')
    }
  }

  async function handleRoleSave(id: string) {
    try {
      await update(id, { role: editRole })
      setEditingId(null)
    } catch {
      // Error handled by hook
    }
  }

  async function handleToggleActive(id: string, isActive: boolean) {
    if (isActive) {
      await deactivate(id)
    } else {
      await update(id, { is_active: true })
    }
  }

  function formatDate(d?: string | null) {
    if (!d) return '—'
    return new Date(d).toLocaleDateString()
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Users</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Manage local user accounts
          </p>
        </div>
        <button
          onClick={() => setShowCreate(true)}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700"
        >
          <Plus className="w-4 h-4" />
          Create User
        </button>
      </div>

      {error && (
        <div className="mb-4 flex items-center gap-2 text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 px-4 py-3 rounded-lg">
          <AlertCircle className="w-4 h-4 flex-shrink-0" />
          {error}
        </div>
      )}

      {/* Create form */}
      {showCreate && (
        <div className="mb-6 bg-white dark:bg-gray-800 rounded-lg shadow p-4 border dark:border-gray-700">
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-3">New User</h3>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Username</label>
              <input
                value={form.username}
                onChange={e => setForm(f => ({ ...f, username: e.target.value }))}
                placeholder="jdoe"
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
                autoFocus
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Email</label>
              <input
                type="email"
                value={form.email}
                onChange={e => setForm(f => ({ ...f, email: e.target.value }))}
                placeholder="jdoe@example.com"
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Display Name</label>
              <input
                value={form.display_name}
                onChange={e => setForm(f => ({ ...f, display_name: e.target.value }))}
                placeholder="John Doe"
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Role</label>
              <select
                value={form.role}
                onChange={e => setForm(f => ({ ...f, role: e.target.value }))}
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              >
                {ROLE_OPTIONS.map(r => (
                  <option key={r} value={r}>{r.charAt(0).toUpperCase() + r.slice(1)}</option>
                ))}
              </select>
            </div>
            <div className="col-span-2">
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Password</label>
              <input
                type="password"
                value={form.password}
                onChange={e => setForm(f => ({ ...f, password: e.target.value }))}
                placeholder="Minimum 8 characters"
                className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>

          {createError && (
            <p className="mt-2 text-sm text-red-600 dark:text-red-400">{createError}</p>
          )}

          <div className="flex gap-2 pt-3">
            <button
              onClick={handleCreate}
              disabled={!form.username.trim() || !form.password}
              className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
            >
              Create
            </button>
            <button
              onClick={() => setShowCreate(false)}
              className="px-4 py-2 text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 text-sm"
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Users table */}
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
              <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Username</th>
              <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Email</th>
              <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Role</th>
              <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Status</th>
              <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Last Login</th>
              <th className="text-right px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Actions</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-gray-500 dark:text-gray-400">
                  Loading...
                </td>
              </tr>
            ) : users.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-gray-500 dark:text-gray-400">
                  <Users className="w-8 h-8 mx-auto mb-2 opacity-40" />
                  No users found
                </td>
              </tr>
            ) : (
              users.map(u => (
                <tr key={u.id} className="border-b dark:border-gray-700 last:border-0 hover:bg-gray-50 dark:hover:bg-gray-750">
                  <td className="px-4 py-3">
                    <div>
                      <span className="font-medium text-gray-900 dark:text-white">{u.username}</span>
                      {u.display_name && (
                        <span className="ml-2 text-gray-500 dark:text-gray-400">({u.display_name})</span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-gray-600 dark:text-gray-400">{u.email || '—'}</td>
                  <td className="px-4 py-3">
                    {editingId === u.id ? (
                      <div className="flex items-center gap-1">
                        <select
                          value={editRole}
                          onChange={e => setEditRole(e.target.value)}
                          className="px-2 py-1 border rounded text-xs dark:bg-gray-700 dark:border-gray-600 dark:text-white"
                        >
                          {ROLE_OPTIONS.map(r => (
                            <option key={r} value={r}>{r.charAt(0).toUpperCase() + r.slice(1)}</option>
                          ))}
                        </select>
                        <button
                          onClick={() => handleRoleSave(u.id)}
                          className="px-2 py-1 bg-blue-600 text-white rounded text-xs hover:bg-blue-700"
                        >
                          Save
                        </button>
                        <button
                          onClick={() => setEditingId(null)}
                          className="px-2 py-1 text-gray-500 text-xs hover:text-gray-700"
                        >
                          Cancel
                        </button>
                      </div>
                    ) : (
                      <button
                        onClick={() => { setEditingId(u.id); setEditRole(u.role) }}
                        className="px-2 py-0.5 bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 rounded text-xs font-medium uppercase hover:bg-blue-200 dark:hover:bg-blue-900/50"
                        title="Click to change role"
                      >
                        {u.role}
                      </button>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    {u.is_active ? (
                      <span className="px-2 py-0.5 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400 rounded text-xs font-medium">
                        Active
                      </span>
                    ) : (
                      <span className="px-2 py-0.5 bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400 rounded text-xs font-medium">
                        Disabled
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-gray-600 dark:text-gray-400">{formatDate(u.last_login_at)}</td>
                  <td className="px-4 py-3 text-right">
                    <button
                      onClick={() => handleToggleActive(u.id, u.is_active)}
                      title={u.is_active ? 'Deactivate user' : 'Activate user'}
                      className="p-1.5 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
                    >
                      {u.is_active ? <UserX className="w-4 h-4" /> : <UserCheck className="w-4 h-4" />}
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
