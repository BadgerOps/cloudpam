import { CheckCircle, Info, Lightbulb } from 'lucide-react'
import { ALLOCATION_STRATEGIES } from '../data/strategies'

interface Props {
  strategy: string
  setStrategy: (s: string) => void
}

export default function StrategyStep({ strategy, setStrategy }: Props) {
  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100 mb-2">Allocation Strategy</h2>
        <p className="text-gray-600 dark:text-gray-300">How should your IP space be organized at the top level?</p>
      </div>

      <div className="space-y-3">
        {ALLOCATION_STRATEGIES.map((s) => {
          const Icon = s.icon
          const isSelected = strategy === s.id
          return (
            <button
              key={s.id}
              onClick={() => setStrategy(s.id)}
              className={`w-full p-4 rounded-lg border-2 text-left transition-all ${
                isSelected
                  ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/30'
                  : 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-500'
              }`}
            >
              <div className="flex items-start gap-3">
                <div className={`p-2 rounded-lg ${isSelected ? 'bg-blue-100 dark:bg-blue-900' : 'bg-gray-100 dark:bg-gray-700'}`}>
                  <Icon className={`w-5 h-5 ${isSelected ? 'text-blue-600 dark:text-blue-400' : 'text-gray-600 dark:text-gray-300'}`} />
                </div>
                <div className="flex-1">
                  <div className="flex items-center gap-2">
                    <h3 className="font-medium text-gray-900 dark:text-gray-100">{s.name}</h3>
                    {s.id === 'region-first' && (
                      <span className="text-xs bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 px-2 py-0.5 rounded">Recommended</span>
                    )}
                  </div>
                  <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{s.description}</p>
                  <div className="mt-2 p-2 bg-gray-100 dark:bg-gray-700 rounded text-xs font-mono text-gray-600 dark:text-gray-300">
                    {s.example}
                  </div>
                  <p className="text-xs text-gray-400 dark:text-gray-500 mt-2 flex items-center gap-1">
                    <Lightbulb className="w-3 h-3" />
                    Best for: {s.best_for}
                  </p>
                </div>
                {isSelected && <CheckCircle className="w-5 h-5 text-blue-500 shrink-0" />}
              </div>
            </button>
          )
        })}
      </div>

      <div className="bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
        <div className="flex items-start gap-2">
          <Info className="w-5 h-5 text-gray-400 dark:text-gray-500 shrink-0 mt-0.5" />
          <div className="text-sm text-gray-600 dark:text-gray-300">
            <p className="font-medium text-gray-700 dark:text-gray-300">Why does this matter?</p>
            <p className="mt-1">
              Your allocation strategy determines how easy it is to add new regions, environments,
              or accounts later. Changing strategy after deployment requires re-IPing workloads,
              so choose based on your expected growth pattern.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
