import { useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { AlertCircle, ArrowDown, ArrowUp, Loader2 } from 'lucide-react'
import { EmptyState } from './EmptyState'

export type ColumnPriority = 1 | 2 | 3

export interface Column<T> {
  key: string
  label: string
  width?: string
  align?: 'left' | 'right'
  priority?: ColumnPriority
  sortable?: boolean
  render?: (row: T) => ReactNode
}

export interface DataTableProps<T> {
  columns: Column<T>[]
  rows: T[]
  rowKey: (row: T) => string
  loading?: boolean
  error?: string
  filtered?: boolean
  empty?: ReactNode
  emptyFiltered?: ReactNode
  toolbar?: ReactNode
}

type Phase = 'loading' | 'error' | 'empty' | 'empty-filtered' | 'populated'

function phaseOf<T>({ loading, error, rows, filtered }: DataTableProps<T>): Phase {
  if (loading) return 'loading'
  if (error) return 'error'
  if (rows.length === 0) return filtered ? 'empty-filtered' : 'empty'
  return 'populated'
}

const PRIORITY_CLASS: Record<ColumnPriority, string> = {
  1: '',
  2: 'hidden md:table-cell',
  3: 'hidden lg:table-cell',
}

const ALIGN_CLASS: Record<'left' | 'right', string> = {
  left: 'text-left',
  right: 'text-right',
}

type Sort = { key: string; direction: 'asc' | 'desc' }

function cellValue<T>(row: T, key: string): unknown {
  return (row as Record<string, unknown>)[key]
}

function compare(a: unknown, b: unknown): number {
  if (typeof a === 'number' && typeof b === 'number') return a - b
  return String(a ?? '').localeCompare(String(b ?? ''), undefined, { numeric: true })
}

export function DataTable<T>(props: DataTableProps<T>) {
  const { columns, rows, rowKey, error, empty, emptyFiltered, toolbar } = props
  const [sort, setSort] = useState<Sort | null>(null)
  const phase = phaseOf(props)

  const sorted = useMemo(() => {
    if (!sort) return rows
    const sign = sort.direction === 'asc' ? 1 : -1
    return [...rows].sort((a, b) => sign * compare(cellValue(a, sort.key), cellValue(b, sort.key)))
  }, [rows, sort])

  function toggleSort(key: string) {
    setSort(prev => (prev?.key === key && prev.direction === 'asc' ? { key, direction: 'desc' } : { key, direction: 'asc' }))
  }

  function cellClass(column: Column<T>): string {
    return `px-4 py-2 ${ALIGN_CLASS[column.align ?? 'left']} ${PRIORITY_CLASS[column.priority ?? 1]}`
  }

  const body: Record<Phase, () => ReactNode> = {
    loading: () => (
      <div className="flex items-center justify-center gap-2 py-12 text-sm text-gray-500 dark:text-gray-400" role="status">
        <Loader2 className="w-4 h-4 animate-spin" />
        Loading...
      </div>
    ),
    error: () => (
      <div className="flex items-center gap-2 m-4 px-4 py-3 rounded-lg text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20" role="alert">
        <AlertCircle className="w-4 h-4 flex-shrink-0" />
        {error}
      </div>
    ),
    empty: () => empty ?? <EmptyState title="Nothing here yet" />,
    'empty-filtered': () => emptyFiltered ?? <EmptyState title="No matches" description="Clear the filter to see every record." />,
    populated: () => (
      <table className="w-full text-sm">
        <thead className="bg-gray-50 dark:bg-gray-900/40 text-xs uppercase text-gray-500 dark:text-gray-400">
          <tr>
            {columns.map(column => {
              const active = sort?.key === column.key
              const ariaSort = !column.sortable ? undefined : active ? (sort.direction === 'asc' ? 'ascending' : 'descending') : 'none'
              return (
                <th
                  key={column.key}
                  scope="col"
                  style={column.width ? { width: column.width } : undefined}
                  aria-sort={ariaSort}
                  className={`font-medium ${cellClass(column)}`}
                >
                  {column.sortable ? (
                    <button
                      type="button"
                      onClick={() => toggleSort(column.key)}
                      className="inline-flex items-center gap-1 uppercase hover:text-gray-900 dark:hover:text-white"
                    >
                      {column.label}
                      {active && (sort.direction === 'asc' ? <ArrowUp className="w-3 h-3" /> : <ArrowDown className="w-3 h-3" />)}
                    </button>
                  ) : (
                    column.label
                  )}
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody className="divide-y dark:divide-gray-700 text-gray-700 dark:text-gray-300">
          {sorted.map(row => (
            <tr key={rowKey(row)} className="hover:bg-gray-50 dark:hover:bg-gray-700/40">
              {columns.map(column => (
                <td key={column.key} className={cellClass(column)}>
                  {column.render ? column.render(row) : String(cellValue(row, column.key) ?? '')}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    ),
  }

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg shadow border dark:border-gray-700 overflow-hidden">
      {toolbar && <div className="px-4 py-3 border-b dark:border-gray-700">{toolbar}</div>}
      <div className="overflow-x-auto">{body[phase]()}</div>
    </div>
  )
}
