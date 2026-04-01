import { useEffect, useRef, useState } from 'react'
import { checkForUpdates, getUpgradeStatus, triggerUpgrade } from '../api/client'
import { useAuth } from '../hooks/useAuth'

const POLL_INTERVAL_MS = 60 * 60 * 1000
const STATUS_POLL_INTERVAL_MS = 5000
const DISMISSED_VERSION_KEY = 'cloudpam_dismissed_update_version'

function renderNotes(notes: string) {
  return notes
    .split('\n')
    .filter(line => line.trim().length > 0)
    .slice(0, 6)
    .map((line, idx) => (
      <p key={idx} className="text-sm text-indigo-100">
        {line.replace(/^#+\s*/, '').replace(/^- /, '• ')}
      </p>
    ))
}

export default function UpdateBanner() {
  const { role } = useAuth()
  const [updateAvailable, setUpdateAvailable] = useState(false)
  const [latestVersion, setLatestVersion] = useState<string | null>(null)
  const [releaseNotes, setReleaseNotes] = useState('')
  const [upgradeSupported, setUpgradeSupported] = useState(false)
  const [upgradeStep, setUpgradeStep] = useState<'confirm' | 'running' | 'completed' | 'failed' | null>(null)
  const [upgradeMessage, setUpgradeMessage] = useState('')
  const [showNotes, setShowNotes] = useState(false)
  const statusPollRef = useRef<number | null>(null)
  const reloadTimeoutRef = useRef<number | null>(null)

  useEffect(() => {
    if (role !== 'admin') return

    async function poll(force = false) {
      try {
        const result = await checkForUpdates(force)
        const dismissed = localStorage.getItem(DISMISSED_VERSION_KEY)
        if (result.update_available && dismissed !== result.latest_version) {
          setUpdateAvailable(true)
          setLatestVersion(result.latest_version)
          setReleaseNotes(result.release_notes || '')
          setUpgradeSupported(result.upgrade_supported)
        } else {
          setUpdateAvailable(false)
        }
      } catch {
        // Ignore transient failures in the banner.
      }
    }

    poll()
    const interval = window.setInterval(() => poll(), POLL_INTERVAL_MS)

    return () => {
      window.clearInterval(interval)
      if (statusPollRef.current) window.clearInterval(statusPollRef.current)
      if (reloadTimeoutRef.current) window.clearTimeout(reloadTimeoutRef.current)
    }
  }, [role])

  if (role !== 'admin') return null
  if (!updateAvailable && upgradeStep == null) return null

  async function handleUpgrade() {
    setUpgradeStep('running')
    setUpgradeMessage('Starting upgrade...')
    try {
      await triggerUpgrade()
      statusPollRef.current = window.setInterval(async () => {
        try {
          const status = await getUpgradeStatus()
          if (status.status === 'running') {
            setUpgradeMessage(status.message || 'Upgrade in progress...')
          } else if (status.status === 'completed') {
            if (statusPollRef.current) window.clearInterval(statusPollRef.current)
            setUpgradeStep('completed')
            setUpgradeMessage(status.message || 'Upgrade complete. Reloading...')
            reloadTimeoutRef.current = window.setTimeout(() => window.location.reload(), 3000)
          } else if (status.status === 'failed') {
            if (statusPollRef.current) window.clearInterval(statusPollRef.current)
            setUpgradeStep('failed')
            setUpgradeMessage(status.message || 'Upgrade failed')
          }
        } catch {
          // Keep polling.
        }
      }, STATUS_POLL_INTERVAL_MS)
    } catch (err) {
      setUpgradeStep('failed')
      setUpgradeMessage(err instanceof Error ? err.message : 'Upgrade failed')
    }
  }

  return (
    <div className="bg-gradient-to-r from-indigo-700 to-blue-700 text-white px-6 py-3 border-b border-indigo-900">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <p className="text-sm font-semibold">
            {upgradeStep === 'running' || upgradeStep === 'completed' || upgradeStep === 'failed'
              ? upgradeMessage
              : `CloudPAM v${latestVersion} is available`}
          </p>
          {showNotes && releaseNotes && upgradeStep == null && (
            <div className="mt-2 space-y-1">
              {renderNotes(releaseNotes)}
            </div>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {upgradeStep == null && releaseNotes && (
            <button
              onClick={() => setShowNotes(v => !v)}
              className="px-3 py-1.5 text-sm rounded bg-white/15 hover:bg-white/20"
            >
              {showNotes ? 'Hide Notes' : "What's New"}
            </button>
          )}
          {upgradeStep == null && upgradeSupported && (
            <button
              onClick={() => setUpgradeStep('confirm')}
              className="px-3 py-1.5 text-sm rounded bg-white text-indigo-700 hover:bg-indigo-50"
            >
              Upgrade
            </button>
          )}
          {upgradeStep === 'confirm' && (
            <>
              <button
                onClick={handleUpgrade}
                className="px-3 py-1.5 text-sm rounded bg-white text-indigo-700 hover:bg-indigo-50"
              >
                Confirm
              </button>
              <button
                onClick={() => setUpgradeStep(null)}
                className="px-3 py-1.5 text-sm rounded bg-white/15 hover:bg-white/20"
              >
                Cancel
              </button>
            </>
          )}
          {upgradeStep == null && latestVersion && (
            <button
              onClick={() => {
                localStorage.setItem(DISMISSED_VERSION_KEY, latestVersion)
                setUpdateAvailable(false)
              }}
              className="px-3 py-1.5 text-sm rounded bg-white/15 hover:bg-white/20"
            >
              Dismiss
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
