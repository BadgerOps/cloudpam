import { useCallback, useEffect, useState } from 'react'
import { get, post } from '../api/client'
import type { UpdateCheckResponse, UpdateStatusResponse, UpgradeTriggerResponse } from '../api/types'

export function useUpdates() {
  const [summary, setSummary] = useState<UpdateCheckResponse | null>(null)
  const [status, setStatus] = useState<UpdateStatusResponse | null>(null)
  const [loadingSummary, setLoadingSummary] = useState(true)
  const [loadingStatus, setLoadingStatus] = useState(true)
  const [actionLoading, setActionLoading] = useState(false)
  const [summaryError, setSummaryError] = useState<string | null>(null)
  const [statusError, setStatusError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const refreshSummary = useCallback(async (force = false) => {
    setLoadingSummary(true)
    try {
      const data = await get<UpdateCheckResponse>(`/api/v1/updates${force ? '?force=true' : ''}`)
      setSummary(data)
      setSummaryError(null)
      return data
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load update information'
      setSummaryError(message)
      throw err
    } finally {
      setLoadingSummary(false)
    }
  }, [])

  const refreshStatus = useCallback(async () => {
    setLoadingStatus(true)
    try {
      const data = await get<UpdateStatusResponse>('/api/v1/updates/status')
      setStatus(data)
      setStatusError(null)
      return data
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load upgrade status'
      setStatusError(message)
      throw err
    } finally {
      setLoadingStatus(false)
    }
  }, [])

  const triggerUpgrade = useCallback(async () => {
    setActionLoading(true)
    try {
      const data = await post<UpgradeTriggerResponse>('/api/v1/updates/upgrade', {})
      await refreshStatus()
      await refreshSummary(true)
      setActionError(null)
      return data
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to request upgrade'
      setActionError(message)
      throw err
    } finally {
      setActionLoading(false)
    }
  }, [refreshStatus, refreshSummary])

  useEffect(() => {
    void refreshSummary()
    void refreshStatus()
  }, [refreshStatus, refreshSummary])

  return {
    summary,
    status,
    loadingSummary,
    loadingStatus,
    actionLoading,
    summaryError,
    statusError,
    actionError,
    refreshSummary,
    refreshStatus,
    triggerUpgrade,
  }
}
