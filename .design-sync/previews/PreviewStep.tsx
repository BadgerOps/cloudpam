import { PreviewStep } from 'cloudpam-ui'
import type { SchemaNode, SchemaConflict } from 'cloudpam-ui'

const node = (
  id: string,
  name: string,
  type: SchemaNode['type'],
  cidr: string,
  children: SchemaNode[] = [],
  conflict = false,
): SchemaNode => ({ id, name, type, cidr, children, conflict })

const schema = node('root', 'Corporate Supernet', 'supernet', '10.0.0.0/8', [
  node('r1', 'us-east-1', 'region', '10.0.0.0/12', [
    node('e1', 'production', 'environment', '10.0.0.0/16', [
      node('v1', 'app-vpc', 'vpc', '10.0.0.0/18'),
      node('v2', 'data-vpc', 'vpc', '10.0.64.0/18'),
    ]),
    node('e2', 'staging', 'environment', '10.1.0.0/16'),
  ]),
  node('r2', 'eu-west-1', 'region', '10.16.0.0/12', [
    node('e3', 'production', 'environment', '10.16.0.0/16'),
  ]),
])

const conflicts: SchemaConflict[] = [
  {
    planned_cidr: '10.16.0.0/16',
    planned_name: 'eu-west-1 / production',
    existing_pool_id: 314,
    existing_pool_name: 'frankfurt-legacy',
    existing_cidr: '10.16.0.0/18',
    overlap_type: 'contains',
  },
]

export function CleanPlan() {
  return (
    <PreviewStep
      schema={schema}
      conflicts={[]}
      conflictsLoading={false}
      conflictsError={null}
      onExport={() => {}}
    />
  )
}

export function WithConflicts() {
  return (
    <PreviewStep
      schema={schema}
      conflicts={conflicts}
      conflictsLoading={false}
      conflictsError={null}
      onExport={() => {}}
    />
  )
}

export function CheckingConflicts() {
  return (
    <PreviewStep
      schema={schema}
      conflicts={[]}
      conflictsLoading={true}
      conflictsError={null}
      onExport={() => {}}
    />
  )
}

export function ConflictCheckFailed() {
  return (
    <PreviewStep
      schema={schema}
      conflicts={[]}
      conflictsLoading={false}
      conflictsError="Could not reach the conflict checker (503)."
      onExport={() => {}}
    />
  )
}
