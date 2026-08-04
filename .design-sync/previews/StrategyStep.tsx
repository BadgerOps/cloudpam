import { StrategyStep } from 'cloudpam-ui'

export function RegionFirst() {
  return <StrategyStep strategy="region-first" setStrategy={() => {}} />
}

export function EnvironmentFirst() {
  return <StrategyStep strategy="environment-first" setStrategy={() => {}} />
}

export function AccountFirst() {
  return <StrategyStep strategy="account-first" setStrategy={() => {}} />
}
