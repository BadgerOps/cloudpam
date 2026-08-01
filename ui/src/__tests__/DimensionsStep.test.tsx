import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import DimensionsStep from '../wizard/steps/DimensionsStep'
import type { Dimensions } from '../wizard/steps/DimensionsStep'

const dimensions: Dimensions = {
  regions: ['us-east-1', 'eu-west-1', 'ap-south-1'],
  environments: ['prod', 'dev'],
  accountTiers: ['app'],
  accountsPerEnv: 4,
  growthYears: 2,
}

function renderStep(strategy: string) {
  return render(
    <DimensionsStep dimensions={dimensions} setDimensions={vi.fn()} strategy={strategy} />,
  )
}

function capacityValues() {
  // The tip renders "<allocations>" then "<allocations * growthYears>".
  return Array.from(document.querySelectorAll('strong')).map(el => el.textContent)
}

describe('DimensionsStep capacity estimate', () => {
  it('counts regions for region-first, which allocates per region', () => {
    renderStep('region-first')

    // 3 regions x 2 environments x 4 accounts = 24, x2 growth = 48
    expect(capacityValues()).toEqual(['24', '48'])
  })

  it('counts regions for environment-first, which also allocates per region', () => {
    renderStep('environment-first')

    expect(capacityValues()).toEqual(['24', '48'])
  })

  it('ignores regions for account-first, which does not allocate per region', () => {
    renderStep('account-first')

    // 2 environments x 4 accounts = 8, x2 growth = 16
    expect(capacityValues()).toEqual(['8', '16'])
  })
})

describe('DimensionsStep regions list visibility', () => {
  it('shows the regions list for region-first', () => {
    renderStep('region-first')

    expect(screen.getByText('Regions')).toBeTruthy()
    expect(screen.getByText('us-east-1')).toBeTruthy()
  })

  it('shows the regions list for environment-first, since the estimate depends on it', () => {
    renderStep('environment-first')

    expect(screen.getByText('Regions')).toBeTruthy()
  })

  it('hides the regions list for account-first', () => {
    renderStep('account-first')

    expect(screen.queryByText('Regions')).toBeNull()
    expect(screen.queryByText('us-east-1')).toBeNull()
  })
})
