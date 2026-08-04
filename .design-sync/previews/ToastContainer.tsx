import { ToastContainer, ToastContext } from 'cloudpam-ui'

// ToastContainer reads its list from ToastContext and renders nothing when the
// list is empty. The shared preview provider deliberately supplies an empty
// list (so every other card isn't covered in floating toasts), so this preview
// nests its own provider to show the real populated states.
// The container is `fixed right-4 bottom-4`, so in a preview card it would
// anchor to the viewport and be clipped. A transform on the wrapper makes it a
// containing block for fixed descendants, so the real component renders in
// place with its own positioning intact.
const withToasts = (toasts: Array<{ id: string; message: string; type: 'info' | 'error' | 'success' }>) => (
  <ToastContext.Provider value={{ toasts, showToast: () => {} }}>
    <div style={{ position: 'relative', height: 224, width: '100%', transform: 'translateZ(0)' }}>
      <ToastContainer />
    </div>
  </ToastContext.Provider>
)

export function Success() {
  return withToasts([{ id: '1', message: 'Pool "production" created.', type: 'success' }])
}

export function Error() {
  return withToasts([
    { id: '2', message: 'Could not delete pool: 12 child pools still assigned.', type: 'error' },
  ])
}

export function Info() {
  return withToasts([{ id: '3', message: 'Discovery sync queued for 4 accounts.', type: 'info' }])
}

export function Stacked() {
  return withToasts([
    { id: '4', message: 'Schema applied — 18 pools created.', type: 'success' },
    { id: '5', message: 'Drift detected in us-west-2.', type: 'info' },
    { id: '6', message: 'Import failed on row 37.', type: 'error' },
  ])
}
