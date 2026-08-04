import { SearchModal } from 'cloudpam-ui'

// SearchModal renders a `fixed` backdrop + centered panel. A transform on the
// wrapper makes it a containing block for those fixed children so the overlay
// fills the card instead of anchoring to the viewport and being clipped.
//
// The component owns its query state via useSearch and only fetches after the
// user types (300ms debounce), so a static card shows the opened palette and
// its empty-state hint. A results-populated card would need simulated
// keystrokes, which isn't statically renderable.
export function Open() {
  return (
    <div style={{ position: 'relative', height: 448, width: '100%', transform: 'translateZ(0)' }}>
      <SearchModal open={true} onClose={() => {}} />
    </div>
  )
}
