import { Loader2, PlugZap, RotateCw } from 'lucide-react'
import { useAccountConnectivity, type ConnectivityState } from '../hooks/useAccountConnectivity'
import type { ConnectivityTestResult } from '../api/types'
import { formatTimeAgo } from '../utils/format'

const SOURCE_LABEL: Record<ConnectivityTestResult['source'], string> = {
  host: 'server identity',
  stored_credential: 'stored credential',
}

const PILL = 'inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium'
const OK_CLASS = 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300'
const FAILED_CLASS = 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300'
const NEUTRAL_CLASS = 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300'

export function describeConnectivity(result: ConnectivityTestResult): string {
  const outcome = result.status === 'ok'
    ? `Reached as ${result.identity ?? 'unknown identity'}`
    : `Failed: ${result.error ?? 'unknown error'}`
  return `${outcome} (${SOURCE_LABEL[result.source]}, ${result.latency_ms} ms, tested ${formatTimeAgo(result.tested_at)})`
}

function Pill({ state }: { state: ConnectivityState }) {
  switch (state.phase) {
    case 'untested':
      return null
    case 'testing':
      return (
        <span className={`${PILL} ${NEUTRAL_CLASS}`}>
          <Loader2 className="w-3 h-3 animate-spin" aria-hidden="true" />
          testing
        </span>
      )
    case 'error':
      return <span className={`${PILL} ${NEUTRAL_CLASS}`} title={state.message}>not tested</span>
    case 'tested': {
      const { result } = state
      const ok = result.status === 'ok'
      return (
        <span className={`${PILL} ${ok ? OK_CLASS : FAILED_CLASS}`} title={describeConnectivity(result)}>
          {ok ? `connected · ${result.latency_ms} ms` : 'failed'}
        </span>
      )
    }
  }
}

interface ConnectivityBadgeProps {
  accountId: number
  accountName: string
  showDetail?: boolean
}

export default function ConnectivityBadge({ accountId, accountName, showDetail = false }: ConnectivityBadgeProps) {
  const { state, test } = useAccountConnectivity(accountId)
  const testing = state.phase === 'testing'
  const detail = state.phase === 'tested' ? describeConnectivity(state.result) : state.phase === 'error' ? state.message : null

  return (
    <div className="inline-flex flex-col gap-1">
      <div className="inline-flex items-center gap-2">
        <Pill state={state} />
        <button
          type="button"
          onClick={() => { void test() }}
          disabled={testing}
          className="inline-flex items-center gap-1 text-xs text-gray-500 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 disabled:opacity-50"
          aria-label={`Test connectivity for ${accountName}`}
          title="Test connectivity"
        >
          {state.phase === 'untested'
            ? <><PlugZap className="w-3.5 h-3.5" aria-hidden="true" />Test</>
            : <RotateCw className="w-3.5 h-3.5" aria-hidden="true" />}
        </button>
      </div>
      {showDetail && detail && (
        <p className="text-xs text-gray-500 dark:text-gray-400 break-all" role="status">{detail}</p>
      )}
    </div>
  )
}
