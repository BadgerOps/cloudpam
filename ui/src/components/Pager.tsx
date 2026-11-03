import { useEffect } from 'react'

interface PagerProps {
  page: number
  pageSize: number
  total: number
  noun: string
  loading?: boolean
  onPageChange: (page: number) => void
}

export function lastPageOf(total: number, pageSize: number): number {
  if (pageSize <= 0) return 1
  return Math.max(1, Math.ceil(total / pageSize))
}

export default function Pager({ page, pageSize, total, noun, loading = false, onPageChange }: PagerProps) {
  const lastPage = lastPageOf(total, pageSize)

  // A delete on the last page can leave the requested page past the end;
  // step back so the list never shows an empty page while items remain.
  useEffect(() => {
    if (page > lastPage) onPageChange(lastPage)
  }, [page, lastPage, onPageChange])

  if (total === 0) return null

  const start = (page - 1) * pageSize + 1
  const end = Math.min(page * pageSize, total)
  const buttonClass =
    'px-3 py-1 border dark:border-gray-700 rounded hover:bg-gray-50 dark:hover:bg-gray-700/50 disabled:opacity-40'

  // `page` is the page the rows came from. While a request is pending it can
  // lag the active query (a filter change resets to page 1), so stepping from
  // it would request the wrong page.
  return (
    <div className="flex items-center justify-between mt-4 text-sm text-gray-500 dark:text-gray-400">
      <span>
        Showing {Math.min(start, total)}-{end} of {total} {noun}
      </span>
      <div className="flex gap-2">
        <button onClick={() => onPageChange(page - 1)} disabled={loading || page <= 1} className={buttonClass}>
          Previous
        </button>
        <button onClick={() => onPageChange(page + 1)} disabled={loading || page >= lastPage} className={buttonClass}>
          Next
        </button>
      </div>
    </div>
  )
}
