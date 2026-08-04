import { StatusBadge } from 'cloudpam-ui'

const Row = ({ children }: { children: React.ReactNode }) => (
  <div className="flex flex-wrap items-center gap-2">{children}</div>
)

export function PoolStatus() {
  return (
    <Row>
      <StatusBadge label="active" />
      <StatusBadge label="planned" />
      <StatusBadge label="deprecated" />
    </Row>
  )
}

export function CloudProvider() {
  return (
    <Row>
      <StatusBadge label="aws" variant="provider" />
      <StatusBadge label="gcp" variant="provider" />
      <StatusBadge label="azure" variant="provider" />
      <StatusBadge label="on-prem" variant="provider" />
    </Row>
  )
}

export function AccountTier() {
  return (
    <Row>
      <StatusBadge label="prd" variant="tier" />
      <StatusBadge label="stg" variant="tier" />
      <StatusBadge label="dev" variant="tier" />
      <StatusBadge label="sbx" variant="tier" />
    </Row>
  )
}

export function AuditAction() {
  return (
    <Row>
      <StatusBadge label="pool.create" variant="action" />
      <StatusBadge label="pool.update" variant="action" />
      <StatusBadge label="account.delete" variant="action" />
    </Row>
  )
}

export function PoolType() {
  return (
    <Row>
      <StatusBadge label="supernet" variant="type" />
      <StatusBadge label="region" variant="type" />
      <StatusBadge label="environment" variant="type" />
      <StatusBadge label="vpc" variant="type" />
      <StatusBadge label="subnet" variant="type" />
    </Row>
  )
}
