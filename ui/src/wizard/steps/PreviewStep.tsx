import { Download, AlertTriangle, HelpCircle } from 'lucide-react'
import type { SchemaNode } from '../utils/cidr'
import { hostCount, formatHostCount, countLeafNodes } from '../utils/cidr'
import type { SchemaConflict } from '../../api/types'
import TreeNode from '../components/TreeNode'

interface Props {
  schema: SchemaNode
  conflicts: SchemaConflict[]
  conflictsLoading: boolean
  conflictsError: string | null
  onExport: (format: 'csv' | 'terraform') => void
}

export default function PreviewStep({ schema, conflicts, conflictsLoading, conflictsError, onExport }: Props) {
  const totalAddresses = hostCount(parseInt(schema.cidr.split('/')[1]))
  const topLevel = schema.children?.length ?? 0
  const totalPools = countLeafNodes(schema)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-semibold text-gray-900 mb-2">Preview Your Schema</h2>
          <p className="text-gray-600">Review the generated IP address plan before applying.</p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => onExport('terraform')}
            className="flex items-center gap-2 px-3 py-2 border border-gray-300 rounded-lg hover:bg-gray-50 text-sm"
          >
            <Download className="w-4 h-4" />
            Export Terraform
          </button>
          <button
            onClick={() => onExport('csv')}
            className="flex items-center gap-2 px-3 py-2 border border-gray-300 rounded-lg hover:bg-gray-50 text-sm"
          >
            <Download className="w-4 h-4" />
            Export CSV
          </button>
        </div>
      </div>

      {conflictsLoading && (
        <div className="bg-blue-50 border border-blue-200 rounded-lg p-4 text-sm text-blue-700">
          Checking for conflicts with existing pools...
        </div>
      )}

      {conflictsError && (
        <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-4 text-sm text-yellow-700">
          Could not check for conflicts: {conflictsError}. You can still apply the schema.
        </div>
      )}

      {conflicts.length > 0 && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4">
          <div className="flex items-start gap-2">
            <AlertTriangle className="w-5 h-5 text-red-500 shrink-0 mt-0.5" />
            <div>
              <h3 className="font-medium text-red-800">Conflicts Detected</h3>
              <p className="text-sm text-red-600 mt-1">
                {conflicts.length} allocation(s) overlap with existing pools in CloudPAM.
              </p>
              <ul className="mt-2 space-y-1">
                {conflicts.map((c, i) => (
                  <li key={i} className="text-sm text-red-700">
                    &bull; <code className="font-mono">{c.planned_cidr}</code> overlaps with existing pool &ldquo;{c.existing_pool_name}&rdquo; ({c.existing_cidr})
                  </li>
                ))}
              </ul>
            </div>
          </div>
        </div>
      )}

      <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
        <div className="bg-gray-50 px-4 py-3 border-b flex items-center justify-between">
          <h3 className="font-medium text-gray-900">Address Space Hierarchy</h3>
          <div className="flex items-center gap-4 text-xs text-gray-500">
            <span className="flex items-center gap-1"><div className="w-2 h-2 rounded-full bg-purple-500" /> Root</span>
            <span className="flex items-center gap-1"><div className="w-2 h-2 rounded-full bg-blue-500" /> Region</span>
            <span className="flex items-center gap-1"><div className="w-2 h-2 rounded-full bg-green-500" /> Environment</span>
            <span className="flex items-center gap-1"><div className="w-2 h-2 rounded-full bg-amber-500" /> Account</span>
          </div>
        </div>
        <div className="p-4 max-h-96 overflow-auto font-mono text-sm">
          <TreeNode node={schema} defaultExpanded={new Set(['root'])} />
        </div>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <div className="bg-blue-50 border border-blue-200 rounded-lg p-4 text-center">
          <div className="text-2xl font-bold text-blue-700">{formatHostCount(totalAddresses)}</div>
          <div className="text-sm text-blue-600">Total Addresses</div>
        </div>
        <div className="bg-green-50 border border-green-200 rounded-lg p-4 text-center">
          <div className="text-2xl font-bold text-green-700">{topLevel}</div>
          <div className="text-sm text-green-600">Top-Level Allocations</div>
        </div>
        <div className="bg-amber-50 border border-amber-200 rounded-lg p-4 text-center">
          <div className="text-2xl font-bold text-amber-700">{totalPools}</div>
          <div className="text-sm text-amber-600">Total Pools</div>
        </div>
      </div>

      <div className="bg-gray-50 border border-gray-200 rounded-lg p-4">
        <h3 className="font-medium text-gray-900 mb-3 flex items-center gap-2">
          <HelpCircle className="w-4 h-4" />
          What happens next?
        </h3>
        <ol className="text-sm text-gray-600 space-y-2">
          {[
            'Click "Apply to CloudPAM" to create these pools as a draft',
            'Review and approve the draft, resolving any conflicts',
            'Use the schema to guide cloud resource creation (VPCs, Subnets)',
            'CloudPAM will track actual vs. planned allocations',
          ].map((text, i) => (
            <li key={i} className="flex items-start gap-2">
              <span className="shrink-0 w-5 h-5 bg-blue-100 text-blue-700 rounded-full flex items-center justify-center text-xs font-medium">
                {i + 1}
              </span>
              <span>{text}</span>
            </li>
          ))}
        </ol>
      </div>
    </div>
  )
}
