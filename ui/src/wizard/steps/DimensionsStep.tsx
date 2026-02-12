import { useState } from 'react'
import { Globe, Server, Building, Trash2, Plus, Lightbulb } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

export interface Dimensions {
  regions: string[]
  environments: string[]
  accountTiers: string[]
  accountsPerEnv: number
  growthYears: number
}

interface Props {
  dimensions: Dimensions
  setDimensions: React.Dispatch<React.SetStateAction<Dimensions>>
  strategy: string
}

export default function DimensionsStep({ dimensions, setDimensions, strategy }: Props) {
  const [newValue, setNewValue] = useState('')
  const [addingTo, setAddingTo] = useState<string | null>(null)

  const handleAdd = (dimension: string) => {
    setAddingTo(dimension)
    setNewValue('')
  }

  const confirmAdd = (dimension: string) => {
    if (newValue.trim()) {
      setDimensions((prev) => ({
        ...prev,
        [dimension]: [...(prev[dimension as keyof Dimensions] as string[]), newValue.trim()],
      }))
      setNewValue('')
      setAddingTo(null)
    }
  }

  const removeItem = (dimension: string, index: number) => {
    setDimensions((prev) => ({
      ...prev,
      [dimension]: (prev[dimension as keyof Dimensions] as string[]).filter((_, i) => i !== index),
    }))
  }

  const renderDimensionList = (title: string, dimension: string, items: string[], icon: LucideIcon) => (
    <div className="space-y-2">
      <label className="flex items-center gap-2 text-sm font-medium text-gray-700">
        {(() => { const I = icon; return <I className="w-4 h-4" /> })()}
        {title}
      </label>
      <div className="flex flex-wrap gap-2">
        {items.map((item, i) => (
          <span
            key={i}
            className="inline-flex items-center gap-1 px-3 py-1 bg-blue-100 text-blue-800 rounded-full text-sm"
          >
            {item}
            <button onClick={() => removeItem(dimension, i)} className="hover:bg-blue-200 rounded-full p-0.5">
              <Trash2 className="w-3 h-3" />
            </button>
          </span>
        ))}
        {addingTo === dimension ? (
          <form
            onSubmit={(e) => { e.preventDefault(); confirmAdd(dimension) }}
            className="inline-flex items-center gap-1"
          >
            <input
              autoFocus
              type="text"
              value={newValue}
              onChange={(e) => setNewValue(e.target.value)}
              onBlur={() => { if (!newValue.trim()) setAddingTo(null) }}
              onKeyDown={(e) => { if (e.key === 'Escape') setAddingTo(null) }}
              placeholder="Enter name..."
              className="px-2 py-1 text-sm border border-gray-300 rounded-full w-32 focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
          </form>
        ) : (
          <button
            onClick={() => handleAdd(dimension)}
            className="inline-flex items-center gap-1 px-3 py-1 border-2 border-dashed border-gray-300 text-gray-500 rounded-full text-sm hover:border-gray-400 hover:text-gray-600"
          >
            <Plus className="w-3 h-3" />
            Add
          </button>
        )}
      </div>
    </div>
  )

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold text-gray-900 mb-2">Define Your Dimensions</h2>
        <p className="text-gray-600">What are the organizational boundaries for your IP space?</p>
      </div>

      <div className="bg-white border border-gray-200 rounded-lg p-6 space-y-6">
        {strategy === 'region-first' && renderDimensionList('Regions', 'regions', dimensions.regions, Globe)}
        {renderDimensionList('Environments', 'environments', dimensions.environments, Server)}
        {renderDimensionList('Account Tiers', 'accountTiers', dimensions.accountTiers, Building)}

        <div className="grid grid-cols-2 gap-4 pt-4 border-t">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Accounts per environment (estimated)
            </label>
            <input
              type="number"
              min="1"
              max="100"
              value={dimensions.accountsPerEnv}
              onChange={(e) =>
                setDimensions((prev) => ({ ...prev, accountsPerEnv: parseInt(e.target.value) || 1 }))
              }
              className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Growth horizon (years)
            </label>
            <select
              value={dimensions.growthYears}
              onChange={(e) =>
                setDimensions((prev) => ({ ...prev, growthYears: parseInt(e.target.value) }))
              }
              className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500"
            >
              <option value={1}>1 year</option>
              <option value={3}>3 years</option>
              <option value={5}>5 years</option>
              <option value={10}>10 years</option>
            </select>
          </div>
        </div>
      </div>

      <div className="bg-amber-50 border border-amber-200 rounded-lg p-4">
        <div className="flex items-start gap-2">
          <Lightbulb className="w-5 h-5 text-amber-500 shrink-0 mt-0.5" />
          <div className="text-sm text-amber-800">
            <p className="font-medium">Capacity Planning Tip</p>
            <p className="mt-1">
              Based on your inputs, you'll need approximately{' '}
              <strong>
                {dimensions.regions.length * dimensions.environments.length * dimensions.accountsPerEnv}
              </strong>{' '}
              account allocations. With {dimensions.growthYears}x growth buffer, plan for{' '}
              <strong>
                {dimensions.regions.length *
                  dimensions.environments.length *
                  dimensions.accountsPerEnv *
                  dimensions.growthYears}
              </strong>{' '}
              total.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
