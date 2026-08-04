import { ProtectedRoute, AuthContext, previewAuth, Routes, Route } from 'cloudpam-ui'

// ProtectedRoute renders no UI of its own — it either passes through to the
// nested route via <Outlet> or redirects. Each cell therefore shows the
// *outcome* of the guard against a small route tree, which is the only way to
// see it work. Note the two failure modes land in different places: a missing
// permission redirects to "/", while an unauthenticated session goes to
// "/login". The shared provider starts the router at /pools, so the guard is
// mounted there.
const Box = ({ tone, title, body }: { tone: 'green' | 'amber' | 'red'; title: string; body: string }) => {
  const palette = {
    green: { border: '#bbf7d0', background: '#f0fdf4', title: '#166534', body: '#15803d' },
    amber: { border: '#fde68a', background: '#fffbeb', title: '#92400e', body: '#b45309' },
    red: { border: '#fecaca', background: '#fef2f2', title: '#991b1b', body: '#b91c1c' },
  }[tone]
  return (
    <div
      style={{
        padding: 16,
        border: `1px solid ${palette.border}`,
        background: palette.background,
        borderRadius: 8,
      }}
    >
      <div style={{ fontSize: 14, fontWeight: 500, color: palette.title }}>{title}</div>
      <div style={{ fontSize: 14, color: palette.body }}>{body}</div>
    </div>
  )
}

function Tree() {
  return (
    <Routes>
      <Route path="/pools" element={<ProtectedRoute requiredPermission="users:list" />}>
        <Route index element={<Box tone="green" title="Protected page rendered" body="The session satisfied the guard, so <Outlet> renders the nested route." />} />
      </Route>
      <Route path="/" element={<Box tone="amber" title="Redirected to /" body="Authenticated, but missing the required permission." />} />
      <Route path="/login" element={<Box tone="red" title="Redirected to /login" body="No authenticated session while auth is enabled." />} />
    </Routes>
  )
}

export function PermissionGranted() {
  return <Tree />
}

export function PermissionDenied() {
  const viewer = {
    ...previewAuth,
    role: 'viewer',
    permissions: ['pools:read'],
    hasPermission: (p: string) => p === 'pools:read',
  }
  return (
    <AuthContext.Provider value={viewer}>
      <Tree />
    </AuthContext.Provider>
  )
}

export function NotAuthenticated() {
  const anon = { ...previewAuth, isAuthenticated: false, role: null, permissions: [], hasPermission: () => false }
  return (
    <AuthContext.Provider value={anon}>
      <Tree />
    </AuthContext.Provider>
  )
}
