import { RefreshCw } from 'lucide-react'

export default function DiscoveryPage() {
  return (
    <div className="flex-1 flex items-center justify-center p-6">
      <div className="text-center">
        <RefreshCw className="w-16 h-16 mx-auto mb-4 text-gray-300 dark:text-gray-600" />
        <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100 mb-2">Cloud Discovery</h2>
        <p className="text-gray-500 dark:text-gray-400 mb-4">Automatically discover and sync cloud resources</p>
        <p className="text-sm text-gray-400 dark:text-gray-500">Coming in a future release</p>
      </div>
    </div>
  )
}
