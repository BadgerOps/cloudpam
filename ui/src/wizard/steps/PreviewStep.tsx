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
          <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100 mb-2">Preview Your Schema</h2>
          <p className="text-gray-600 dark:text-gray-300">Review the generated IP address plan before applying.</p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => onExport('terraform')}
            className="flex items-center gap-2 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700/50 text-sm dark:text-gray-100"
          >
            <Download className="w-4 h-4" />
            Export Terraform
          </button>
          <button
            onClick={() => onExport('csv')}
            className="flex items-center gap-2 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700/50 text-sm dark:text-gray-100"
          >
            <Download className="w-4 h-4" />
            Export CSV
          </button>
        </div>
      </div>

      {conflictsLoading && (
        <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-700 rounded-lg p-4 text-sm text-blue-700 dark:text-blue-300">
          Checking for conflicts with existing pools...
        </div>
      )}

      {conflictsError && (
        <div className="bg-yellow-50 dark:bg-yellow-900/30 border border-yellow-200 dark:border-yellow-800 rounded-lg p-4 text-sm text-yellow-700 dark:text-yellow-300">
          Could not check for conflicts: {conflictsError}. You can still apply the schema.
        </div>
      )}

      {conflicts.length > 0 && (
        <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg p-4">
          <div className="flex items-start gap-2">
            <AlertTriangle className="w-5 h-5 text-red-500 dark:text-red-400 shrink-0 mt-0.5" />
            <div>
              <h3 className="font-medium text-red-800 dark:text-red-300">Conflicts Detected</h3>
              <p className="text-sm text-red-600 dark:text-red-400 mt-1">
                {conflicts.length} allocation(s) overlap with existing pools in CloudPAM.
              </p>
              <ul className="mt-2 space-y-1">
                {conflicts.map((c, i) => (
                  <li key={i} className="text-sm text-red-700 dark:text-red-300">
                    &bull; <code className="font-mono">{c.planned_cidr}</code> overlaps with existing pool &ldquo;{c.existing_pool_name}&rdquo; ({c.existing_cidr})
                  </li>
                ))}
              </ul>
            </div>
          </div>
        </div>
      )}

      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden">
        <div className="bg-gray-50 dark:bg-gray-800 px-4 py-3 border-b dark:border-gray-700 flex items-center justify-between">
          <h3 className="font-medium text-gray-900 dark:text-gray-100">Address Space Hierarchy</h3>
          <div className="flex items-center gap-4 text-xs text-gray-500 dark:text-gray-400">
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
        <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-700 rounded-lg p-4 text-center">
          <div className="text-2xl font-bold text-blue-700 dark:text-blue-300">{formatHostCount(totalAddresses)}</div>
          <div className="text-sm text-blue-600 dark:text-blue-400">Total Addresses</div>
        </div>
        <div className="bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-700 rounded-lg p-4 text-center">
          <div className="text-2xl font-bold text-green-700 dark:text-green-300">{topLevel}</div>
          <div className="text-sm text-green-600 dark:text-green-400">Top-Level Allocations</div>
        </div>
        <div className="bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-700 rounded-lg p-4 text-center">
          <div className="text-2xl font-bold text-amber-700 dark:text-amber-300">{totalPools}</div>
          <div className="text-sm text-amber-600 dark:text-amber-400">Total Pools</div>
        </div>
      </div>

      <div className="bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
        <h3 className="font-medium text-gray-900 dark:text-gray-100 mb-3 flex items-center gap-2">
          <HelpCircle className="w-4 h-4" />
          What happens next?
        </h3>
        <ol className="text-sm text-gray-600 dark:text-gray-300 space-y-2">
          {[
            'Click "Apply to CloudPAM" to create these pools as a draft',
            'Review and approve the draft, resolving any conflicts',
            'Use the schema to guide cloud resource creation (VPCs, Subnets)',
            'CloudPAM will track actual vs. planned allocations',
          ].map((text, i) => (
            <li key={i} className="flex items-start gap-2">
              <span className="shrink-0 w-5 h-5 bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 rounded-full flex items-center justify-center text-xs font-medium">
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
