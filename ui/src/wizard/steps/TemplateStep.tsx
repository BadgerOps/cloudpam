import { CheckCircle, ChevronRight } from 'lucide-react'
import type { Blueprint } from '../data/blueprints'
import { BLUEPRINTS } from '../data/blueprints'
import { RFC_SPACES } from '../data/rfcSpaces'
import { formatHostCount, hostCount } from '../utils/cidr'

interface Props {
  selectedBlueprint: Blueprint | null
  setSelectedBlueprint: (bp: Blueprint) => void
  customCidr: string
  setCustomCidr: (cidr: string) => void
}

export default function TemplateStep({ selectedBlueprint, setSelectedBlueprint, customCidr, setCustomCidr }: Props) {
  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100 mb-2">Choose a Starting Template</h2>
        <p className="text-gray-600 dark:text-gray-300">Select a blueprint that matches your organization's scale, or start from scratch.</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {BLUEPRINTS.map((bp) => {
          const Icon = bp.icon
          const isSelected = selectedBlueprint?.id === bp.id
          return (
            <button
              key={bp.id}
              onClick={() => setSelectedBlueprint(bp)}
              className={`p-4 rounded-lg border-2 text-left transition-all ${
                isSelected
                  ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/30 ring-2 ring-blue-200 dark:ring-blue-800'
                  : 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-500 hover:bg-gray-50 dark:hover:bg-gray-700/50'
              }`}
            >
              <div className="flex items-start gap-3">
                <div className={`p-2 rounded-lg ${isSelected ? 'bg-blue-100 dark:bg-blue-900' : 'bg-gray-100 dark:bg-gray-700'}`}>
                  <Icon className={`w-5 h-5 ${isSelected ? 'text-blue-600 dark:text-blue-400' : 'text-gray-600 dark:text-gray-300'}`} />
                </div>
                <div className="flex-1">
                  <h3 className="font-medium text-gray-900 dark:text-gray-100">{bp.name}</h3>
                  <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{bp.description}</p>
                  {bp.rootCidr && (
                    <div className="mt-2 text-xs font-mono bg-gray-100 dark:bg-gray-700 rounded px-2 py-1 inline-block">
                      {bp.rootCidr}
                    </div>
                  )}
                </div>
                {isSelected && <CheckCircle className="w-5 h-5 text-blue-500 shrink-0" />}
              </div>
            </button>
          )
        })}
      </div>

      {selectedBlueprint?.id === 'custom' && (
        <div className="bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-700 rounded-lg p-4">
          <h3 className="font-medium text-amber-800 dark:text-amber-300 mb-3">Define Your Root CIDR</h3>
          <div className="space-y-3">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Select RFC Space or Enter Custom</label>
              <div className="grid grid-cols-2 gap-2 mb-3">
                {RFC_SPACES.map((rfc) => (
                  <button
                    key={rfc.cidr}
                    onClick={() => setCustomCidr(rfc.cidr)}
                    className={`p-2 text-left rounded border text-sm ${
                      customCidr === rfc.cidr
                        ? 'border-amber-500 bg-amber-100 dark:bg-amber-900'
                        : 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-500'
                    }`}
                  >
                    <div className="font-mono text-xs">{rfc.cidr}</div>
                    <div className="text-gray-600 dark:text-gray-300 text-xs">{rfc.name} ({rfc.hosts})</div>
                  </button>
                ))}
              </div>
              <input
                type="text"
                value={customCidr}
                onChange={(e) => setCustomCidr(e.target.value)}
                placeholder="Or enter custom CIDR (e.g., 10.100.0.0/16)"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg focus:ring-2 focus:ring-amber-500 focus:border-amber-500 font-mono text-sm dark:bg-gray-700 dark:text-gray-100"
              />
            </div>
          </div>
        </div>
      )}

      {selectedBlueprint && selectedBlueprint.id !== 'custom' && (
        <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-700 rounded-lg p-4">
          <h3 className="font-medium text-blue-800 dark:text-blue-300 mb-2">Template Details</h3>
          <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm">
              <span className="text-gray-600 dark:text-gray-300">Root Space:</span>
              <code className="bg-blue-100 dark:bg-blue-900 px-2 py-0.5 rounded font-mono">{selectedBlueprint.rootCidr}</code>
              <span className="text-gray-500 dark:text-gray-400">
                ({formatHostCount(hostCount(parseInt(selectedBlueprint.rootCidr.split('/')[1])))} addresses)
              </span>
            </div>
            <div className="text-sm text-gray-600 dark:text-gray-300">
              <span className="font-medium">Hierarchy:</span>
              <div className="mt-1 ml-4 space-y-1">
                {selectedBlueprint.hierarchy.map((h, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <ChevronRight className="w-3 h-3 text-gray-400 dark:text-gray-500" />
                    <span className="capitalize">{h.level}</span>
                    <span className="text-gray-400 dark:text-gray-500">&rarr;</span>
                    <span className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">/{h.prefixSize}</span>
                    <span className="text-gray-500 dark:text-gray-400 text-xs">({h.description})</span>
                  </div>
                ))}
              </div>
            </div>
            <div className="text-sm">
              <span className="font-medium text-gray-600 dark:text-gray-300">Best for:</span>
              <ul className="mt-1 ml-4 text-gray-500 dark:text-gray-400">
                {selectedBlueprint.recommended.map((r, i) => (
                  <li key={i} className="flex items-center gap-1">
                    <CheckCircle className="w-3 h-3 text-green-500 dark:text-green-400" />
                    {r}
                  </li>
                ))}
              </ul>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
