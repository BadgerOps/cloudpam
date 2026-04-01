import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { CheckCircle2, ExternalLink, FileText, Key, Loader2, Radio, RefreshCw, Shield } from 'lucide-react'
import { checkForUpdates, getSystemInfo, getUpgradeStatus, triggerUpgrade } from '../api/client'
import type { SystemInfoResponse, UpdateCheckResponse } from '../api/types'

function formatVersion(version: string | null | undefined) {
  if (!version) return 'unknown'
  return version.startsWith('v') ? version : `v${version}`
}

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

export default function ConfigurationPage() {
  const [systemInfo, setSystemInfo] = useState<SystemInfoResponse | null>(null)
  const [updateInfo, setUpdateInfo] = useState<UpdateCheckResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [checking, setChecking] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [upgradeMessage, setUpgradeMessage] = useState<string | null>(null)
  const [upgradeRunning, setUpgradeRunning] = useState(false)
  const pollRef = useRef<number | null>(null)

  useEffect(() => {
    async function load() {
      try {
        setLoading(true)
        const [info, updates] = await Promise.all([
          getSystemInfo(),
          checkForUpdates(),
        ])
        setSystemInfo(info)
        setUpdateInfo(updates)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load configuration')
      } finally {
        setLoading(false)
      }
    }

    load()
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current)
    }
  }, [])

  async function refreshUpdates(force = false) {
    try {
      setChecking(true)
      setError(null)
      setUpdateInfo(await checkForUpdates(force))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to check for updates')
    } finally {
      setChecking(false)
    }
  }

  async function startUpgrade() {
    try {
      setUpgradeRunning(true)
      setUpgradeMessage('Starting upgrade...')
      await triggerUpgrade()
      pollRef.current = window.setInterval(async () => {
        const status = await getUpgradeStatus()
        if (status.status === 'running') {
          setUpgradeMessage(status.message || 'Upgrade in progress...')
        } else if (status.status === 'completed') {
          if (pollRef.current) window.clearInterval(pollRef.current)
          setUpgradeMessage(status.message || 'Upgrade complete. Refreshing...')
          window.setTimeout(() => window.location.reload(), 3000)
        } else if (status.status === 'failed') {
          if (pollRef.current) window.clearInterval(pollRef.current)
          setUpgradeRunning(false)
          setUpgradeMessage(status.message || 'Upgrade failed')
        }
      }, 5000)
    } catch (err) {
      setUpgradeRunning(false)
      setUpgradeMessage(err instanceof Error ? err.message : 'Failed to start upgrade')
    }
  }

  return (
    <div className="p-6 space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Configuration</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
          System settings, release management, and operational controls.
        </p>
      </div>

      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
          {error}
        </div>
      )}

      <div className="grid gap-6 xl:grid-cols-[1.25fr_0.9fr]">
        <section className="bg-white dark:bg-gray-800 rounded-xl shadow border dark:border-gray-700 p-6">
          <div className="flex items-center justify-between gap-4 mb-4">
            <div>
              <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Release Management</h2>
              <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                Check for new releases and trigger in-app upgrades when supported.
              </p>
            </div>
            <button
              onClick={() => refreshUpdates(true)}
              disabled={checking}
              className="inline-flex items-center gap-2 px-3 py-2 rounded-lg bg-blue-600 text-white text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
            >
              {checking ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
              Check Now
            </button>
          </div>

          <div className="grid gap-4 md:grid-cols-2 mb-4">
            <div className="rounded-lg border border-gray-200 dark:border-gray-700 p-4">
              <p className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">Current Version</p>
              <p className="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
                {loading ? '…' : formatVersion(systemInfo?.version)}
              </p>
            </div>
            <div className="rounded-lg border border-gray-200 dark:border-gray-700 p-4">
              <p className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">Latest Release</p>
              <p className="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
                {loading ? '…' : formatVersion(updateInfo?.latest_version)}
              </p>
            </div>
          </div>

          {updateInfo && (
            <div className="rounded-lg border border-gray-200 dark:border-gray-700 p-4 space-y-3">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <p className="text-sm font-medium text-gray-900 dark:text-white">
                    {updateInfo.update_available
                      ? `${formatVersion(updateInfo.latest_version)} is available`
                      : 'This deployment is current'}
                  </p>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Checked {formatDate(updateInfo.checked_at)}. Published {formatDate(updateInfo.published_at)}.
                  </p>
                </div>
                {updateInfo.update_available && updateInfo.upgrade_supported && (
                  <button
                    onClick={startUpgrade}
                    disabled={upgradeRunning}
                    className="px-3 py-2 rounded-lg bg-indigo-600 text-white text-sm font-medium hover:bg-indigo-700 disabled:opacity-50"
                  >
                    {upgradeRunning ? 'Upgrading…' : 'Upgrade Now'}
                  </button>
                )}
              </div>

              {upgradeMessage && (
                <div className="rounded-lg bg-indigo-50 text-indigo-700 dark:bg-indigo-950/30 dark:text-indigo-300 px-3 py-2 text-sm">
                  {upgradeMessage}
                </div>
              )}

              {updateInfo.release_notes && (
                <div className="rounded-lg bg-gray-50 dark:bg-gray-900/40 px-4 py-3">
                  <p className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2">Release Notes</p>
                  <div className="space-y-1 text-sm text-gray-700 dark:text-gray-300">
                    {updateInfo.release_notes.split('\n').filter(Boolean).slice(0, 8).map((line, idx) => (
                      <p key={idx}>{line.replace(/^#+\s*/, '').replace(/^- /, '• ')}</p>
                    ))}
                  </div>
                </div>
              )}

              <div className="flex items-center gap-4 text-sm">
                <Link to="/changelog" className="inline-flex items-center gap-1.5 text-blue-600 hover:text-blue-700 dark:text-blue-400">
                  <FileText className="w-4 h-4" />
                  Open In-App Changelog
                </Link>
                {updateInfo.release_url && (
                  <a href={updateInfo.release_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 text-blue-600 hover:text-blue-700 dark:text-blue-400">
                    <ExternalLink className="w-4 h-4" />
                    Open GitHub Release
                  </a>
                )}
              </div>
            </div>
          )}
        </section>

        <section className="bg-white dark:bg-gray-800 rounded-xl shadow border dark:border-gray-700 p-6">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white mb-4">Configuration Areas</h2>
          <div className="space-y-3">
            <Link to="/config/security" className="flex items-center justify-between rounded-lg border border-gray-200 dark:border-gray-700 px-4 py-3 hover:border-blue-400 hover:bg-blue-50/60 dark:hover:bg-blue-950/20">
              <div className="flex items-center gap-3">
                <Shield className="w-5 h-5 text-blue-600 dark:text-blue-400" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Security Policy</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Session, password, rate limit, and proxy settings</p>
                </div>
              </div>
            </Link>

            <Link to="/config/api-keys" className="flex items-center justify-between rounded-lg border border-gray-200 dark:border-gray-700 px-4 py-3 hover:border-blue-400 hover:bg-blue-50/60 dark:hover:bg-blue-950/20">
              <div className="flex items-center gap-3">
                <Key className="w-5 h-5 text-blue-600 dark:text-blue-400" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">API Keys</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Manage machine credentials and access scopes</p>
                </div>
              </div>
            </Link>

            <Link to="/config/log-destinations" className="flex items-center justify-between rounded-lg border border-gray-200 dark:border-gray-700 px-4 py-3 hover:border-blue-400 hover:bg-blue-50/60 dark:hover:bg-blue-950/20">
              <div className="flex items-center gap-3">
                <Radio className="w-5 h-5 text-blue-600 dark:text-blue-400" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Log Destinations</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">External logging and forwarding integrations</p>
                </div>
              </div>
            </Link>

            <Link to="/changelog" className="flex items-center justify-between rounded-lg border border-gray-200 dark:border-gray-700 px-4 py-3 hover:border-blue-400 hover:bg-blue-50/60 dark:hover:bg-blue-950/20">
              <div className="flex items-center gap-3">
                <FileText className="w-5 h-5 text-blue-600 dark:text-blue-400" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Release Notes</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Browse the in-app changelog and recent changes</p>
                </div>
              </div>
            </Link>
          </div>

          {systemInfo && (
            <div className="mt-6 rounded-lg bg-gray-50 dark:bg-gray-900/40 px-4 py-3">
              <div className="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-white">
                <CheckCircle2 className="w-4 h-4 text-green-500" />
                Deployment Details
              </div>
              <dl className="mt-3 space-y-2 text-sm">
                <div className="flex justify-between gap-4">
                  <dt className="text-gray-500 dark:text-gray-400">Upgrade Mode</dt>
                  <dd className="text-gray-900 dark:text-white">{systemInfo.upgrade_mode}</dd>
                </div>
                <div className="flex justify-between gap-4">
                  <dt className="text-gray-500 dark:text-gray-400">In-App Upgrade</dt>
                  <dd className="text-gray-900 dark:text-white">{systemInfo.in_app_upgrade_enabled ? 'enabled' : 'not enabled'}</dd>
                </div>
              </dl>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
