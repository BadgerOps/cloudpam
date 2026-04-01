export interface ReleaseEntry {
  version: string
  date: string | null
  unreleased: boolean
  categories: Record<string, string[]>
}

function parseSemver(version: string): [number, number, number] | null {
  const match = version.trim().match(/^(\d+)\.(\d+)\.(\d+)$/)
  if (!match) return null
  return [Number(match[1]), Number(match[2]), Number(match[3])]
}

export function parseChangelog(markdown: string): ReleaseEntry[] {
  const releases: ReleaseEntry[] = []
  let current: ReleaseEntry | null = null
  let currentCategory: string | null = null

  for (const rawLine of markdown.split('\n')) {
    const line = rawLine.trimEnd()
    const versionMatch = line.match(/^##\s+\[?([^\]\s]+)\]?\s*-?\s*(.*)$/)
    if (versionMatch) {
      const version = versionMatch[1]
      current = {
        version,
        date: versionMatch[2].trim() || null,
        unreleased: version.toLowerCase() === 'unreleased',
        categories: {},
      }
      releases.push(current)
      currentCategory = null
      continue
    }

    const categoryMatch = line.match(/^###\s+(.+)$/)
    if (categoryMatch && current) {
      currentCategory = categoryMatch[1].trim()
      if (!current.categories[currentCategory]) {
        current.categories[currentCategory] = []
      }
      continue
    }

    if (line.match(/^\s*-\s+/) && current && currentCategory) {
      current.categories[currentCategory].push(line.replace(/^\s*-\s+/, ''))
      continue
    }

    if (line.match(/^\s{2,}/) && current && currentCategory) {
      const items = current.categories[currentCategory]
      if (items.length > 0) {
        items[items.length - 1] += ` ${line.trim()}`
      }
    }
  }

  return releases
}

export function sortReleases(releases: ReleaseEntry[]): ReleaseEntry[] {
  return releases
    .map((release, index) => ({ release, index }))
    .sort((left, right) => {
      if (left.release.unreleased !== right.release.unreleased) {
        return left.release.unreleased ? -1 : 1
      }

      const leftVersion = parseSemver(left.release.version)
      const rightVersion = parseSemver(right.release.version)
      if (leftVersion && rightVersion) {
        if (leftVersion[0] !== rightVersion[0]) return rightVersion[0] - leftVersion[0]
        if (leftVersion[1] !== rightVersion[1]) return rightVersion[1] - leftVersion[1]
        if (leftVersion[2] !== rightVersion[2]) return rightVersion[2] - leftVersion[2]
      } else if (leftVersion || rightVersion) {
        return leftVersion ? -1 : 1
      }

      return left.index - right.index
    })
    .map(({ release }) => release)
}

export function filterReleases(releases: ReleaseEntry[], searchTerm: string): ReleaseEntry[] {
  const term = searchTerm.trim().toLowerCase()
  if (!term) return releases

  return releases
    .map((release) => {
      const matchesRelease =
        release.version.toLowerCase().includes(term) ||
        release.date?.toLowerCase().includes(term) ||
        (release.unreleased && 'upcoming'.includes(term))

      const filteredCategories = Object.fromEntries(
        Object.entries(release.categories)
          .map(([category, items]) => {
            if (category.toLowerCase().includes(term)) return [category, items]
            const filteredItems = items.filter((item) => item.toLowerCase().includes(term))
            return filteredItems.length > 0 ? [category, filteredItems] : null
          })
          .filter((entry): entry is [string, string[]] => entry !== null),
      )

      if (matchesRelease) return release
      if (Object.keys(filteredCategories).length === 0) return null
      return {
        ...release,
        categories: filteredCategories,
      }
    })
    .filter((release): release is ReleaseEntry => release !== null)
}

export function summarizeReleases(releases: ReleaseEntry[]) {
  return releases.reduce(
    (summary, release) => {
      summary.releases += 1
      summary.added += release.categories.Added?.length ?? 0
      summary.changed += release.categories.Changed?.length ?? 0
      summary.fixed += release.categories.Fixed?.length ?? 0
      return summary
    },
    { releases: 0, added: 0, changed: 0, fixed: 0 },
  )
}

export function formatReleaseDate(date: string | null): string {
  if (!date) return ''
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return date

  const parsed = new Date(`${date}T00:00:00`)
  if (Number.isNaN(parsed.getTime())) return date

  return parsed.toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })
}

export function versionType(version: string): 'major' | 'minor' | 'patch' | 'upcoming' | 'other' {
  if (version.toLowerCase() === 'unreleased') return 'upcoming'

  const parsed = parseSemver(version)
  if (!parsed) return 'other'

  if (parsed[0] > 0 && parsed[1] === 0 && parsed[2] === 0) return 'major'
  if (parsed[2] === 0) return 'minor'
  return 'patch'
}
