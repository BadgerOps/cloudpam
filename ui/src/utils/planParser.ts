import type { GeneratedPlan } from '../api/types'

const jsonBlockRegex = /```json\s*\n([\s\S]*?)\n```/g

export function extractPlan(content: string): GeneratedPlan | null {
  const matches = content.matchAll(jsonBlockRegex)
  for (const match of matches) {
    if (!match[1]) continue
    try {
      const parsed = JSON.parse(match[1])
      if (parsed.pools && Array.isArray(parsed.pools) && parsed.pools.length > 0) {
        return parsed as GeneratedPlan
      }
    } catch {
      continue
    }
  }
  return null
}
