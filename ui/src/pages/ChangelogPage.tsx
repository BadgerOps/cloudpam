import { useEffect, useMemo, useState } from 'react'
import { FileText, Loader2 } from 'lucide-react'
import { getChangelogMarkdown } from '../api/client'

interface ReleaseEntry {
  version: string
  date: string
  unreleased: boolean
  categories: Record<string, string[]>
}

function parseChangelog(markdown: string): ReleaseEntry[] {
  const releases: ReleaseEntry[] = []
  let current: ReleaseEntry | null = null
  let currentCategory = ''

  for (const rawLine of markdown.split('\n')) {
    const line = rawLine.trimEnd()
    const versionMatch = line.match(/^##\s+\[?([^\]\s]+)\]?\s*-?\s*(.*)$/)
    if (versionMatch) {
      const version = versionMatch[1]
      current = {
        version,
        date: versionMatch[2].trim(),
        unreleased: version === 'Unreleased',
        categories: {},
      }
      releases.push(current)
      currentCategory = ''
      continue
    }

    const categoryMatch = line.match(/^###\s+(.+)$/)
    if (categoryMatch && current) {
      currentCategory = categoryMatch[1].trim()
      current.categories[currentCategory] = []
      continue
    }

    if (line.match(/^\s*-\s+/) && current && currentCategory) {
      current.categories[currentCategory].push(line.replace(/^\s*-\s+/, ''))
    }
  }

  return releases
}

export default function ChangelogPage() {
  const [markdown, setMarkdown] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    async function load() {
      try {
        setMarkdown(await getChangelogMarkdown())
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load changelog')
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  const releases = useMemo(() => {
    const entries = parseChangelog(markdown)
    const unreleased = entries.filter(entry => entry.unreleased)
    const released = entries.filter(entry => !entry.unreleased)
    return [...unreleased, ...released]
  }, [markdown])

  return (
    <div className="p-6 space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white flex items-center gap-2">
          <FileText className="w-6 h-6" />
          Release Notes
        </h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
          In-app changelog for CloudPAM releases.
        </p>
      </div>

      {loading && (
        <div className="flex items-center gap-2 text-gray-500 dark:text-gray-400">
          <Loader2 className="w-5 h-5 animate-spin" />
          Loading changelog…
        </div>
      )}

      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
          {error}
        </div>
      )}

      <div className="space-y-4">
        {releases.map(release => (
          <section key={release.version} className="bg-white dark:bg-gray-800 rounded-xl shadow border dark:border-gray-700 p-6">
            <div className="flex items-center justify-between gap-4 mb-4">
              <div>
                <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
                  {release.unreleased ? 'Upcoming release' : `v${release.version}`}
                </h2>
                <p className="text-sm text-gray-500 dark:text-gray-400">
                  {release.unreleased ? 'Unreleased changes queued for the next version' : release.date || 'Date not specified'}
                </p>
              </div>
              {release.unreleased && (
                <span className="inline-flex items-center rounded-full bg-blue-100 px-3 py-1 text-xs font-semibold uppercase tracking-wide text-blue-700 dark:bg-blue-900/40 dark:text-blue-200">
                  Unreleased
                </span>
              )}
            </div>
            <div className="space-y-4">
              {Object.entries(release.categories).map(([category, items]) => (
                <div key={category}>
                  <h3 className="text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2">{category}</h3>
                  <ul className="space-y-2">
                    {items.map((item, idx) => (
                      <li key={idx} className="text-sm text-gray-700 dark:text-gray-300 leading-6">
                        {item}
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </section>
        ))}
      </div>
    </div>
  )
}
