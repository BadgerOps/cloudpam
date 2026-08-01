/**
 * Escaping helpers for generated deployment snippets.
 *
 * The discovery wizard renders shell, YAML, HCL and docker commands that embed
 * user-supplied and API-supplied values (account names, IAM role names, region
 * lists, external IDs). Without quoting, a value such as `foo"; rm -rf /; #`
 * turns a copy-paste command into an injection, so every interpolation must go
 * through the helper matching its output format.
 */

function asText(value: unknown): string {
  if (value === null || value === undefined) return ''
  return String(value)
}

/**
 * POSIX shell single-quoted literal. Safe for any character except NUL: inside
 * single quotes the shell performs no expansion, and an embedded quote is
 * closed, escaped and reopened.
 */
export function shellQuote(value: unknown): string {
  return `'${asText(value).replace(/\0/g, '').replace(/'/g, `'\\''`)}'`
}

/**
 * Double-quoted YAML scalar. JSON string syntax is a valid YAML 1.2
 * double-quoted scalar, so JSON.stringify gives correct escaping.
 */
export function yamlQuote(value: unknown): string {
  return JSON.stringify(asText(value).replace(/\0/g, ''))
}

/**
 * Escapes HCL template interpolation markers in text that is already quoted or
 * embedded in a heredoc. `$${` and `%%{` render as literal `${` and `%{`.
 */
export function hclTemplateEscape(value: string): string {
  // Replacer functions, not replacement strings: "$$" in a replacement string
  // is String.replace's own escape for a literal "$", which would silently
  // collapse "$${" back to "${" and leave interpolation live.
  return value.replace(/\$\{/g, () => '$${').replace(/%\{/g, () => '%%{')
}

/** Quoted HCL string literal with interpolation disabled. */
export function hclQuote(value: unknown): string {
  return hclTemplateEscape(JSON.stringify(asText(value).replace(/\0/g, '')))
}

/**
 * Shell-quoted literal for use inside an HCL heredoc, which is itself a
 * template: quote for the shell first, then neutralise HCL interpolation.
 */
export function hclHeredocShellQuote(value: unknown): string {
  return hclTemplateEscape(shellQuote(value))
}

/**
 * HCL identifier for resource labels and references (`aws_iam_role.<ident>`).
 * Identifiers accept letters, digits, underscores and dashes and may not start
 * with a digit or dash.
 */
export function hclIdent(value: unknown, fallback = 'cloudpam'): string {
  const cleaned = asText(value).replace(/[^A-Za-z0-9_-]/g, '_')
  if (!cleaned || !/^[A-Za-z_]/.test(cleaned)) return fallback
  return cleaned
}

/**
 * Filename for a generated download. Strips path separators and leading dots so
 * a crafted agent name cannot escape the download directory.
 */
export function safeFilename(value: unknown, fallback = 'cloudpam-agent'): string {
  const cleaned = asText(value)
    .replace(/[^A-Za-z0-9._-]/g, '-')
    .replace(/^[.-]+/, '')
    .replace(/-+/g, '-')
  return cleaned || fallback
}
