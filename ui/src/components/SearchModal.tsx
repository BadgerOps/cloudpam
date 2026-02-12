import { useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { Search, Server, Cloud, Loader2 } from 'lucide-react'
import { useSearch } from '../hooks/useSearch'
import StatusBadge from './StatusBadge'

interface SearchModalProps {
  open: boolean
  onClose: () => void
}

export default function SearchModal({ open, onClose }: SearchModalProps) {
  const navigate = useNavigate()
  const { query, setQuery, results, loading, error } = useSearch()
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (open) {
      setQuery('')
      requestAnimationFrame(() => inputRef.current?.focus())
    }
  }, [open, setQuery])

  function handleSelect(type: 'pool' | 'account') {
    navigate(type === 'pool' ? '/pools' : '/accounts')
    onClose()
  }

  if (!open) return null

  const poolResults = results?.items.filter(i => i.type === 'pool') ?? []
  const accountResults = results?.items.filter(i => i.type === 'account') ?? []
  const hasResults = poolResults.length > 0 || accountResults.length > 0

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh] bg-black/40"
      onClick={onClose}
    >
      <div
        className="bg-white dark:bg-gray-800 rounded-xl shadow-2xl w-full max-w-2xl overflow-hidden"
        onClick={e => e.stopPropagation()}
      >
        {/* Search input */}
        <div className="flex items-center gap-3 px-4 py-3 border-b dark:border-gray-700">
          {loading ? (
            <Loader2 className="w-5 h-5 text-blue-500 flex-shrink-0 animate-spin" />
          ) : (
            <Search className="w-5 h-5 text-gray-400 dark:text-gray-500 flex-shrink-0" />
          )}
          <input
            ref={inputRef}
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="Search pools, accounts, or enter an IP address..."
            className="flex-1 text-sm outline-none dark:bg-gray-800 dark:text-gray-100 dark:placeholder-gray-400"
          />
          <kbd className="text-xs text-gray-400 dark:text-gray-500 bg-gray-100 dark:bg-gray-700 px-1.5 py-0.5 rounded">Esc</kbd>
        </div>

        {/* Results */}
        <div className="max-h-[50vh] overflow-auto">
          {!query.trim() ? (
            <div className="p-6 text-center text-sm text-gray-400 dark:text-gray-500">
              Search by name, CIDR, or IP address...
            </div>
          ) : error ? (
            <div className="p-6 text-center text-sm text-red-500">
              {error}
            </div>
          ) : !loading && !hasResults ? (
            <div className="p-6 text-center text-sm text-gray-400 dark:text-gray-500">
              No results for &ldquo;{query}&rdquo;
            </div>
          ) : hasResults ? (
            <div>
              {poolResults.length > 0 && (
                <div>
                  <div className="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase bg-gray-50 dark:bg-gray-800">
                    Pools
                    {results && <span className="ml-1 normal-case">({poolResults.length})</span>}
                  </div>
                  {poolResults.map(item => (
                    <button
                      key={`pool-${item.id}`}
                      onClick={() => handleSelect('pool')}
                      className="w-full flex items-center gap-3 px-4 py-2 text-sm hover:bg-blue-50 dark:hover:bg-blue-900/30 text-left"
                    >
                      <Server className="w-4 h-4 text-gray-400 dark:text-gray-500 flex-shrink-0" />
                      <span className="font-mono text-xs text-gray-500 dark:text-gray-400 w-32 flex-shrink-0">{item.cidr}</span>
                      <span className="text-gray-900 dark:text-gray-100 truncate">{item.name}</span>
                      <span className="ml-auto flex gap-1.5 flex-shrink-0">
                        {item.pool_type && <StatusBadge label={item.pool_type} variant="type" />}
                        {item.status && <StatusBadge label={item.status} />}
                      </span>
                    </button>
                  ))}
                </div>
              )}
              {accountResults.length > 0 && (
                <div>
                  <div className="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase bg-gray-50 dark:bg-gray-800">
                    Accounts
                    <span className="ml-1 normal-case">({accountResults.length})</span>
                  </div>
                  {accountResults.map(item => (
                    <button
                      key={`account-${item.id}`}
                      onClick={() => handleSelect('account')}
                      className="w-full flex items-center gap-3 px-4 py-2 text-sm hover:bg-blue-50 dark:hover:bg-blue-900/30 text-left"
                    >
                      <Cloud className="w-4 h-4 text-gray-400 dark:text-gray-500 flex-shrink-0" />
                      <span className="text-gray-900 dark:text-gray-100">{item.name}</span>
                      {item.provider && <StatusBadge label={item.provider} variant="provider" />}
                      <span className="ml-auto text-xs text-gray-400 dark:text-gray-500 font-mono">{item.account_key}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          ) : null}
        </div>
      </div>
    </div>
  )
}
