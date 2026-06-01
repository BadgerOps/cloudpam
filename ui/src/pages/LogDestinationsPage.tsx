import { Cable, CheckCircle2, FileJson, Radio, ShieldCheck } from 'lucide-react'

export default function LogDestinationsPage() {
  const env = [
    ['CLOUDPAM_AUDIT_SYSLOG_ADDR', 'siem.example.com:514'],
    ['CLOUDPAM_AUDIT_SYSLOG_NETWORK', 'udp'],
    ['CLOUDPAM_AUDIT_SYSLOG_APP_NAME', 'cloudpam'],
  ]
  const receivers = ['Syslog', 'Splunk', 'Security Onion', 'Datadog']

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Log Destinations</h1>
        <p className="text-gray-500 dark:text-gray-400 mt-1">
          Forward audit events to SIEM and log aggregation platforms
        </p>
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-5">
          <div className="flex items-center gap-3">
            <span className="inline-flex h-10 w-10 items-center justify-center rounded-lg bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300">
              <Radio className="h-5 w-5" />
            </span>
            <div>
              <h2 className="text-base font-semibold text-gray-900 dark:text-white">Syslog Transport</h2>
              <p className="text-sm text-gray-500 dark:text-gray-400">UDP or TCP listener</p>
            </div>
          </div>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-5">
          <div className="flex items-center gap-3">
            <span className="inline-flex h-10 w-10 items-center justify-center rounded-lg bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">
              <ShieldCheck className="h-5 w-5" />
            </span>
            <div>
              <h2 className="text-base font-semibold text-gray-900 dark:text-white">CEF Payload</h2>
              <p className="text-sm text-gray-500 dark:text-gray-400">Common security event schema</p>
            </div>
          </div>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-5">
          <div className="flex items-center gap-3">
            <span className="inline-flex h-10 w-10 items-center justify-center rounded-lg bg-violet-50 text-violet-700 dark:bg-violet-950 dark:text-violet-300">
              <Cable className="h-5 w-5" />
            </span>
            <div>
              <h2 className="text-base font-semibold text-gray-900 dark:text-white">Generic Receiver</h2>
              <p className="text-sm text-gray-500 dark:text-gray-400">No vendor API required</p>
            </div>
          </div>
        </div>
      </div>

      <section className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-6">
        <div className="flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
          <div className="max-w-2xl">
            <div className="flex items-center gap-2 text-gray-900 dark:text-white">
              <FileJson className="h-5 w-5 text-gray-500 dark:text-gray-400" />
              <h2 className="text-lg font-semibold">Common Format</h2>
            </div>
            <p className="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">
              CloudPAM emits audit events as CEF over syslog. Point the syslog address at a collector,
              forwarder, or SIEM listener and parse the CEF fields there.
            </p>
          </div>
          <div className="grid min-w-64 gap-2 sm:grid-cols-2">
            {receivers.map((name) => (
              <div key={name} className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                <CheckCircle2 className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
                <span>{name}</span>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
        <div className="border-b border-gray-200 dark:border-gray-700 px-6 py-4">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Runtime Settings</h2>
        </div>
        <div className="divide-y divide-gray-200 dark:divide-gray-700">
          {env.map(([key, value]) => (
            <div key={key} className="grid gap-2 px-6 py-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
              <code className="text-sm font-semibold text-gray-900 dark:text-gray-100">{key}</code>
              <code className="text-sm text-gray-600 dark:text-gray-300">{value}</code>
            </div>
          ))}
        </div>
      </section>

      <div className="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-100">
        Application logs still use structured JSON on stdout. Use Vector, Fluent Bit, or your platform log
        collector for vendor-specific routes such as Splunk HEC or Datadog intake.
      </div>
    </div>
  )
}
