import { useEffect, useState } from 'react'
import { RotateCcw } from 'lucide-react'
import { useRecentlyDeleted } from '../hooks/useRecentlyDeleted'
import { useAuth } from '../hooks/useAuth'
import { useToast } from '../hooks/useToast'
import { formatTimeAgo } from '../utils/format'

const TH = 'px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase'

function RestoreButton({ label, disabledReason, busy, onClick }: {
  label: string
  disabledReason?: string
  busy: boolean
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      disabled={busy || !!disabledReason}
      title={disabledReason}
      aria-label={label}
      className="inline-flex items-center gap-1 px-2 py-1 text-sm rounded text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/30 disabled:opacity-40 disabled:cursor-not-allowed"
    >
      <RotateCcw className="w-4 h-4" />
      {busy ? 'Restoring...' : 'Restore'}
    </button>
  )
}

function Empty({ children }: { children: string }) {
  return <p className="text-sm text-gray-500 dark:text-gray-400 py-6 text-center">{children}</p>
}

export default function RecentlyDeletedPage() {
  const { hasPermission } = useAuth()
  const canPools = hasPermission('pools:delete')
  const canAccounts = hasPermission('accounts:delete')
  const { pools, accounts, loading, error, fetchDeleted, restorePool, restoreAccount } = useRecentlyDeleted()
  const { showToast } = useToast()
  const [busy, setBusy] = useState<string | null>(null)

  useEffect(() => {
    fetchDeleted({ pools: canPools, accounts: canAccounts })
  }, [fetchDeleted, canPools, canAccounts])

  const deletedPoolNames = new Map(pools.map(p => [p.id, p.name]))

  async function restore(key: string, name: string, run: () => Promise<unknown>) {
    setBusy(key)
    try {
      await run()
      showToast(`Restored ${name}`, 'success')
    } catch (err) {
      showToast(err instanceof Error ? err.message : `Failed to restore ${name}`, 'error')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="flex-1 overflow-auto p-6 space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Recently Deleted</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
          Deleted pools and accounts stay here until restored. Restoring brings back one record: restore a parent pool before its children, and an account before its pools.
        </p>
      </div>

      {error && (
        <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm p-3 rounded">{error}</div>
      )}

      {canPools && (
        <section aria-labelledby="deleted-pools">
          <h2 id="deleted-pools" className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-2">Pools</h2>
          <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg overflow-hidden">
            {loading ? <Empty>Loading...</Empty> : pools.length === 0 ? <Empty>No deleted pools.</Empty> : (
              <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
                <thead className="bg-gray-50 dark:bg-gray-800">
                  <tr>
                    <th className={TH}>Name</th>
                    <th className={TH}>CIDR</th>
                    <th className={`hidden md:table-cell ${TH}`}>Deleted</th>
                    <th className={`${TH} text-right`}>Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100 dark:divide-gray-700">
                  {pools.map(p => {
                    const deletedParent = p.parent_id != null ? deletedPoolNames.get(p.parent_id) : undefined
                    return (
                      <tr key={p.id}>
                        <td className="px-4 py-2 text-sm font-medium text-gray-900 dark:text-gray-100">
                          {p.name}
                          {deletedParent && <span className="block text-xs font-normal text-gray-500 dark:text-gray-400">inside deleted pool {deletedParent}</span>}
                        </td>
                        <td className="px-4 py-2 text-sm font-mono text-gray-600 dark:text-gray-300">{p.cidr}</td>
                        <td className="hidden md:table-cell px-4 py-2 text-sm text-gray-400 dark:text-gray-500">{formatTimeAgo(p.deleted_at ?? '')}</td>
                        <td className="px-4 py-2 text-right">
                          <RestoreButton
                            label={`Restore pool ${p.name}`}
                            disabledReason={deletedParent ? `Restore ${deletedParent} first` : undefined}
                            busy={busy === `pool-${p.id}`}
                            onClick={() => restore(`pool-${p.id}`, p.name, () => restorePool(p.id))}
                          />
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            )}
          </div>
        </section>
      )}

      {canAccounts && (
        <section aria-labelledby="deleted-accounts">
          <h2 id="deleted-accounts" className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-2">Accounts</h2>
          <div className="bg-white dark:bg-gray-800 border dark:border-gray-700 rounded-lg overflow-hidden">
            {loading ? <Empty>Loading...</Empty> : accounts.length === 0 ? <Empty>No deleted accounts.</Empty> : (
              <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
                <thead className="bg-gray-50 dark:bg-gray-800">
                  <tr>
                    <th className={TH}>Name</th>
                    <th className={TH}>Key</th>
                    <th className={`hidden md:table-cell ${TH}`}>Deleted</th>
                    <th className={`${TH} text-right`}>Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100 dark:divide-gray-700">
                  {accounts.map(a => (
                    <tr key={a.id}>
                      <td className="px-4 py-2 text-sm font-medium text-gray-900 dark:text-gray-100">{a.name}</td>
                      <td className="px-4 py-2 text-sm font-mono text-gray-600 dark:text-gray-300">{a.key}</td>
                      <td className="hidden md:table-cell px-4 py-2 text-sm text-gray-400 dark:text-gray-500">{formatTimeAgo(a.deleted_at ?? '')}</td>
                      <td className="px-4 py-2 text-right">
                        <RestoreButton
                          label={`Restore account ${a.name}`}
                          busy={busy === `account-${a.id}`}
                          onClick={() => restore(`account-${a.id}`, a.name, () => restoreAccount(a.id))}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </section>
      )}
    </div>
  )
}
