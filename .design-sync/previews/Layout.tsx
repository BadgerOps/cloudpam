import { Layout, Routes, Route } from 'cloudpam-ui'

// Layout is the authenticated frame and renders its page content through an
// <Outlet>. Mounted bare it shows the chrome around an empty hole, so the
// preview supplies a route tree — the only composition that shows what the
// shell actually looks like in use.
function SamplePage() {
  return (
    <div style={{ padding: 24 }}>
      <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Address Pools</h1>
      <p className="text-gray-600 dark:text-gray-300">
        Page content renders here, inside Layout's outlet.
      </p>
    </div>
  )
}

export function AppFrame() {
  return (
    <div style={{ height: 800 }}>
      <Routes>
        <Route element={<Layout />}>
          <Route path="*" element={<SamplePage />} />
        </Route>
      </Routes>
    </div>
  )
}
