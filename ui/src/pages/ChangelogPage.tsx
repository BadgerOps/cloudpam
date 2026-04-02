import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  ArrowLeft,
  FileText,
  Info,
  Loader2,
  Plus,
  RefreshCw,
  Search,
  Shield,
  Sparkles,
  Wrench,
} from 'lucide-react'
import type { ReactNode } from 'react'
import { getChangelogMarkdown, getSystemInfo } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import type { SystemInfoResponse } from '../api/types'
import {
  filterReleases,
  formatReleaseDate,
  parseChangelog,
  sortReleases,
  summarizeReleases,
  versionType,
  type ReleaseEntry,
} from '../utils/changelog'

const VERSION_TYPE_STYLES = {
  major: 'bg-red-100 text-red-700 ring-1 ring-red-200 dark:bg-red-900/40 dark:text-red-200 dark:ring-red-800',
  minor: 'bg-blue-100 text-blue-700 ring-1 ring-blue-200 dark:bg-blue-900/40 dark:text-blue-200 dark:ring-blue-800',
  patch: 'bg-slate-100 text-slate-700 ring-1 ring-slate-200 dark:bg-slate-800 dark:text-slate-200 dark:ring-slate-700',
  upcoming: 'bg-violet-100 text-violet-700 ring-1 ring-violet-200 dark:bg-violet-900/40 dark:text-violet-200 dark:ring-violet-800',
  other: 'bg-zinc-100 text-zinc-700 ring-1 ring-zinc-200 dark:bg-zinc-800 dark:text-zinc-200 dark:ring-zinc-700',
} as const

interface CategoryMeta {
  icon: ReactNode
  badgeClass: string
  dotClass: string
  iconClass: string
}

function categoryMeta(category: string): CategoryMeta {
  switch (category) {
    case 'Added':
      return {
        icon: <Plus className="h-4 w-4" />,
        badgeClass: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-200',
        dotClass: 'bg-emerald-500',
        iconClass: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-200',
      }
    case 'Changed':
      return {
        icon: <RefreshCw className="h-4 w-4" />,
        badgeClass: 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-200',
        dotClass: 'bg-amber-500',
        iconClass: 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-200',
      }
    case 'Fixed':
      return {
        icon: <Wrench className="h-4 w-4" />,
        badgeClass: 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-200',
        dotClass: 'bg-blue-500',
        iconClass: 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-200',
      }
    case 'Security':
      return {
        icon: <Shield className="h-4 w-4" />,
        badgeClass: 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-200',
        dotClass: 'bg-rose-500',
        iconClass: 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-200',
      }
    default:
      return {
        icon: <Info className="h-4 w-4" />,
        badgeClass: 'bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-200',
        dotClass: 'bg-zinc-500',
        iconClass: 'bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-200',
      }
  }
}

function renderInlineMarkdown(text: string) {
  const parts: ReactNode[] = []
  let remaining = text
  let key = 0

  while (remaining.length > 0) {
    const codeMatch = /`([^`]+)`/.exec(remaining)
    const boldMatch = /\*\*([^*]+)\*\*/.exec(remaining)

    let earliest: RegExpExecArray | null = null
    let type: 'code' | 'bold' | null = null
    let earliestIndex = Number.POSITIVE_INFINITY

    if (codeMatch && codeMatch.index < earliestIndex) {
      earliest = codeMatch
      earliestIndex = codeMatch.index
      type = 'code'
    }
    if (boldMatch && boldMatch.index < earliestIndex) {
      earliest = boldMatch
      earliestIndex = boldMatch.index
      type = 'bold'
    }

    if (!earliest || type == null) {
      parts.push(<span key={key++}>{remaining}</span>)
      break
    }

    const matchIndex = earliest.index ?? 0
    if (matchIndex > 0) {
      parts.push(<span key={key++}>{remaining.slice(0, matchIndex)}</span>)
    }

    if (type === 'code') {
      parts.push(
        <code key={key++} className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-[13px] text-blue-700 dark:bg-slate-800 dark:text-blue-200">
          {earliest[1]}
        </code>,
      )
    } else {
      parts.push(
        <strong key={key++} className="font-semibold text-slate-900 dark:text-white">
          {earliest[1]}
        </strong>,
      )
    }

    remaining = remaining.slice(matchIndex + earliest[0].length)
  }

  return parts
}

function SummaryCard({
  icon,
  label,
  value,
}: {
  icon: ReactNode
  label: string
  value: string
}) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white/85 p-4 shadow-sm shadow-slate-200/60 backdrop-blur dark:border-slate-800 dark:bg-slate-900/80 dark:shadow-black/20">
      <div className="mb-3 flex items-center gap-3">
        <div className="rounded-xl bg-slate-100 p-2 text-slate-700 dark:bg-slate-800 dark:text-slate-100">
          {icon}
        </div>
        <p className="text-sm font-medium text-slate-600 dark:text-slate-300">{label}</p>
      </div>
      <p className="text-2xl font-semibold text-slate-950 dark:text-white">{value}</p>
    </div>
  )
}

function ReleaseCard({
  release,
  isLatest,
  forceExpanded,
  showConnector,
}: {
  release: ReleaseEntry
  isLatest: boolean
  forceExpanded: boolean
  showConnector: boolean
}) {
  const [expanded, setExpanded] = useState(forceExpanded)

  useEffect(() => {
    setExpanded(forceExpanded)
  }, [forceExpanded])

  const totalChanges = Object.values(release.categories).reduce((sum, items) => sum + items.length, 0)
  const type = versionType(release.version)
  const releaseLabel = release.unreleased ? 'Upcoming release' : `v${release.version}`
  const detailLabel = release.unreleased
    ? 'Queued changes for the next CloudPAM release'
    : formatReleaseDate(release.date) || 'Date not specified'

  return (
    <div className="relative pl-8 pb-8 last:pb-0">
      {showConnector ? (
        <div className="absolute left-[11px] top-6 bottom-0 w-px bg-gradient-to-b from-blue-300 to-slate-200 dark:from-blue-700 dark:to-slate-800" />
      ) : null}
      <div
        className={`absolute left-0 top-1.5 flex h-6 w-6 items-center justify-center rounded-full border-2 ${
          isLatest
            ? 'border-blue-300 bg-blue-500 shadow-lg shadow-blue-500/25 dark:border-blue-700'
            : 'border-slate-300 bg-white dark:border-slate-600 dark:bg-slate-950'
        }`}
      >
        {isLatest ? <span className="h-2 w-2 rounded-full bg-white" /> : null}
      </div>

      <div
        className={`rounded-2xl border transition-all duration-200 ${
          expanded
            ? 'border-slate-200 bg-white shadow-md shadow-slate-200/60 dark:border-slate-700 dark:bg-slate-900 dark:shadow-black/20'
            : 'border-slate-200 bg-slate-50/90 hover:border-blue-300 hover:shadow-sm dark:border-slate-800 dark:bg-slate-900/60 dark:hover:border-blue-700'
        }`}
      >
        <button
          type="button"
          onClick={() => setExpanded((current) => !current)}
          className="flex w-full items-center justify-between gap-4 px-5 py-4 text-left"
        >
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <span className={`inline-flex items-center rounded-lg px-3 py-1 font-mono text-sm font-bold ${VERSION_TYPE_STYLES[type]}`}>
                {release.unreleased ? 'Unreleased' : `v${release.version}`}
              </span>
              {isLatest ? (
                <span className="inline-flex items-center rounded-full bg-blue-500 px-2 py-0.5 text-xs font-medium text-white">
                  Latest
                </span>
              ) : null}
              <span className="text-sm font-medium text-slate-900 dark:text-white">{releaseLabel}</span>
            </div>
            <p className="mt-2 text-sm text-slate-500 dark:text-slate-400">{detailLabel}</p>
            <div className="mt-3 hidden flex-wrap gap-2 sm:flex">
              {Object.entries(release.categories).map(([category, items]) => {
                const meta = categoryMeta(category)
                return (
                  <span key={category} className={`inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium ${meta.badgeClass}`}>
                    {meta.icon}
                    <span>{category}</span>
                    <span>{items.length}</span>
                  </span>
                )
              })}
            </div>
          </div>
          <div className="shrink-0 text-right">
            <p className="text-xs uppercase tracking-[0.18em] text-slate-400 dark:text-slate-500">
              {totalChanges} change{totalChanges === 1 ? '' : 's'}
            </p>
            <svg
              className={`mt-2 h-4 w-4 text-slate-400 transition-transform ${expanded ? 'rotate-180' : ''}`}
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth={2}
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
            </svg>
          </div>
        </button>

        {expanded ? (
          <div className="space-y-5 border-t border-slate-100 px-5 pb-5 pt-4 dark:border-slate-800">
            {Object.entries(release.categories).map(([category, items]) => {
              const meta = categoryMeta(category)
              return (
                <section key={category}>
                  <div className="mb-3 flex items-center gap-2">
                    <span className={`inline-flex h-7 w-7 items-center justify-center rounded-lg ${meta.iconClass}`}>
                      {meta.icon}
                    </span>
                    <h3 className="text-sm font-semibold text-slate-900 dark:text-white">{category}</h3>
                    <span className={`rounded-md px-1.5 py-0.5 text-xs font-medium ${meta.badgeClass}`}>{items.length}</span>
                  </div>
                  <ul className="space-y-2 pl-6">
                    {items.map((item, index) => (
                      <li key={index} className="relative text-sm leading-6 text-slate-700 dark:text-slate-300">
                        <span className={`absolute -left-4 top-2 h-1.5 w-1.5 rounded-full ${meta.dotClass}`} />
                        {renderInlineMarkdown(item)}
                      </li>
                    ))}
                  </ul>
                </section>
              )
            })}
          </div>
        ) : null}
      </div>
    </div>
  )
}

export default function ChangelogPage() {
  const { role } = useAuth()
  const [markdown, setMarkdown] = useState('')
  const [systemInfo, setSystemInfo] = useState<SystemInfoResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [searchTerm, setSearchTerm] = useState('')

  useEffect(() => {
    let cancelled = false

    async function load() {
      const [changelogResult, systemInfoResult] = await Promise.allSettled([
        getChangelogMarkdown(),
        getSystemInfo(),
      ])

      if (cancelled) return

      if (changelogResult.status === 'fulfilled') {
        setMarkdown(changelogResult.value)
        setError(null)
      } else {
        setError(changelogResult.reason instanceof Error ? changelogResult.reason.message : 'Failed to load changelog')
      }

      if (systemInfoResult.status === 'fulfilled') {
        setSystemInfo(systemInfoResult.value)
      }

      setLoading(false)
    }

    void load()
    return () => {
      cancelled = true
    }
  }, [])

  const parsedReleases = sortReleases(parseChangelog(markdown))
  const releases = filterReleases(parsedReleases, searchTerm)
  const stats = summarizeReleases(parsedReleases)

  return (
    <div className="min-h-full bg-[radial-gradient(circle_at_top_left,_rgba(59,130,246,0.12),_transparent_40%),linear-gradient(180deg,_rgba(248,250,252,1)_0%,_rgba(241,245,249,1)_100%)] px-4 py-8 dark:bg-[radial-gradient(circle_at_top_left,_rgba(59,130,246,0.16),_transparent_35%),linear-gradient(180deg,_rgba(2,6,23,1)_0%,_rgba(15,23,42,1)_100%)] sm:px-6">
      <div className="mx-auto max-w-5xl">
        <div className="mb-8 flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div className="min-w-0">
            <div className="mb-3 inline-flex items-center gap-3 rounded-2xl border border-blue-200 bg-white/80 px-4 py-3 shadow-sm shadow-blue-100/60 backdrop-blur dark:border-blue-900/60 dark:bg-slate-900/80 dark:shadow-black/20">
              <div className="rounded-xl bg-gradient-to-br from-blue-500 to-cyan-500 p-2 text-white shadow-lg shadow-blue-500/20">
                <FileText className="h-5 w-5" />
              </div>
              <div>
                <h1 className="text-2xl font-bold text-slate-950 dark:text-white">Release Notes</h1>
                <p className="text-sm text-slate-500 dark:text-slate-400">What changed in CloudPAM, presented in-app.</p>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {systemInfo?.version ? (
                <span className="inline-flex items-center rounded-full bg-slate-900 px-3 py-1 text-xs font-semibold uppercase tracking-[0.18em] text-white dark:bg-slate-100 dark:text-slate-900">
                  Current v{systemInfo.version}
                </span>
              ) : null}
              <span className="inline-flex items-center rounded-full bg-white/80 px-3 py-1 text-xs font-medium text-slate-600 ring-1 ring-slate-200 backdrop-blur dark:bg-slate-900/80 dark:text-slate-300 dark:ring-slate-700">
                Embedded from `docs/CHANGELOG.md`
              </span>
            </div>
          </div>

          <div className="flex flex-wrap gap-2">
            {role === 'admin' ? (
              <Link
                to="/config/updates"
                className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white/85 px-4 py-2 text-sm font-medium text-slate-700 shadow-sm shadow-slate-200/50 transition hover:border-blue-300 hover:text-blue-700 dark:border-slate-700 dark:bg-slate-900/85 dark:text-slate-200 dark:hover:border-blue-700 dark:hover:text-blue-200"
              >
                <Sparkles className="h-4 w-4" />
                Updates
              </Link>
            ) : null}
            <Link
              to="/"
              className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white/85 px-4 py-2 text-sm font-medium text-slate-700 shadow-sm shadow-slate-200/50 transition hover:border-blue-300 hover:text-blue-700 dark:border-slate-700 dark:bg-slate-900/85 dark:text-slate-200 dark:hover:border-blue-700 dark:hover:text-blue-200"
            >
              <ArrowLeft className="h-4 w-4" />
              Dashboard
            </Link>
          </div>
        </div>

        <div className="mb-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <SummaryCard icon={<FileText className="h-4 w-4" />} label="Releases" value={String(stats.releases)} />
          <SummaryCard icon={<Plus className="h-4 w-4" />} label="Added" value={String(stats.added)} />
          <SummaryCard icon={<RefreshCw className="h-4 w-4" />} label="Changed" value={String(stats.changed)} />
          <SummaryCard icon={<Wrench className="h-4 w-4" />} label="Fixed" value={String(stats.fixed)} />
          <SummaryCard icon={<Shield className="h-4 w-4" />} label="Security" value={String(stats.security)} />
        </div>

        <div className="mb-6 rounded-2xl border border-slate-200 bg-white/85 p-4 shadow-sm shadow-slate-200/60 backdrop-blur dark:border-slate-800 dark:bg-slate-900/80 dark:shadow-black/20">
          <label className="relative block">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
            <input
              type="text"
              value={searchTerm}
              onChange={(event) => setSearchTerm(event.target.value)}
              placeholder="Search versions, categories, or release notes"
              className="w-full rounded-xl border border-slate-200 bg-slate-50 py-3 pl-10 pr-4 text-sm text-slate-900 outline-none transition focus:border-blue-400 focus:bg-white dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:focus:border-blue-600"
            />
          </label>
        </div>

        {loading ? (
          <div className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white/85 px-5 py-4 text-sm text-slate-500 shadow-sm shadow-slate-200/60 backdrop-blur dark:border-slate-800 dark:bg-slate-900/80 dark:text-slate-300 dark:shadow-black/20">
            <Loader2 className="h-5 w-5 animate-spin" />
            Loading changelog…
          </div>
        ) : null}

        {error ? (
          <div className="mb-6 rounded-2xl border border-red-200 bg-red-50/90 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-200">
            {error}
          </div>
        ) : null}

        {!loading && !error ? (
          <div className="rounded-3xl border border-slate-200 bg-white/70 p-6 shadow-sm shadow-slate-200/60 backdrop-blur dark:border-slate-800 dark:bg-slate-950/55 dark:shadow-black/20">
            {releases.length > 0 ? (
              <div>
                {releases.map((release, index) => (
                  <ReleaseCard
                    key={release.version}
                    release={release}
                    isLatest={index === 0}
                    forceExpanded={index === 0 || searchTerm.trim().length > 0}
                    showConnector={index < releases.length - 1}
                  />
                ))}
              </div>
            ) : (
              <div className="rounded-2xl border border-dashed border-slate-300 px-6 py-12 text-center dark:border-slate-700">
                <Search className="mx-auto h-8 w-8 text-slate-300 dark:text-slate-600" />
                <p className="mt-4 text-sm font-medium text-slate-700 dark:text-slate-200">No release notes match “{searchTerm}”.</p>
                <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">Try a version number, category, or keyword from the notes.</p>
              </div>
            )}
          </div>
        ) : null}
      </div>
    </div>
  )
}
