import { Link } from 'react-router-dom'
import { MetricCard } from './primitives'
import type { MetricTone } from './primitives'
import type { AttentionLoad, AttentionSource } from '../hooks/useAttention'

export type AttentionCardProps = Pick<AttentionSource, 'title' | 'icon' | 'link'> & { load: AttentionLoad }

const DOT: Record<MetricTone, string> = {
  neutral: 'bg-gray-300 dark:bg-gray-600',
  ok: 'bg-green-500',
  info: 'bg-blue-500',
  warning: 'bg-amber-500',
  critical: 'bg-red-500',
}

export default function AttentionCard({ title, icon: Icon, link, load }: AttentionCardProps) {
  const summary = load.state === 'ready' ? load.summary : null
  return (
    <MetricCard
      label={title}
      icon={<Icon className="w-5 h-5 text-gray-500 dark:text-gray-400" />}
      loading={load.state === 'loading'}
      value={summary?.value ?? '—'}
      tone={summary?.tone ?? 'neutral'}
      detail={
        load.state === 'error' ? (
          <span className="text-red-600 dark:text-red-400">Could not load: {load.message}</span>
        ) : (
          summary?.detail
        )
      }
      action={link}
    >
      {summary && summary.items.length > 0 && (
        <ul className="space-y-1.5">
          {summary.items.map(item => (
            <li key={item.key}>
              <Link to={item.to} className="group flex items-start gap-2 text-sm">
                <span aria-hidden className={`mt-1.5 w-2 h-2 rounded-full flex-shrink-0 ${DOT[item.tone]}`} />
                <span className="min-w-0">
                  <span className="block truncate text-gray-900 dark:text-gray-100 group-hover:text-blue-600 dark:group-hover:text-blue-400">
                    {item.label}
                  </span>
                  <span className="block truncate text-xs text-gray-500 dark:text-gray-400">{item.detail}</span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </MetricCard>
  )
}
