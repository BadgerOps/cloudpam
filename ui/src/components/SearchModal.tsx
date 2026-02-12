import { useState, useEffect, useMemo, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { Search, Server, Cloud } from 'lucide-react'
import { usePools } from '../hooks/usePools'
import { useAccounts } from '../hooks/useAccounts'
import StatusBadge from './StatusBadge'

interface SearchModalProps {
  open: boolean
  onClose: () => void
}

export default function SearchModal({ open, onClose }: SearchModalProps) {
  const navigate = useNavigate()
  const { pools, fetchPools } = usePools()
  const { accounts, fetchAccounts } = useAccounts()
  const [query, setQuery] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (open) {
      fetchPools()
      fetchAccounts()
      setQuery('')
      // Focus input after render
      requestAnimationFrame(() => inputRef.current?.focus())
    }
  }, [open, fetchPools, fetchAccounts])

  const results = useMemo(() => {
    if (!query.trim()) return { pools: [], accounts: [] }
    const q = query.toLowerCase()
    return {
      pools: pools.filter(p =>
        p.name.toLowerCase().includes(q) ||
        p.cidr.includes(q) ||
        (p.description || '').toLowerCase().includes(q)
      ).slice(0, 8),
      accounts: accounts.filter(a =>
        a.name.toLowerCase().includes(q) ||
        a.key.toLowerCase().includes(q) ||
        (a.provider || '').toLowerCase().includes(q)
      ).slice(0, 5),
    }
  }, [query, pools, accounts])

  function handleSelect(type: 'pool' | 'account') {
    navigate(type === 'pool' ? '/pools' : '/accounts')
    onClose()
  }

  if (!open) return null

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
          <Search className="w-5 h-5 text-gray-400 dark:text-gray-500 flex-shrink-0" />
          <input
            ref={inputRef}
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="Search pools, accounts..."
            className="flex-1 text-sm outline-none dark:bg-gray-800 dark:text-gray-100 dark:placeholder-gray-400"
          />
          <kbd className="text-xs text-gray-400 dark:text-gray-500 bg-gray-100 dark:bg-gray-700 px-1.5 py-0.5 rounded">Esc</kbd>
        </div>

        {/* Results */}
        <div className="max-h-[50vh] overflow-auto">
          {!query.trim() ? (
            <div className="p-6 text-center text-sm text-gray-400 dark:text-gray-500">
              Start typing to search pools and accounts...
            </div>
          ) : results.pools.length === 0 && results.accounts.length === 0 ? (
            <div className="p-6 text-center text-sm text-gray-400 dark:text-gray-500">
              No results for "{query}"
            </div>
          ) : (
            <div>
              {results.pools.length > 0 && (
                <div>
                  <div className="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase bg-gray-50 dark:bg-gray-800">Pools</div>
                  {results.pools.map(p => (
                    <button
                      key={p.id}
                      onClick={() => handleSelect('pool')}
                      className="w-full flex items-center gap-3 px-4 py-2 text-sm hover:bg-blue-50 dark:hover:bg-blue-900/30 text-left"
                    >
                      <Server className="w-4 h-4 text-gray-400 dark:text-gray-500 flex-shrink-0" />
                      <span className="font-mono text-xs text-gray-500 dark:text-gray-400 w-32 flex-shrink-0">{p.cidr}</span>
                      <span className="text-gray-900 dark:text-gray-100 truncate">{p.name}</span>
                      <span className="ml-auto flex gap-1.5 flex-shrink-0">
                        <StatusBadge label={p.type} variant="type" />
                        <StatusBadge label={p.status} />
                      </span>
                    </button>
                  ))}
                </div>
              )}
              {results.accounts.length > 0 && (
                <div>
                  <div className="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase bg-gray-50 dark:bg-gray-800">Accounts</div>
                  {results.accounts.map(a => (
                    <button
                      key={a.id}
                      onClick={() => handleSelect('account')}
                      className="w-full flex items-center gap-3 px-4 py-2 text-sm hover:bg-blue-50 dark:hover:bg-blue-900/30 text-left"
                    >
                      <Cloud className="w-4 h-4 text-gray-400 dark:text-gray-500 flex-shrink-0" />
                      <span className="text-gray-900 dark:text-gray-100">{a.name}</span>
                      <StatusBadge label={a.provider || 'other'} variant="provider" />
                      <span className="ml-auto text-xs text-gray-400 dark:text-gray-500 font-mono">{a.key}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
