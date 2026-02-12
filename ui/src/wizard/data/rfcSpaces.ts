export interface RFCSpace {
  cidr: string
  name: string
  hosts: string
  description: string
}

export const RFC_SPACES: RFCSpace[] = [
  { cidr: '10.0.0.0/8', name: 'RFC1918 Class A', hosts: '16M', description: 'Enterprise scale' },
  { cidr: '172.16.0.0/12', name: 'RFC1918 Class B', hosts: '1M', description: 'Medium organization' },
  { cidr: '192.168.0.0/16', name: 'RFC1918 Class C', hosts: '65K', description: 'Small/Lab' },
  { cidr: '100.64.0.0/10', name: 'RFC6598 CGNAT', hosts: '4M', description: 'Carrier-grade NAT' },
]
