import { Radio } from 'lucide-react'

export default function LogDestinationsPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Log Destinations</h1>
        <p className="text-gray-500 dark:text-gray-400 mt-1">
          Configure where to push audit and system logs
        </p>
      </div>

      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-8 text-center">
        <Radio className="w-12 h-12 text-gray-400 dark:text-gray-500 mx-auto mb-4" />
        <h2 className="text-lg font-semibold text-gray-700 dark:text-gray-300 mb-2">Coming Soon</h2>
        <p className="text-gray-500 dark:text-gray-400 max-w-md mx-auto">
          Log destination configuration will allow you to forward audit events and system logs to
          external services including syslog, webhooks, S3 buckets, and SIEM integrations.
        </p>
      </div>
    </div>
  )
}
