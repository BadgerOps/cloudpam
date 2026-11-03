import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { DataTable, EmptyState } from '../components/primitives'
import type { Column } from '../components/primitives'

type Key = { id: string; name: string; uses: number; created: string }

const columns: Column<Key>[] = [
  { key: 'name', label: 'Name', sortable: true },
  { key: 'uses', label: 'Uses', align: 'right', sortable: true, priority: 2 },
  { key: 'created', label: 'Created', priority: 3, render: row => <time>{row.created}</time> },
]

const rows: Key[] = [
  { id: 'b', name: 'beta', uses: 10, created: '2026-01-02' },
  { id: 'a', name: 'alpha', uses: 2, created: '2026-01-01' },
]

function bodyNames(): (string | null)[] {
  const body = screen.getByRole('table').querySelector('tbody') as HTMLElement
  return within(body).getAllByRole('row').map(row => within(row).getAllByRole('cell')[0].textContent)
}

describe('DataTable', () => {
  it('renders rows through column renderers and hides low-priority columns on narrow viewports', () => {
    render(<DataTable columns={columns} rows={rows} rowKey={r => r.id} />)

    expect(bodyNames()).toEqual(['beta', 'alpha'])
    expect(screen.getByText('2026-01-02').tagName).toBe('TIME')
    const headers = screen.getAllByRole('columnheader')
    expect(headers[0].className).not.toMatch(/hidden/)
    expect(headers[1].className).toMatch(/\bhidden md:table-cell\b/)
    expect(headers[2].className).toMatch(/\bhidden lg:table-cell\b/)
  })

  it('sorts a sortable column ascending then descending and reports it with aria-sort', () => {
    render(<DataTable columns={columns} rows={rows} rowKey={r => r.id} />)
    const uses = screen.getByRole('button', { name: 'Uses' })

    fireEvent.click(uses)
    expect(bodyNames()).toEqual(['alpha', 'beta'])
    expect(screen.getAllByRole('columnheader')[1].getAttribute('aria-sort')).toBe('ascending')

    fireEvent.click(uses)
    expect(bodyNames()).toEqual(['beta', 'alpha'])
    expect(screen.getAllByRole('columnheader')[1].getAttribute('aria-sort')).toBe('descending')
    expect(screen.getAllByRole('columnheader')[2].getAttribute('aria-sort')).toBeNull()
  })

  it('shows loading and error instead of the rows', () => {
    const { rerender } = render(<DataTable columns={columns} rows={rows} rowKey={r => r.id} loading />)
    expect(screen.getByRole('status').textContent).toMatch(/loading/i)
    expect(screen.queryByRole('table')).toBeNull()

    rerender(<DataTable columns={columns} rows={rows} rowKey={r => r.id} error="keys unavailable" />)
    expect(screen.getByRole('alert').textContent).toMatch('keys unavailable')
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('distinguishes never-configured from filtered-empty', () => {
    const empty = <EmptyState title="No API keys yet" action={{ label: 'Create key', onClick: () => {} }} />
    const emptyFiltered = <EmptyState title="No keys match" />
    const { rerender } = render(
      <DataTable columns={columns} rows={[]} rowKey={r => r.id} empty={empty} emptyFiltered={emptyFiltered} />,
    )
    expect(screen.getByText('No API keys yet')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Create key' })).toBeTruthy()

    rerender(
      <DataTable columns={columns} rows={[]} rowKey={r => r.id} filtered empty={empty} emptyFiltered={emptyFiltered} />,
    )
    expect(screen.getByText('No keys match')).toBeTruthy()
    expect(screen.queryByText('No API keys yet')).toBeNull()
  })

  it('never renders a bare empty table when the caller passes no empty slots', () => {
    const { rerender } = render(<DataTable columns={columns} rows={[]} rowKey={r => r.id} />)
    expect(screen.queryByRole('table')).toBeNull()
    expect(screen.getByText('Nothing here yet')).toBeTruthy()

    rerender(<DataTable columns={columns} rows={[]} rowKey={r => r.id} filtered />)
    expect(screen.getByText('No matches')).toBeTruthy()
  })

  it('renders the toolbar above every state', () => {
    render(<DataTable columns={columns} rows={[]} rowKey={r => r.id} loading toolbar={<input aria-label="Filter keys" />} />)

    expect(screen.getByLabelText('Filter keys')).toBeTruthy()
    expect(screen.getByRole('status')).toBeTruthy()
  })
})
