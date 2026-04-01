import { describe, expect, it } from 'vitest'
import {
  filterReleases,
  formatReleaseDate,
  parseChangelog,
  sortReleases,
  summarizeReleases,
  versionType,
} from '../utils/changelog'

const sampleChangelog = `# Changelog

## [0.9.0] - 2026-03-31

### Added
- Searchable release notes page

### Fixed
- Restored missing JS bundles
  across SPA asset requests

## [0.8.1] - 2026-03-30

### Changed
- Updated release pipeline

## [Unreleased]

### Added
- Grapheon-style timeline cards
`

describe('parseChangelog', () => {
  it('parses releases, categories, and continued bullet lines', () => {
    const releases = parseChangelog(sampleChangelog)

    expect(releases).toHaveLength(3)
    expect(releases[0]).toMatchObject({
      version: '0.9.0',
      unreleased: false,
    })
    expect(releases[0].categories.Fixed).toEqual([
      'Restored missing JS bundles across SPA asset requests',
    ])
    expect(releases[2]).toMatchObject({
      version: 'Unreleased',
      unreleased: true,
    })
  })
})

describe('sortReleases', () => {
  it('puts unreleased first, then semver releases in descending order', () => {
    const releases = sortReleases(parseChangelog(sampleChangelog))

    expect(releases.map((release) => release.version)).toEqual([
      'Unreleased',
      '0.9.0',
      '0.8.1',
    ])
  })
})

describe('filterReleases', () => {
  it('filters changelog entries by keyword while keeping matching categories', () => {
    const releases = sortReleases(parseChangelog(sampleChangelog))
    const filtered = filterReleases(releases, 'timeline')

    expect(filtered).toHaveLength(1)
    expect(filtered[0].version).toBe('Unreleased')
    expect(filtered[0].categories.Added).toEqual(['Grapheon-style timeline cards'])
  })
})

describe('summarizeReleases', () => {
  it('counts release totals and the main category totals', () => {
    const summary = summarizeReleases(parseChangelog(sampleChangelog))

    expect(summary).toEqual({
      releases: 3,
      added: 2,
      changed: 1,
      fixed: 1,
    })
  })
})

describe('helpers', () => {
  it('formats ISO changelog dates for display', () => {
    expect(formatReleaseDate('2026-03-31')).toBe('March 31, 2026')
    expect(formatReleaseDate('Sprint 20')).toBe('Sprint 20')
  })

  it('classifies release version types', () => {
    expect(versionType('Unreleased')).toBe('upcoming')
    expect(versionType('1.0.0')).toBe('major')
    expect(versionType('0.9.0')).toBe('minor')
    expect(versionType('0.9.1')).toBe('patch')
  })
})
