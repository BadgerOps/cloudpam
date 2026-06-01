const DISMISSED_VERSION_KEY = 'cloudpam_dismissed_update_version'
const UPGRADE_RELOAD_PARAM = 'cloudpam_upgrade_reload'

export async function resetFrontendAfterUpgrade() {
  try {
    localStorage.removeItem(DISMISSED_VERSION_KEY)
  } catch {
    // Ignore storage failures; the reload is the important recovery path.
  }

  if ('caches' in window) {
    try {
      const cacheNames = await caches.keys()
      await Promise.all(cacheNames.map(name => caches.delete(name)))
    } catch {
      // Cache API can be unavailable or denied in some browser contexts.
    }
  }

  const url = new URL(window.location.href)
  url.searchParams.set(UPGRADE_RELOAD_PARAM, Date.now().toString())
  window.location.replace(url.toString())
}

export function scheduleFrontendResetAfterUpgrade(delayMs = 3000): number {
  return window.setTimeout(() => {
    void resetFrontendAfterUpgrade()
  }, delayMs)
}
