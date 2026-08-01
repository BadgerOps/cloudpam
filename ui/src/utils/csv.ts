// Leading characters that spreadsheet applications interpret as the start of a
// formula. Prefixing with an apostrophe forces the cell to be read as text.
const FORMULA_PREFIXES = ['=', '+', '-', '@', '\t', '\r']

export function escapeCsvCell(value: unknown): string {
  let cell = value === null || value === undefined ? '' : String(value)

  if (cell.length > 0 && FORMULA_PREFIXES.includes(cell[0])) {
    cell = `'${cell}`
  }

  // RFC 4180: quote the field and double any embedded quotes.
  if (/[",\n\r]/.test(cell)) {
    cell = `"${cell.replace(/"/g, '""')}"`
  }

  return cell
}

export function toCsvRow(cells: unknown[]): string {
  return cells.map(escapeCsvCell).join(',')
}

export function toCsv(header: string[], rows: unknown[][]): string {
  return [toCsvRow(header), ...rows.map(toCsvRow)].join('\n')
}
