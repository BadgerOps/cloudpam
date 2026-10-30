---
name: verify-cloudpam
description: "Drive a real CloudPAM instance (Go server + embedded React SPA + REST API) the way a user does and capture proof: launch a disposable SQLite-backed server, sign in through the UI, exercise a mapped feature with Playwright or the API, save screenshots/ARIA/API evidence, tear down. Use for 'verify this change works', 'prove the UI does X', 'screenshot the pools page', or before claiming any user-facing change is done."
---

# Verify CloudPAM

CloudPAM's primary surface is the React SPA served by the Go binary at `/`, backed by the REST API under `/api/v1`. Secondary surfaces not covered here: the `cloudpam-agent` discovery binary, the Terraform provider, and the `/openapi` reference. Everything below is driven through one helper, `cloudpam-verify`, and a feature map in `features/`. Read `features/README.md` first, then the feature file you are proving.

```
.claude/skills/verify-cloudpam/
├── SKILL.md                  this file
├── bin/cloudpam-verify       launch | doctor | api | browser | env | cleanup
├── bin/journey.cjs           Playwright runner used by `browser` (login, api, shot, aria helpers)
├── journeys/                 proven journeys, one per feature file (create-pool.cjs is the fullest template)
└── features/                 the verification map (README.md index + one file per feature)
```

All commands run from the repo root. The helper wraps itself in `nix develop` (the repo's required toolchain) so you do not need to enter the shell first. Use the path `.claude/skills/verify-cloudpam/bin/cloudpam-verify`, or put `bin/` on `PATH`.

## Launch

```
.claude/skills/verify-cloudpam/bin/cloudpam-verify launch
```

What it does, in order (about 1-2 minutes cold, under 30 seconds when `ui/node_modules` and the Go cache are warm):

1. Builds the SPA (`cd ui && npm ci && npm run build` → `web/dist/`, embedded via `go:embed`). The build empties `web/dist/` and rewrites the tracked `web/dist/index.html`, so launch copies the whole directory to `state/web-dist.orig` first and puts it back once the server binary exists (step 3), logging `restored the checkout's web/dist`; the binary serves the freshly built copy, and your checkout keeps exactly what was there, uncommitted edits and matching assets included (a checkout with no `web/dist` gets none back). Side-by-side launches take turns at the build (`tmp/cloudpam-verify/build.lock`), so one cannot save or restore another's output. Skip with `CLOUDPAM_VERIFY_SKIP_UI=1` only when you know `web/dist/assets` is current.
2. Installs Playwright + Chromium from the repo-root `package.json` (idempotent).
3. Builds the server with `-tags sqlite` and `CGO_ENABLED=0` (the release configuration) into the run's state dir.
4. Picks a free port in 18080-18179 and starts the binary with a fresh SQLite file (`SQLITE_DSN=file:<state>/cloudpam.db`), `RATE_LIMIT_RPS=0`, JSON logs, and `APP_VERSION=cloudpam-verify-<git short rev>-<run id>` (the server trims a leading `v`, so the stamp must not start with one). Migrations `0001`..`0021` apply on startup. Everything else, including `SQLITE_MAX_OPEN_CONNS=1`, is the product default.
5. Waits for `GET /readyz` (database health), then runs first-boot setup (`POST /api/v1/auth/setup`), logs in with curl (cookie jar in state), and seeds `Corp Supernet` 10.0.0.0/8, child `Prod East VPC` 10.20.0.0/16, and account `prod-platform`.
6. Writes `<run>/state/env` (one `NAME=value` line per variable, quoted with `printf %q`, so a credential with quotes or spaces sources back intact) and prints the run id. Ready means `GET /api/v1/auth/me` returns 200 for the seeded admin, not merely that the port answers.

Runs live in `tmp/cloudpam-verify/<RUN_ID>/` (gitignored). `state/` holds the binary, database, `server.log`, pid, cookies, env. `evidence/` holds proof. `tmp/cloudpam-verify/current` names the latest run; set `CLOUDPAM_VERIFY_RUN_ID` to pin one when running two instances side by side (each run gets its own port and database, so parallel runs are safe). A run id names a directory that `cleanup` deletes, so it must match `^[A-Za-z0-9._-]+$`; every subcommand refuses any other value, including `.` and `..`.

Credentials: `verify-admin` / `VerifyDev12345!` (`CLOUDPAM_VERIFY_USERNAME` / `CLOUDPAM_VERIFY_PASSWORD` override before launch; the password policy needs 12+ characters).

## Doctor

```
.claude/skills/verify-cloudpam/bin/cloudpam-verify doctor
```

Read-only, except that it logs the curl session in again if the server evicted it. Run it first whenever anything looks off. It checks: the recorded pid is alive and its command line is this run's binary; `/healthz` reports `status ok`, `needs_setup false`, and `version` equal to the run's `APP_VERSION` (that is how you know the port is ours and not another CloudPAM); `/readyz` is 200; the curl session still resolves to `verify-admin` (logging in again once if the server evicted it); `/api/v1/pools/hierarchy` answers within 5 seconds with a child pool present (the Address Pools page's data, and the request that hung before #261); the seed pools exist; and the binary was built from the current `HEAD`. Exit code 0 means drive it; anything else means `cleanup` and `launch` again.

## Drive

Two recipes, both authenticated as the seeded admin.

**API** (curl with the run's cookie jar; unsafe methods get the `X-CSRF-Token` double-submit header automatically):

```
.claude/skills/verify-cloudpam/bin/cloudpam-verify api GET /api/v1/pools
.claude/skills/verify-cloudpam/bin/cloudpam-verify api --save pool-create POST /api/v1/pools '{"name":"verify-x","cidr":"10.31.0.0/16","parent_id":1}'
```

Prints the status line then the body; non-2xx exits 1, which is the expected exit for a refusal you are proving. `--save NAME` also writes `evidence/api/NAME.txt`; NAME must be one path component (`^[A-Za-z0-9._-]+$`, not `.` or `..`) or the command refuses. The server keeps 10 sessions per user and evicts the oldest, so after ten browser logins in one run the curl session is gone; the helper then logs `curl session evicted (401); logging in again` and retries once.

**Browser** (headless Chromium through Playwright):

```
.claude/skills/verify-cloudpam/bin/cloudpam-verify browser .claude/skills/verify-cloudpam/journeys/create-pool.cjs
```

A journey is a CommonJS module exporting `async ({ page, context, baseURL, username, password, login, api, shot, aria, note, evidenceDir }) => {}`. `login()` drives the real `/login` form and waits for the `Main navigation` landmark; if the server answers `too many login attempts` (5 logins per minute per IP), it waits 60 seconds and retries once. Call it once per browser context. `api()` sends requests through the browser's session with CSRF handled. `shot(name)` and `aria(name)` write `evidence/<label>/name.png` and `name.aria.txt`, where `<label>` is the journey's basename or the `--label NAME` argument; it must be one path component (`^[A-Za-z0-9._-]+$`, not `.` or `..`) or the command refuses, so a label can never name a directory outside `evidence/`. `note()` appends to `evidence/<label>/journey.log`. `confirm()` dialogs are auto-accepted and logged. A thrown error fails the run (exit 1) and leaves `failure.png`.

Stable handles in this app: sidebar links by role `link` and name (`Address Pools`, `Cloud Accounts`, `Discovery`, ...); page `heading`s with the same names; the Pools create form fields by `getByLabel` (`Name`, `CIDR`, `Type`, `Status`, `Account`, `Parent Pool`, `Description`); buttons by name (`New Pool`, `Create Pool`, `Sign In`); toasts by text (`Pool created`, `Account created`). The login and account forms have labels without `htmlFor`, so use the placeholders named in their feature files. Prefer these over CSS classes, which are Tailwind utilities and change freely.

## Evidence

Proof goes under `tmp/cloudpam-verify/<RUN_ID>/evidence/` and survives cleanup. Standards:

- Exercise the real user path: the login form, the sidebar, the create form. Do not call internal setters, seed helpers, or test-only endpoints as a substitute for the UI step you are proving. The API is a legitimate second entry point only when the feature file lists it as one.
- Capture the action and the result: a screenshot of the filled form before submit, the toast or row after, and an ARIA snapshot of the end state.
- Verify side effects alongside what is visible: re-read through `GET /api/v1/...` and, for audited actions, `GET /api/v1/audit?action=...&resource_type=...`. The database file is `state/cloudpam.db` if you need to inspect a row directly (copy it before cleanup; `sqlite3` is not in the dev shell).
- Mocks only where production already isolates the boundary: cloud discovery is fed through `POST /api/v1/discovery/ingest/org` (the agent's real ingest path) rather than a live AWS account; the LLM planner needs `CLOUDPAM_LLM_ENDPOINT`, which launch does not set, so AI Planner journeys must provide their own endpoint or report the feature unreachable.
- A journey that cannot reach its precondition reports failure with the command and the missing state; it does not weaken the assertion.
- Record what you proved in the feature file's terms (feature ID, entry point) in the journey log or your report.

## Cleanup

```
.claude/skills/verify-cloudpam/bin/cloudpam-verify cleanup
```

Sends SIGTERM only to the pid recorded in `state/server.pid`, and only if that process's command line is this run's binary (never kills by name). Waits for the server's graceful shutdown, copies `server.log` into `evidence/`, deletes `state/` (binary, database, cookies), and leaves `evidence/` in place. Run it after every attempt, including failed ones, so no instance or port is stranded. `tmp/cloudpam-verify/` as a whole is safe to delete by hand once evidence has been collected.

## Helpers

- `bin/cloudpam-verify` (bash, executable): subcommands above; `cloudpam-verify` with no arguments prints usage.
- `bin/journey.cjs` (node, run via `cloudpam-verify browser`): the Playwright runner. Do not call it directly; `browser` exports the `CLOUDPAM_VERIFY_*` variables it reads (base URL, evidence dir, version, credentials, label) from the run's env file.
- `journeys/<feature>.cjs`: the proven journey for `features/<feature>.md` (`login`, `create-pool`, `create-account`, `search`, `navigation-permissions`). New journeys go next to them, named after their feature file.

## Maintenance

The map in `features/` is the source of truth for what "works" means; keep it honest as routes, labels, and endpoints change. Use `/pstack:maintain-verification-skill` to re-walk the map against the current app and record drift.
