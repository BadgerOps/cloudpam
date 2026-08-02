# Regenerating Screenshots

`scripts/capture_screenshots.js` drives a **live** CloudPAM instance with Playwright and
writes PNGs to `photos/`. It is not a test — it needs a running server, an admin login and
realistic data, because empty-state screenshots are worthless for review.

> **Point this at a throwaway dev server only.** The script logs in, runs first-boot setup
> and writes accounts, pools and discovered resources into whatever instance `APP_URL`
> names.

## Quick start

All commands run from the repo root inside the Nix dev shell
(`nix develop`, or prefix each command with `nix develop --command bash -c '...'`).

```bash
# 1. Build the UI (embedded into the binary via go:embed)
cd ui && npm install && npm run build && cd ..

# 2. Build the server (in-memory store is the easiest target)
go build -o /tmp/cloudpam-shots ./cmd/cloudpam

# 3. Run it on a scratch port. DEV_MODE=1 is required to opt into the in-memory store;
#    RATE_LIMIT_RPS=0 keeps the default 10 rps limiter from throttling the capture run.
DEV_MODE=1 RATE_LIMIT_RPS=0 CLOUDPAM_LOG_LEVEL=warn /tmp/cloudpam-shots -addr :8195 &

# 4. Install the browser (first run only)
npm install && npx playwright install chromium

# 5. Capture
APP_URL=http://localhost:8195 \
  SCREENSHOT_USERNAME=screenshot-admin \
  SCREENSHOT_PASSWORD='ScreenshotDev12345!' \
  npm run screenshots
```

`npm run screenshots:discovery` captures only the discovery set, which is what you want
when iterating on the Discovery page.

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `APP_URL` | `http://localhost:8080` | Base URL of the running instance |
| `SCREENSHOT_USERNAME` | `CLOUDPAM_ADMIN_USERNAME` | Admin username used for setup + login |
| `SCREENSHOT_PASSWORD` | `CLOUDPAM_ADMIN_PASSWORD` | Admin password (min 12 chars, NIST 800-63B policy) |
| `SCREENSHOT_SEED` | seed enabled | Set to `0` to skip seeding and capture existing data |
| `SCREENSHOT_ONLY` | all groups | `legacy` and/or `discovery`, comma-separated |

## Auth

The app defaults to auth-always, so the script authenticates before it navigates anywhere.

It deliberately does **not** rely on `CLOUDPAM_ADMIN_USERNAME` / `CLOUDPAM_ADMIN_PASSWORD`
for the server. `cmd/cloudpam/main.go` sets the `needs_setup` flag *before* bootstrapping
the env-var admin and never clears it, so a server started that way still reports
`needs_setup: true` and the SPA's `ProtectedRoute` bounces every page to `/setup`. Instead:

1. `GET /healthz` — if `needs_setup` is true,
2. `POST /api/v1/auth/setup` with the screenshot credentials (this is what actually clears
   the flag), then
3. `POST /api/v1/auth/login`.

The session cookie lands in the Playwright browser context, so page navigations and the
seeding requests below share one authenticated jar. State-changing seed requests read the
`csrf_token` cookie and echo it in the `X-CSRF-Token` header to satisfy the double-submit
CSRF middleware.

## Seeding

Seeding is idempotent and runs once per capture run:

1. **Managed pools** via `POST /api/v1/pools` — `Corp Supernet` (10.0.0.0/8) with children
   `Prod East VPC` (10.20.0.0/16) and `Edge Transit` (10.40.0.0/17). Pools already present
   by name are left alone.
2. **Discovered cloud resources** via `POST /api/v1/discovery/ingest/org`, which also
   auto-creates the `prod-platform` and `sandbox` cloud accounts. Org ingest upserts by
   `(account, resource_id)`, so re-running is safe.

The resource set is designed so every conflict class in the merged network view actually
fires:

| Seeded resource | Produces |
|-----------------|----------|
| `vpc-0a1prod` 10.20.0.0/16, `vpc-0c1edge` 10.40.64.0/17 | `unlinked_exact_pool` — exact match against a managed pool |
| `subnet-0b9` parented to `vpc-deleted-999` | `missing_parent` |
| `subnet-0b8` 172.16.5.0/24 inside `vpc-0b1prod` 10.21.0.0/16 | `invalid_nesting` |
| `vpc-0d1shared` / `vpc-0d2shared`, both 192.168.10.0/24 | `duplicate_cidr` under the default account policy |
| `vpc-0b1prod` + subnets | clean importable rows for the import preview |

One detail worth knowing if you edit the payload: `last_seen_at` is set slightly in the
**future**. `SyncService.ProcessResources` marks anything last seen before the ingest
timestamp as stale, and stale resources are neither selectable nor importable, so a
"now" timestamp from the client would race the server clock and produce an all-stale table.

## Captures

| File | View |
|------|------|
| `discovery-import-selection.png` | Resources tab with four rows checkbox-selected, selection toolbar and bulk link controls active |
| `discovery-import-preview.png` | Import preview modal — per-resource action, status (importable / blocked) and evidence |
| `discovery-network-hierarchy.png` | Merged Network → Hierarchy: managed pools with discovered children and conflict badges |
| `discovery-network-flat.png` | Merged Network → Flat: all merged rows with state, issues and relationships |
| `discovery-network-conflicts.png` | Merged Network → Conflicts: conflict list plus the populated review panel (evidence, review decisions, actions) |

Two implementation notes:

- The Discovery page renders inside a `flex-1 overflow-auto` container, so the document
  never scrolls and Playwright's `fullPage` option returns exactly one viewport. The script
  temporarily grows the viewport (`withViewportHeight`) so each view fits in one capture.
- The release-update banner is dismissed first. It depends on an external version check and
  would otherwise shift layout nondeterministically between runs.

## Viewports

`VIEWPORTS` at the top of the script is the single place viewports are declared. Adding one
is a one-line change: append an entry and every capture re-runs against it, with `suffix`
appended to each filename.

**Only desktop (1280 wide) is captured today.** This is deliberate. The app has no
responsive layout yet — `ui/src/components/Layout.tsx` and `Header.tsx` carry no breakpoint
classes and there is no mobile navigation — so a narrow capture would document a broken
layout rather than a reviewable one. Responsive work is tracked in issue #83; uncomment the
mobile entry once that lands.

## Known rot: the legacy captures

`pools.png`, `blocks.png`, `accounts.png`, `analytics.png`, `visualization.png` and
`bulk-actions-pools.png` predate the Sprint 9 React SPA. Their selectors (`Top-level
Pools`, `button:has-text("Pools")`, the `⋮` bulk menu) belong to the old Alpine.js UI and
no longer match anything, so the `legacy` group silently falls back to capturing the
dashboard for each name and skips two files entirely. The committed PNGs are still the
older, correct ones — do not regenerate the legacy group until those captures are rewritten
against the current SPA routes.

Use `SCREENSHOT_ONLY=discovery` (or `npm run screenshots:discovery`) to avoid overwriting
them in the meantime.

## Git LFS

`.gitattributes` declares `photos/**/*.png` as LFS-tracked, but the PNGs currently in
history were committed as plain git blobs and never migrated (`git lfs ls-files` returns
nothing). Two consequences:

- Every file under `photos/` shows as modified in a clean checkout, because git runs the
  LFS clean filter over the working file and compares a pointer against a raw blob.
- Newly added PNGs *are* stored as LFS pointers, so `photos/` ends up half-LFS.

Migrating the existing files (`git lfs migrate import --include='photos/**/*.png'`) or
dropping the LFS rules would fix this; it is out of scope for screenshot changes.
