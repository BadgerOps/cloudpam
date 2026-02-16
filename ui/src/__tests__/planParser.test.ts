import { describe, it, expect } from 'vitest'
import { extractPlan } from '../utils/planParser'

describe('extractPlan', () => {
  it('extracts a valid plan from markdown', () => {
    const content = `Here's a network plan:

\`\`\`json
{
  "name": "Production VPC",
  "description": "Production network layout",
  "pools": [
    {"ref": "root", "name": "prod-vpc", "cidr": "10.0.0.0/16", "type": "supernet"},
    {"ref": "sub1", "name": "web-subnet", "cidr": "10.0.1.0/24", "type": "subnet", "parent_ref": "root"}
  ]
}
\`\`\`

You can apply this plan using the Apply button.`

    const plan = extractPlan(content)
    expect(plan).not.toBeNull()
    expect(plan!.name).toBe('Production VPC')
    expect(plan!.pools).toHaveLength(2)
    expect(plan!.pools[0].ref).toBe('root')
    expect(plan!.pools[1].parent_ref).toBe('root')
  })

  it('returns null when no JSON block is found', () => {
    const content = 'This is just a regular message with no code blocks.'
    expect(extractPlan(content)).toBeNull()
  })

  it('returns null for JSON without pools', () => {
    const content = `\`\`\`json
{"name": "test", "description": "no pools"}
\`\`\``
    expect(extractPlan(content)).toBeNull()
  })

  it('returns null for empty pools array', () => {
    const content = `\`\`\`json
{"name": "test", "description": "empty pools", "pools": []}
\`\`\``
    expect(extractPlan(content)).toBeNull()
  })

  it('skips invalid JSON blocks and finds valid one', () => {
    const content = `\`\`\`json
{invalid json}
\`\`\`

\`\`\`json
{
  "name": "Valid Plan",
  "description": "This one works",
  "pools": [
    {"ref": "root", "name": "vpc", "cidr": "10.0.0.0/16", "type": "supernet"}
  ]
}
\`\`\``

    const plan = extractPlan(content)
    expect(plan).not.toBeNull()
    expect(plan!.name).toBe('Valid Plan')
  })
})
