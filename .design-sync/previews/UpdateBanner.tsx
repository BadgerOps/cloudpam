import { UpdateBanner } from 'cloudpam-ui'

// UpdateBanner is admin-gated and driven entirely by GET /api/v1/updates.
// Preview cards run with no CloudPAM backend, so the real fetch 404s and the
// banner correctly renders nothing. Serving the release payload the API would
// return is what makes the component's actual UI visible; the component code
// itself is untouched.
if (!(globalThis as { __dsUpdateStub?: boolean }).__dsUpdateStub) {
  ;(globalThis as { __dsUpdateStub?: boolean }).__dsUpdateStub = true
  const real = globalThis.fetch
  globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(typeof input === 'string' ? input : (input as Request).url ?? input)
    if (url.includes('/api/v1/updates')) {
      return Promise.resolve(
        new Response(
          JSON.stringify({
            current_version: 'v1.3.2',
            latest_version: '1.4.0',
            update_available: true,
            release_url: 'https://github.com/BadgerOps/cloudpam/releases/tag/v1.4.0',
            release_notes:
              '- Drift detection for discovered VPCs\n- Faster CIDR containment search\n- Fixes an OIDC group-mapping regression',
            published_at: '2026-07-30T12:00:00Z',
            checked_at: '2026-08-02T09:15:00Z',
            upgrade_supported: true,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      )
    }
    return real(input, init)
  }) as typeof fetch
}

export function ReleaseAvailable() {
  return <UpdateBanner />
}
