import { describe, it, expect } from 'vitest'
import {
  formatHostCount,
  getHostCount,
  getPoolTypeColor,
  getUtilizationColor,
  getProviderBadgeClass,
  getTierBadgeClass,
  getStatusBadgeClass,
  getActionBadgeClass,
} from '../utils/format'

describe('formatHostCount', () => {
  it('returns raw number for small counts', () => {
    expect(formatHostCount(256)).toBe('256')
  })

  it('formats thousands with K suffix', () => {
    expect(formatHostCount(65536)).toBe('65.5K')
  })

  it('formats millions with M suffix', () => {
    expect(formatHostCount(16777216)).toBe('16.8M')
  })

  it('handles zero', () => {
    expect(formatHostCount(0)).toBe('0')
  })
})

describe('getHostCount', () => {
  it('computes /8 correctly', () => {
    expect(getHostCount('10.0.0.0/8')).toBe(16777216)
  })

  it('computes /24 correctly', () => {
    expect(getHostCount('192.168.1.0/24')).toBe(256)
  })

  it('computes /32 correctly', () => {
    expect(getHostCount('10.0.0.1/32')).toBe(1)
  })

  it('returns 0 for empty input', () => {
    expect(getHostCount('')).toBe(0)
  })

  it('returns 0 for invalid CIDR', () => {
    expect(getHostCount('invalid')).toBe(0)
  })
})

describe('color helpers', () => {
  it('getPoolTypeColor returns correct class for known types', () => {
    expect(getPoolTypeColor('supernet')).toBe('bg-purple-500')
    expect(getPoolTypeColor('vpc')).toBe('bg-amber-500')
    expect(getPoolTypeColor('unknown')).toBe('bg-gray-400')
  })

  it('getUtilizationColor returns red for high utilization', () => {
    expect(getUtilizationColor(90)).toBe('bg-red-500')
    expect(getUtilizationColor(70)).toBe('bg-amber-500')
    expect(getUtilizationColor(30)).toBe('bg-green-500')
  })

  it('getProviderBadgeClass returns classes for known providers', () => {
    expect(getProviderBadgeClass('aws')).toContain('amber')
    expect(getProviderBadgeClass('gcp')).toContain('blue')
    expect(getProviderBadgeClass('other')).toContain('gray')
  })

  it('getTierBadgeClass returns classes for known tiers', () => {
    expect(getTierBadgeClass('prd')).toContain('red')
    expect(getTierBadgeClass('dev')).toContain('green')
  })

  it('getStatusBadgeClass returns classes for known statuses', () => {
    expect(getStatusBadgeClass('active')).toContain('green')
    expect(getStatusBadgeClass('deprecated')).toContain('red')
  })

  it('getActionBadgeClass returns classes based on action', () => {
    expect(getActionBadgeClass('create')).toContain('green')
    expect(getActionBadgeClass('update')).toContain('blue')
    expect(getActionBadgeClass('delete')).toContain('red')
  })
})
