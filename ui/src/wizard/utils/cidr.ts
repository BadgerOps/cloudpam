export interface ParsedCIDR {
  ip: string
  octets: number[]
  prefixLen: number
  ipNum: number
}

export function parse(cidr: string): ParsedCIDR {
  const [ip, prefix] = cidr.split('/')
  const octets = ip.split('.').map(Number)
  const prefixLen = parseInt(prefix)
  const ipNum = (octets[0] << 24) + (octets[1] << 16) + (octets[2] << 8) + octets[3]
  return { ip, octets, prefixLen, ipNum }
}

export function toIp(num: number): string {
  return [
    (num >>> 24) & 255,
    (num >>> 16) & 255,
    (num >>> 8) & 255,
    num & 255,
  ].join('.')
}

export function hostCount(prefix: number): number {
  return Math.pow(2, 32 - prefix)
}

export function usableHosts(prefix: number): number {
  const total = Math.pow(2, 32 - prefix)
  if (prefix >= 31) return total
  return total - 5 // AWS reserves 5 per subnet
}

export function subdivide(cidr: string, newPrefix: number): string[] {
  const parsed = parse(cidr)
  const count = Math.pow(2, newPrefix - parsed.prefixLen)
  const blockSize = Math.pow(2, 32 - newPrefix)
  const results: string[] = []
  for (let i = 0; i < count; i++) {
    results.push(`${toIp(parsed.ipNum + i * blockSize)}/${newPrefix}`)
  }
  return results
}

export function formatHostCount(count: number): string {
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K`
  return count.toString()
}

export interface SchemaNode {
  id: string
  name: string
  type: 'root' | 'region' | 'environment' | 'account' | 'subnet'
  cidr: string
  children: SchemaNode[]
  conflict?: boolean
}

export function countLeafNodes(node: SchemaNode): number {
  if (!node.children || node.children.length === 0) return 1
  return node.children.reduce((sum, child) => sum + countLeafNodes(child), 0)
}
