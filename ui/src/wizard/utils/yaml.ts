import { yamlQuote } from '../../utils/escape'

/**
 * Renders a JSON-compatible value as block-style YAML, mirroring
 * JSON.stringify: undefined properties are dropped and non-finite numbers
 * become null, so a YAML and a JSON rendering of the same value parse back to
 * the same document. Every string is double-quoted, so user-supplied text
 * never reads as YAML syntax or as another type.
 */

const PLAIN_KEY = /^[A-Za-z_][A-Za-z0-9_-]*$/
// YAML 1.1 parsers read these plain keys as booleans or null.
const RESERVED_KEYS = new Set(['true', 'false', 'null', 'yes', 'no', 'on', 'off', 'y', 'n'])

function key(name: string): string {
  return PLAIN_KEY.test(name) && !RESERVED_KEYS.has(name.toLowerCase()) ? name : yamlQuote(name)
}

function scalar(value: unknown): string {
  switch (typeof value) {
    case 'string':
      return yamlQuote(value)
    case 'number':
      return Number.isFinite(value) ? String(value) : 'null'
    case 'boolean':
      return String(value)
    default:
      return 'null'
  }
}

function entriesOf(value: object): [string, unknown][] {
  return Object.entries(value).filter(([, v]) => v !== undefined)
}

function isNonEmptyContainer(value: unknown): boolean {
  if (Array.isArray(value)) return value.length > 0
  return typeof value === 'object' && value !== null && entriesOf(value).length > 0
}

const indent = (line: string) => `  ${line}`

function lines(value: unknown): string[] {
  if (Array.isArray(value)) {
    if (value.length === 0) return ['[]']
    return value.flatMap((item) => lines(item).map((line, i) => (i === 0 ? `- ${line}` : indent(line))))
  }
  if (typeof value === 'object' && value !== null) {
    const entries = entriesOf(value)
    if (entries.length === 0) return ['{}']
    return entries.flatMap(([k, v]) =>
      isNonEmptyContainer(v) ? [`${key(k)}:`, ...lines(v).map(indent)] : [`${key(k)}: ${lines(v)[0]}`],
    )
  }
  return [scalar(value)]
}

export function toYaml(value: unknown): string {
  return lines(value).join('\n') + '\n'
}
