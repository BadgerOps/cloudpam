import { TreeNode } from 'cloudpam-ui'
import type { SchemaNode } from 'cloudpam-ui'

const node = (
  id: string,
  name: string,
  type: SchemaNode['type'],
  cidr: string,
  children: SchemaNode[] = [],
  conflict = false,
): SchemaNode => ({ id, name, type, cidr, children, conflict })

const plannedRegion = node('r1', 'us-east-1', 'region', '10.0.0.0/12', [
  node('e1', 'production', 'environment', '10.0.0.0/16', [
    node('v1', 'app-vpc', 'vpc', '10.0.0.0/18'),
    node('v2', 'data-vpc', 'vpc', '10.0.64.0/18'),
  ]),
  node('e2', 'staging', 'environment', '10.1.0.0/16', [node('v3', 'app-vpc', 'vpc', '10.1.0.0/18')]),
])

const Frame = ({ children }: { children: React.ReactNode }) => (
  <div style={{ maxWidth: 576, fontSize: 14 }}>{children}</div>
)

// An empty set collapses every node. Omitting defaultExpanded entirely instead
// auto-expands anything above depth 2.
export function Collapsed() {
  return (
    <Frame>
      <TreeNode node={plannedRegion} defaultExpanded={new Set()} />
    </Frame>
  )
}

// defaultExpanded takes the set of node ids that start open.
export function Expanded() {
  return (
    <Frame>
      <TreeNode node={plannedRegion} defaultExpanded={new Set(['r1', 'e1', 'e2'])} />
    </Frame>
  )
}

export function WithConflict() {
  return (
    <Frame>
      <TreeNode
        node={node('r2', 'eu-west-1', 'region', '10.16.0.0/12', [
          node('e3', 'production', 'environment', '10.16.0.0/16', [], true),
          node('e4', 'staging', 'environment', '10.17.0.0/16'),
        ])}
        defaultExpanded={new Set(['r2'])}
      />
    </Frame>
  )
}

export function LeafSubnet() {
  return (
    <Frame>
      <TreeNode node={node('s1', 'private-a', 'subnet', '10.0.1.0/24')} />
    </Frame>
  )
}
