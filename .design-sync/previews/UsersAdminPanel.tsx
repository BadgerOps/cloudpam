import { UsersAdminPanel } from 'cloudpam-ui'

// UsersAdminPanel loads /api/v1/auth/users, /auth/roles and /auth/permissions.
// With no CloudPAM backend behind the preview those 404 and the panel renders
// two red error banners over an empty table. Serving the payloads the real API
// returns is what makes the populated table visible; the component is unchanged.
if (!(globalThis as { __dsUsersStub?: boolean }).__dsUsersStub) {
  ;(globalThis as { __dsUsersStub?: boolean }).__dsUsersStub = true
  const real = globalThis.fetch
  const json = (body: unknown) =>
    Promise.resolve(
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
  const users = [
    {
      id: 'u-1024',
      username: 'dana.reyes',
      email: 'dana.reyes@example.com',
      display_name: 'Dana Reyes',
      role: 'admin',
      is_active: true,
      created_at: '2026-01-14T09:20:00Z',
      last_login_at: '2026-08-01T08:12:00Z',
      failed_login_attempts: 0,
    },
    {
      id: 'u-1088',
      username: 'sam.okafor',
      email: 'sam.okafor@example.com',
      display_name: 'Sam Okafor',
      role: 'operator',
      is_active: true,
      created_at: '2026-03-02T11:05:00Z',
      last_login_at: '2026-07-31T17:44:00Z',
      failed_login_attempts: 0,
    },
    {
      id: 'u-1131',
      username: 'ci-pipeline',
      email: 'ci@example.com',
      display_name: 'CI Pipeline',
      role: 'viewer',
      is_active: false,
      created_at: '2026-05-19T13:30:00Z',
      last_login_at: null,
      failed_login_attempts: 0,
    },
    {
      id: 'u-1150',
      username: 'jules.tan',
      email: 'jules.tan@example.com',
      display_name: 'Jules Tan',
      role: 'operator',
      is_active: true,
      created_at: '2026-06-08T08:00:00Z',
      last_login_at: '2026-07-20T09:02:00Z',
      failed_login_attempts: 5,
      locked_at: '2026-07-20T09:05:00Z',
      lockout_until: '2026-08-03T09:05:00Z',
    },
  ]
  const roles = ['admin', 'operator', 'viewer'].map((r) => ({
    id: r,
    name: r,
    description: `Built-in ${r} role`,
    is_builtin: true,
    permissions: [],
  }))
  globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(typeof input === 'string' ? input : (input as Request).url ?? input)
    if (url.includes('/api/v1/auth/users')) return json({ users })
    if (url.includes('/api/v1/auth/roles')) return json({ roles })
    if (url.includes('/api/v1/auth/permissions')) return json({ permissions: [] })
    return real(input, init)
  }) as typeof fetch
}

export function UserList() {
  return <UsersAdminPanel />
}

export function Embedded() {
  return <UsersAdminPanel embedded={true} />
}
