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
#    RATE_LIMIT_RPS=0 keeps the default 100 rps limiter from throttling the capture run.
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

`just screenshots` (default port 8195) and `just gifs` (default port 8196) wrap the
build, throwaway server, and capture commands. GIF details are in [Demo GIFs](#demo-gifs).

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
| `pools.png` | `/pools`, after `h1` "Address Pools" |
| `blocks.png` | `/blocks`, after `h1` "Allocated Blocks" |
| `accounts.png` | `/accounts`, after `h1` "Cloud Accounts" |
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

`VIEWPORTS` in `scripts/lib/capture_helpers.js` is the single place viewports are declared. Adding one
is a one-line change: append an entry and every capture re-runs against it, with `suffix`
appended to each filename.

**Only desktop (1280 wide) is captured today.** This is deliberate. The app has no
responsive layout yet — `ui/src/components/Layout.tsx` and `Header.tsx` carry no breakpoint
classes and there is no mobile navigation — so a narrow capture would document a broken
layout rather than a reviewable one. Responsive work is tracked in issue #83; uncomment the
mobile entry once that lands.

## Legacy group

The `legacy` group is the SPA still set. It uses `page.goto` for the routes in
`ui/src/navigation.ts` and does not click sidebar labels (`button:has-text("Pools")`
and the old Alpine selectors are gone):

| File | Route | Ready selector |
|------|-------|----------------|
| `pools.png` | `/pools` | `h1:has-text("Address Pools")` |
| `blocks.png` | `/blocks` | `h1:has-text("Allocated Blocks")` |
| `accounts.png` | `/accounts` | `h1:has-text("Cloud Accounts")` |

The sidebar also renders an `h1` ("CloudPAM"), so blocks waits on the page heading
rather than any `h1`. The release-update banner is dismissed once it appears; that
check is asynchronous, so the first dismiss waits up to 8s.

`visualization.png`, `bulk-actions-pools.png`, and `analytics.png` are not part of
the curated blog set. There is no Analytics route in the SPA, and the Alpine
bulk-menu / IP-space card captures were dropped. Those three files stay in `photos/`
as historical Git LFS objects and are not regenerated. Add a capture back only if
the page returns.

`SCREENSHOT_ONLY=legacy` captures the three stills.
`SCREENSHOT_ONLY=discovery` (or `npm run screenshots:discovery`) captures the
discovery set.

## Demo GIFs

`scripts/capture_gifs.js` records three short UI flows and encodes them with **host**
`ffmpeg` (palettegen / paletteuse). The Nix dev shell does not provide ffmpeg. Install
it on the OS and leave it off the flake. `ffprobe` comes with that same ffmpeg and is
how you check duration.

```bash
nix develop --command just gifs
```

Inside an existing dev shell, `just gifs` is enough. The recipe checks that `ffmpeg` is
on `PATH`, builds the UI, starts a throwaway in-memory server on port 8196
(`DEV_MODE=1`, `RATE_LIMIT_RPS=0`), and runs `npm run gifs`. It uses a different port
from `just screenshots` so the two can be reasoned about separately. Pass another port
with `just gifs 8200`.

Credentials match the still capture: `screenshot-admin` / `ScreenshotDev12345!`.

| File | What it shows |
|------|----------------|
| `photos/gifs/setup-login.gif` | First-boot setup (`Create Admin Account`), sign-in, then Address Pools |
| `photos/gifs/create-pool.gif` | `New Pool` → name `Demo App Tier`, CIDR `10.20.10.0/24`, parent Prod East VPC (Corp Supernet if that option is missing), then the new row |
| `photos/gifs/discovery-import.gif` | Seeded `prod-platform` resources, `Preview Import`, then `Apply import` |

Each clip is aimed at about 8–12 seconds. They are demo-seeded on the throwaway server.
They are not a recording of a live AWS account.

The pools table lists root pools until the search box has text, so the create-pool clip
searches for `Demo App Tier` to reveal the child of Prod East VPC. Discovery applies the
import for real (`Apply import` on the preview modal) and holds on the import summary.
`KEEP_GIF_RAW=1` keeps the intermediate WebM files in `photos/gifs/raw/`.

## Git LFS

`.gitattributes` declares `photos/**/*.png` and `photos/**/*.gif` as LFS-tracked, and
every PNG under `photos/` is one — `git lfs ls-files` lists all of them. The demo GIFs
are committed the same way.

Six of them (`accounts`, `analytics`, `blocks`, `bulk-actions-pools`, `pools`,
`visualization`) predate that and had been committed as plain git blobs, which left
`photos/` half-LFS and made those six show as modified in every clean checkout: git ran
the LFS clean filter over the working file and compared the resulting pointer against a
raw blob. They were re-added through the filter, so the pointers and the pixels are
unchanged — the same bytes, stored the way `.gitattributes` always said they were.

History was deliberately **not** rewritten. `git lfs migrate import` would have rewritten
every commit that touched `photos/`, which is not worth it for a fixed working tree; the
original blobs stay reachable in old commits, so the repository does not shrink.
