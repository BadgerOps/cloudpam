import { useState } from 'react'
import { ChevronRight, ChevronDown } from 'lucide-react'
import type { PoolWithStats } from '../api/types'
import { formatHostCount, getPoolTypeColor, getUtilizationColor } from '../utils/format'

interface PoolTreeProps {
  nodes: PoolWithStats[]
  selectedId?: number | null
  onSelect: (pool: PoolWithStats) => void
}

export default function PoolTree({ nodes, selectedId, onSelect }: PoolTreeProps) {
  const [expandAll, setExpandAll] = useState(false)

  return (
    <div>
      <div className="flex gap-2 mb-3">
        <button
          onClick={() => setExpandAll(true)}
          className="text-xs text-blue-600 hover:text-blue-800"
        >
          Expand All
        </button>
        <button
          onClick={() => setExpandAll(false)}
          className="text-xs text-blue-600 hover:text-blue-800"
        >
          Collapse All
        </button>
      </div>
      <div className="space-y-0.5">
        {nodes.map(node => (
          <TreeNode
            key={node.id}
            node={node}
            depth={0}
            selectedId={selectedId}
            onSelect={onSelect}
            forceExpand={expandAll}
          />
        ))}
      </div>
    </div>
  )
}

interface TreeNodeProps {
  node: PoolWithStats
  depth: number
  selectedId?: number | null
  onSelect: (pool: PoolWithStats) => void
  forceExpand: boolean
}

function TreeNode({ node, depth, selectedId, onSelect, forceExpand }: TreeNodeProps) {
  const [expanded, setExpanded] = useState(depth < 1)
  const isExpanded = forceExpand || expanded
  const hasChildren = node.children && node.children.length > 0
  const isSelected = selectedId === node.id

  const utilPct = node.stats?.utilization ?? 0
  const totalIPs = node.stats?.total_ips ?? 0

  return (
    <div>
      <div
        className={`flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer text-sm
          ${isSelected ? 'bg-blue-50 border border-blue-200' : 'hover:bg-gray-50'}`}
        style={{ paddingLeft: `${depth * 20 + 8}px` }}
        onClick={() => onSelect(node)}
      >
        {hasChildren ? (
          <button
            onClick={e => { e.stopPropagation(); setExpanded(!expanded) }}
            className="p-0.5 hover:bg-gray-200 rounded"
          >
            {isExpanded
              ? <ChevronDown className="w-3.5 h-3.5 text-gray-500" />
              : <ChevronRight className="w-3.5 h-3.5 text-gray-500" />}
          </button>
        ) : (
          <span className="w-4.5" />
        )}
        <span className={`w-2 h-2 rounded-full flex-shrink-0 ${getPoolTypeColor(node.type)}`} />
        <span className="font-mono text-xs text-gray-500">{node.cidr}</span>
        <span className="text-gray-900 truncate">{node.name}</span>
        <span className="ml-auto flex items-center gap-2 flex-shrink-0">
          <span className="text-xs text-gray-400">{formatHostCount(totalIPs)} IPs</span>
          {utilPct > 0 && (
            <div className="w-16 h-1.5 bg-gray-200 rounded-full overflow-hidden">
              <div
                className={`h-full rounded-full ${getUtilizationColor(utilPct)}`}
                style={{ width: `${Math.min(utilPct, 100)}%` }}
              />
            </div>
          )}
        </span>
      </div>
      {isExpanded && hasChildren && (
        <div>
          {node.children!.map(child => (
            <TreeNode
              key={child.id}
              node={child}
              depth={depth + 1}
              selectedId={selectedId}
              onSelect={onSelect}
              forceExpand={forceExpand}
            />
          ))}
        </div>
      )}
    </div>
  )
}
