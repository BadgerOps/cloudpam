import { describe, expect, it } from 'vitest'
import { resolveConfigTab, type ConfigTab } from '../components/DiscoveryWizard'

const ORG_TABS: ConfigTab[] = ['shell', 'yaml', 'terraform', 'docker', 'iam']
const SINGLE_TABS: ConfigTab[] = ['shell', 'yaml', 'terraform', 'docker']

describe('resolveConfigTab', () => {
  it('keeps a selection that is still offered', () => {
    expect(resolveConfigTab('terraform', SINGLE_TABS)).toBe('terraform')
    expect(resolveConfigTab('iam', ORG_TABS)).toBe('iam')
  })

  // Regression: selecting the org-only "IAM Setup" tab and then switching back
  // to single-account mode left configTab on 'iam', so the wizard kept
  // rendering the org IAM snippet under a tab strip that no longer listed it.
  it('falls back when the selected tab is no longer available', () => {
    expect(resolveConfigTab('iam', SINGLE_TABS)).toBe('shell')
  })

  it('falls back to the first available tab, whatever that is', () => {
    expect(resolveConfigTab('iam', ['docker', 'yaml'])).toBe('docker')
  })

  it('is stable across a mode round trip', () => {
    // org -> pick iam -> back to single -> back to org
    let selected: ConfigTab = 'iam'
    expect(resolveConfigTab(selected, ORG_TABS)).toBe('iam')
    expect(resolveConfigTab(selected, SINGLE_TABS)).toBe('shell')
    // The stored selection is untouched, so returning to org mode restores it.
    expect(resolveConfigTab(selected, ORG_TABS)).toBe('iam')

    selected = 'yaml'
    expect(resolveConfigTab(selected, SINGLE_TABS)).toBe('yaml')
    expect(resolveConfigTab(selected, ORG_TABS)).toBe('yaml')
  })
})
