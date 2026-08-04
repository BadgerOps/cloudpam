import { Sidebar, AuthContext, previewAuth } from 'cloudpam-ui'

export function AdminNavigation() {
  return (
    <div style={{ height: 1000 }}>
      <Sidebar onImportExport={() => {}} open={true} onClose={() => {}} />
    </div>
  )
}

// Nav entries are permission-gated: settings, users and audit only appear for
// principals holding the matching permission. An operator without them sees a
// visibly shorter rail.
export function OperatorNavigation() {
  const operator = {
    ...previewAuth,
    role: 'operator',
    permissions: ['pools:read', 'accounts:read'],
    hasPermission: (p: string) => ['pools:read', 'accounts:read'].includes(p),
  }
  return (
    <AuthContext.Provider value={operator}>
      <div style={{ height: 1000 }}>
        <Sidebar onImportExport={() => {}} open={true} onClose={() => {}} />
      </div>
    </AuthContext.Provider>
  )
}
