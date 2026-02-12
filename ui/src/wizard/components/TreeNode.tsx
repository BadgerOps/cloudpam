import { useState } from 'react'
import { ChevronRight, ChevronDown, AlertTriangle } from 'lucide-react'
import type { SchemaNode } from '../utils/cidr'
import { usableHosts, formatHostCount } from '../utils/cidr'

const TYPE_COLORS: Record<string, string> = {
  root: 'bg-purple-500',
  region: 'bg-blue-500',
  environment: 'bg-green-500',
  account: 'bg-amber-500',
  subnet: 'bg-gray-400',
}

interface Props {
  node: SchemaNode
  depth?: number
  defaultExpanded?: Set<string>
}

export default function TreeNode({ node, depth = 0, defaultExpanded }: Props) {
  const [expanded, setExpanded] = useState(defaultExpanded?.has(node.id) ?? depth < 2)
  const hasChildren = node.children && node.children.length > 0
  const hosts = usableHosts(parseInt(node.cidr.split('/')[1]))

  return (
    <div className="select-none">
      <div
        className={`flex items-center gap-2 py-1.5 px-2 rounded hover:bg-gray-100 cursor-pointer ${
          node.conflict ? 'bg-red-50 hover:bg-red-100' : ''
        }`}
        style={{ paddingLeft: `${depth * 20 + 8}px` }}
        onClick={() => hasChildren && setExpanded(!expanded)}
      >
        {hasChildren ? (
          expanded ? (
            <ChevronDown className="w-4 h-4 text-gray-400" />
          ) : (
            <ChevronRight className="w-4 h-4 text-gray-400" />
          )
        ) : (
          <div className="w-4" />
        )}

        <div className={`w-2 h-2 rounded-full ${TYPE_COLORS[node.type] ?? 'bg-gray-400'}`} />

        <span className="font-medium text-gray-900">{node.name}</span>
        <code className="text-xs bg-gray-100 px-1.5 py-0.5 rounded font-mono text-gray-600">
          {node.cidr}
        </code>
        <span className="text-xs text-gray-400">({formatHostCount(hosts)} usable)</span>

        {node.conflict && (
          <span className="flex items-center gap-1 text-xs text-red-600 bg-red-100 px-2 py-0.5 rounded">
            <AlertTriangle className="w-3 h-3" />
            Conflict
          </span>
        )}
      </div>

      {hasChildren && expanded && (
        <div>
          {node.children.map((child) => (
            <TreeNode key={child.id} node={child} depth={depth + 1} defaultExpanded={defaultExpanded} />
          ))}
        </div>
      )}
    </div>
  )
}
