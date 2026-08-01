import { describe, expect, it } from 'vitest'
import {
  hclHeredocShellQuote,
  hclIdent,
  hclQuote,
  hclTemplateEscape,
  safeFilename,
  shellQuote,
  yamlQuote,
} from '../utils/escape'

// A value crafted to break out of each output format if interpolated raw.
const SHELL_BREAKOUT = `us-east-1"; rm -rf /; echo "`
const HCL_BREAKOUT = 'prod-${var.secret}-%{if true}x%{endif}'

describe('shellQuote', () => {
  it('wraps plain values in single quotes', () => {
    expect(shellQuote('us-east-1')).toBe(`'us-east-1'`)
  })

  it('neutralises command substitution and quote breakouts', () => {
    const quoted = shellQuote(SHELL_BREAKOUT)
    expect(quoted.startsWith(`'`)).toBe(true)
    expect(quoted.endsWith(`'`)).toBe(true)
    // No bare double quote can terminate anything: the whole value is literal.
    expect(quoted).toBe(`'us-east-1"; rm -rf /; echo "'`)
  })

  it('closes, escapes and reopens embedded single quotes', () => {
    expect(shellQuote("it's")).toBe(`'it'\\''s'`)
  })

  it('escapes backticks and dollar signs by containment', () => {
    expect(shellQuote('$(id)`whoami`')).toBe(`'$(id)\`whoami\`'`)
  })

  it('renders null and undefined as an empty literal', () => {
    expect(shellQuote(null)).toBe(`''`)
    expect(shellQuote(undefined)).toBe(`''`)
  })

  it('strips NUL, which cannot be represented in a shell word', () => {
    expect(shellQuote('a\0b')).toBe(`'ab'`)
  })
})

describe('yamlQuote', () => {
  it('produces a double-quoted scalar', () => {
    expect(yamlQuote('us-east-1')).toBe('"us-east-1"')
  })

  it('escapes embedded quotes so the scalar cannot be closed early', () => {
    expect(yamlQuote('a"b')).toBe('"a\\"b"')
  })

  it('escapes newlines rather than emitting a second document line', () => {
    expect(yamlQuote('a\nrole_name: evil')).toBe('"a\\nrole_name: evil"')
  })

  it('escapes backslashes', () => {
    expect(yamlQuote('a\\b')).toBe('"a\\\\b"')
  })

  it('renders empty for null and undefined', () => {
    expect(yamlQuote(null)).toBe('""')
    expect(yamlQuote(undefined)).toBe('""')
  })
})

describe('hclTemplateEscape', () => {
  it('neutralises interpolation and directive markers', () => {
    expect(hclTemplateEscape('${a}')).toBe('$${a}')
    expect(hclTemplateEscape('%{if x}')).toBe('%%{if x}')
  })

  it('leaves lone dollar and percent characters alone', () => {
    expect(hclTemplateEscape('100% $USD')).toBe('100% $USD')
  })
})

describe('hclQuote', () => {
  it('produces a quoted literal with interpolation disabled', () => {
    expect(hclQuote(HCL_BREAKOUT)).toBe('"prod-$${var.secret}-%%{if true}x%%{endif}"')
  })

  it('escapes embedded quotes', () => {
    expect(hclQuote('a"b')).toBe('"a\\"b"')
  })
})

describe('hclHeredocShellQuote', () => {
  it('shell-quotes first, then neutralises HCL interpolation', () => {
    expect(hclHeredocShellQuote(HCL_BREAKOUT)).toBe(
      `'prod-$\${var.secret}-%%{if true}x%%{endif}'`
    )
  })

  it('keeps shell breakout attempts inside the literal', () => {
    expect(hclHeredocShellQuote(SHELL_BREAKOUT)).toBe(`'us-east-1"; rm -rf /; echo "'`)
  })
})

describe('hclIdent', () => {
  it('keeps a valid identifier unchanged', () => {
    expect(hclIdent('CloudPAMDiscoveryRole')).toBe('CloudPAMDiscoveryRole')
  })

  it('replaces characters that would terminate the resource label', () => {
    const ident = hclIdent('role" { evil = "1')
    expect(ident).toBe('role____evil____1')
    expect(/^[A-Za-z_][A-Za-z0-9_-]*$/.test(ident)).toBe(true)
  })

  it('falls back when the result cannot start an identifier', () => {
    expect(hclIdent('123')).toBe('cloudpam')
    expect(hclIdent('-abc')).toBe('cloudpam')
    expect(hclIdent('')).toBe('cloudpam')
    expect(hclIdent('-', 'fallback_role')).toBe('fallback_role')
  })
})

describe('safeFilename', () => {
  it('keeps a simple name', () => {
    expect(safeFilename('agent-prod')).toBe('agent-prod')
  })

  it('strips path separators so a download cannot escape its directory', () => {
    expect(safeFilename('../../etc/passwd')).toBe('etc-passwd')
    expect(safeFilename('a/b\\c')).toBe('a-b-c')
  })

  it('strips leading dots and dashes', () => {
    expect(safeFilename('...hidden')).toBe('hidden')
  })

  it('falls back when nothing usable remains', () => {
    expect(safeFilename('///')).toBe('cloudpam-agent')
    expect(safeFilename(null)).toBe('cloudpam-agent')
  })
})
