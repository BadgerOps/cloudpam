import { describe, it, expect } from 'vitest'
import { parse, toIp, hostCount, usableHosts, subdivide, formatHostCount, countLeafNodes } from '../wizard/utils/cidr'
import type { SchemaNode } from '../wizard/utils/cidr'

describe('parse', () => {
  it('parses a /8 CIDR', () => {
    const result = parse('10.0.0.0/8')
    expect(result.ip).toBe('10.0.0.0')
    expect(result.octets).toEqual([10, 0, 0, 0])
    expect(result.prefixLen).toBe(8)
    expect(result.ipNum).toBe(0x0a000000)
  })

  it('parses a /24 CIDR', () => {
    const result = parse('192.168.1.0/24')
    expect(result.prefixLen).toBe(24)
    expect(result.octets).toEqual([192, 168, 1, 0])
  })
})

describe('toIp', () => {
  it('converts number to IP string', () => {
    expect(toIp(0x0a000000)).toBe('10.0.0.0')
    expect(toIp(0xc0a80100)).toBe('192.168.1.0')
    expect(toIp(0xffffffff)).toBe('255.255.255.255')
  })
})

describe('hostCount', () => {
  it('returns correct host counts', () => {
    expect(hostCount(8)).toBe(16777216)  // 2^24
    expect(hostCount(16)).toBe(65536)     // 2^16
    expect(hostCount(24)).toBe(256)       // 2^8
    expect(hostCount(32)).toBe(1)         // 2^0
  })
})

describe('usableHosts', () => {
  it('subtracts AWS reserved IPs for normal prefixes', () => {
    expect(usableHosts(24)).toBe(251)  // 256 - 5
    expect(usableHosts(16)).toBe(65531) // 65536 - 5
  })

  it('returns total for /31 and /32', () => {
    expect(usableHosts(31)).toBe(2)
    expect(usableHosts(32)).toBe(1)
  })
})

describe('subdivide', () => {
  it('subdivides /8 into /12 blocks', () => {
    const blocks = subdivide('10.0.0.0/8', 12)
    expect(blocks.length).toBe(16) // 2^(12-8) = 16
    expect(blocks[0]).toBe('10.0.0.0/12')
    expect(blocks[1]).toBe('10.16.0.0/12')
    expect(blocks[15]).toBe('10.240.0.0/12')
  })

  it('subdivides /16 into /24 blocks', () => {
    const blocks = subdivide('10.0.0.0/16', 24)
    expect(blocks.length).toBe(256) // 2^(24-16) = 256
    expect(blocks[0]).toBe('10.0.0.0/24')
    expect(blocks[1]).toBe('10.0.1.0/24')
  })

  it('same prefix returns single block', () => {
    const blocks = subdivide('10.0.0.0/24', 24)
    expect(blocks.length).toBe(1)
    expect(blocks[0]).toBe('10.0.0.0/24')
  })
})

describe('formatHostCount', () => {
  it('formats millions', () => {
    expect(formatHostCount(16777216)).toBe('16.8M')
  })

  it('formats thousands', () => {
    expect(formatHostCount(65536)).toBe('65.5K')
  })

  it('formats small numbers as-is', () => {
    expect(formatHostCount(256)).toBe('256')
    expect(formatHostCount(1)).toBe('1')
  })
})

describe('countLeafNodes', () => {
  it('returns 1 for a leaf node', () => {
    const leaf: SchemaNode = { id: 'l', name: 'leaf', type: 'subnet', cidr: '10.0.0.0/24', children: [] }
    expect(countLeafNodes(leaf)).toBe(1)
  })

  it('counts leaves in a tree', () => {
    const tree: SchemaNode = {
      id: 'root',
      name: 'Root',
      type: 'supernet',
      cidr: '10.0.0.0/8',
      children: [
        {
          id: 'r1',
          name: 'Region 1',
          type: 'region',
          cidr: '10.0.0.0/12',
          children: [
            { id: 'e1', name: 'prod', type: 'environment', cidr: '10.0.0.0/16', children: [] },
            { id: 'e2', name: 'dev', type: 'environment', cidr: '10.1.0.0/16', children: [] },
          ],
        },
        {
          id: 'r2',
          name: 'Region 2',
          type: 'region',
          cidr: '10.16.0.0/12',
          children: [],
        },
      ],
    }
    expect(countLeafNodes(tree)).toBe(3) // e1, e2, r2
  })
})
