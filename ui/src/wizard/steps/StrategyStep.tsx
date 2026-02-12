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
        <h2 className="text-xl font-semibold text-gray-900 mb-2">Allocation Strategy</h2>
        <p className="text-gray-600">How should your IP space be organized at the top level?</p>
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
                  ? 'border-blue-500 bg-blue-50'
                  : 'border-gray-200 hover:border-gray-300'
              }`}
            >
              <div className="flex items-start gap-3">
                <div className={`p-2 rounded-lg ${isSelected ? 'bg-blue-100' : 'bg-gray-100'}`}>
                  <Icon className={`w-5 h-5 ${isSelected ? 'text-blue-600' : 'text-gray-600'}`} />
                </div>
                <div className="flex-1">
                  <div className="flex items-center gap-2">
                    <h3 className="font-medium text-gray-900">{s.name}</h3>
                    {s.id === 'region-first' && (
                      <span className="text-xs bg-green-100 text-green-700 px-2 py-0.5 rounded">Recommended</span>
                    )}
                  </div>
                  <p className="text-sm text-gray-500 mt-1">{s.description}</p>
                  <div className="mt-2 p-2 bg-gray-100 rounded text-xs font-mono text-gray-600">
                    {s.example}
                  </div>
                  <p className="text-xs text-gray-400 mt-2 flex items-center gap-1">
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

      <div className="bg-gray-50 border border-gray-200 rounded-lg p-4">
        <div className="flex items-start gap-2">
          <Info className="w-5 h-5 text-gray-400 shrink-0 mt-0.5" />
          <div className="text-sm text-gray-600">
            <p className="font-medium text-gray-700">Why does this matter?</p>
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
