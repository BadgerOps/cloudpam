import { useEffect, useRef } from 'react'
import type { MeResponse } from '../api/types'

const POLL_INTERVAL_MS = 60_000 // 60 seconds
const LIFETIME_THRESHOLD = 0.2 // trigger refresh when 20% of lifetime remains

/**
 * useSessionRefresh - Silent session re-authentication for OIDC users.
 *
 * Polls /api/v1/auth/me every 60 seconds. When an OIDC session enters the
 * last 20% of its lifetime, creates a hidden iframe to attempt prompt=none
 * re-authentication with the IdP. On success the session cookie is refreshed
 * automatically. On failure a toast-style event is dispatched so the user
 * knows they need to log in again.
 */
export function useSessionRefresh() {
  const refreshAttemptedRef = useRef(false)
  const iframeRef = useRef<HTMLIFrameElement | null>(null)

  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null
    let cancelled = false

    function cleanup() {
      if (iframeRef.current) {
        document.body.removeChild(iframeRef.current)
        iframeRef.current = null
      }
    }

    function handleMessage(event: MessageEvent) {
      if (
        event.data &&
        typeof event.data === 'object' &&
        event.data.type === 'oidc-refresh'
      ) {
        cleanup()
        if (!event.data.success) {
          // Notify the user that silent re-auth failed.
          window.dispatchEvent(
            new CustomEvent('toast', {
              detail: {
                type: 'warning',
                message: 'Session expiring \u2014 please log in again',
              },
            })
          )
        }
        // On success the session cookie was already refreshed by the
        // callback handler. Reset so we can refresh again later.
        refreshAttemptedRef.current = false
      }
    }

    window.addEventListener('message', handleMessage)

    async function poll() {
      if (cancelled) return

      try {
        const res = await fetch('/api/v1/auth/me', {
          credentials: 'same-origin',
        })
        if (!res.ok) return

        const me: MeResponse = await res.json()

        // Only act on OIDC sessions.
        if (me.auth_type !== 'session' || me.auth_provider !== 'oidc') return
        if (!me.session_expires_at) return

        const expiresAt = new Date(me.session_expires_at).getTime()
        const now = Date.now()
        const remaining = expiresAt - now

        if (remaining <= 0) return // already expired

        // Estimate total session duration: remaining is what's left,
        // but we don't know when it was created. Use DefaultSessionDuration
        // (24h) as the denominator since that's what the backend uses.
        // A simpler approach: trigger when remaining < threshold * totalDuration.
        // Since we know total = 24h from auth.DefaultSessionDuration:
        const totalDuration = 24 * 60 * 60 * 1000 // 24 hours in ms
        const threshold = totalDuration * LIFETIME_THRESHOLD

        if (remaining > threshold) {
          // Not yet in the danger zone.
          refreshAttemptedRef.current = false
          return
        }

        // In the last 20% of lifetime — attempt silent refresh.
        if (refreshAttemptedRef.current) return
        refreshAttemptedRef.current = true

        // Get the redirect URL from the backend.
        const csrfMatch = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]+)/)
        const csrfToken = csrfMatch ? csrfMatch[1] : ''

        const refreshRes = await fetch('/api/v1/auth/oidc/refresh', {
          method: 'POST',
          credentials: 'same-origin',
          headers: {
            'Content-Type': 'application/json',
            ...(csrfToken ? { 'X-CSRF-Token': csrfToken } : {}),
          },
        })

        if (!refreshRes.ok) {
          refreshAttemptedRef.current = false
          return
        }

        const { redirect_url } = (await refreshRes.json()) as {
          redirect_url: string
        }

        // Create hidden iframe for silent re-auth.
        cleanup()
        const iframe = document.createElement('iframe')
        iframe.style.display = 'none'
        iframe.setAttribute('aria-hidden', 'true')
        iframe.src = redirect_url
        document.body.appendChild(iframe)
        iframeRef.current = iframe

        // Safety timeout: remove iframe after 30 seconds if no response.
        setTimeout(() => {
          if (iframeRef.current === iframe) {
            cleanup()
            refreshAttemptedRef.current = false
          }
        }, 30_000)
      } catch {
        // Network error — skip this cycle.
      }
    }

    // Start polling.
    poll()
    timer = setInterval(poll, POLL_INTERVAL_MS)

    return () => {
      cancelled = true
      if (timer) clearInterval(timer)
      window.removeEventListener('message', handleMessage)
      cleanup()
    }
  }, [])
}
