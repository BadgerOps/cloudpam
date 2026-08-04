import { Header } from 'cloudpam-ui'

// Only one cell: the `sidebarOpen` menu toggle is viewport-gated (hidden at
// desktop widths), so a second card at preview width would be pixel-identical
// to this one.
export function Default() {
  return <Header onSearchClick={() => {}} onMenuClick={() => {}} sidebarOpen={true} />
}
