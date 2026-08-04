import { ImportExportModal } from 'cloudpam-ui'

// Same containing-block trick as SearchModal: the dialog is `fixed`, so a
// transformed wrapper keeps the backdrop and panel inside the card.
export function Open() {
  return (
    <div style={{ position: 'relative', height: 544, width: '100%', transform: 'translateZ(0)' }}>
      <ImportExportModal open={true} onClose={() => {}} />
    </div>
  )
}
