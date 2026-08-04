import { useState } from 'react'
import { DimensionsStep } from 'cloudpam-ui'

const enterprise = {
  regions: ['us-east-1', 'us-west-2', 'eu-west-1'],
  environments: ['production', 'staging', 'development'],
  accountTiers: ['prd', 'stg', 'dev'],
  accountsPerEnv: 12,
  growthYears: 5,
}

const startup = {
  regions: ['us-east-1'],
  environments: ['production', 'development'],
  accountTiers: ['prd', 'dev'],
  accountsPerEnv: 2,
  growthYears: 3,
}

export function EnterpriseScale() {
  const [dimensions, setDimensions] = useState(enterprise)
  return <DimensionsStep dimensions={dimensions} setDimensions={setDimensions} strategy="region-first" />
}

export function SmallFootprint() {
  const [dimensions, setDimensions] = useState(startup)
  return <DimensionsStep dimensions={dimensions} setDimensions={setDimensions} strategy="environment-first" />
}
