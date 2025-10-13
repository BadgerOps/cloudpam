# CloudPAM

[![Test](https://github.com/BadgerOps/cloudpam/actions/workflows/test.yml/badge.svg)](https://github.com/BadgerOps/cloudpam/actions/workflows/test.yml)
[![Lint](https://github.com/BadgerOps/cloudpam/actions/workflows/lint.yml/badge.svg)](https://github.com/BadgerOps/cloudpam/actions/workflows/lint.yml)

Lightweight, cloud‑native IP Address Management (IPAM) for AWS and GCP with an extensible provider model. Backend in Go, UI with Alpine.js, storage via in‑memory or SQLite.

## Quick Start (Dev)
- Prereqs: Go 1.24+
- Run (in‑memory): `make dev` (or `go run ./cmd/cloudpam`)
- Open: http://localhost:8080

Features in the UI
- Create top‑level pools (CIDR).
- Explore selectable IPv4 blocks within a pool with pagination (10/50/100/All) and create sub‑pools from free blocks.

## SQLite Mode (optional)
- Add driver: `go get modernc.org/sqlite@latest`
- Build with tag: `make sqlite-build` (or `go build -tags sqlite -o cloudpam ./cmd/cloudpam`)
- Run with DSN: `make sqlite-run` (or `SQLITE_DSN='file:cloudpam.db?cache=shared&_fk=1' ./cloudpam`)

## Tasks via Make or Just
- Makefile delegates to `just` if installed; otherwise runs built-in fallbacks.
- Common tasks: `make dev | build | sqlite-build | sqlite-run | fmt | lint | test | tidy`
- If you prefer Justfile directly: `just dev`, `just sqlite-run`, etc.

## Releases
- Release artifacts are multi-platform binaries (Linux/macOS/Windows on amd64/arm64) built with the `sqlite` tag using the CGO-less `modernc.org/sqlite` driver.
- No system SQLite is required. Migrations are embedded and applied automatically.
- To use SQLite, set `SQLITE_DSN` (defaults to `file:cloudpam.db?cache=shared&_fk=1` when unset in sqlite builds). Examples:
  - Linux/macOS: `SQLITE_DSN='file:cloudpam.db?cache=shared&_fk=1' ./cloudpam`
  - Windows (PowerShell): `$env:SQLITE_DSN='file:cloudpam.db?cache=shared&_fk=1'; .\cloudpam.exe`

## Upgrading
- SQLite builds run forward-only schema migrations automatically at startup; binaries embed SQL migrations.
- View schema status: `./cloudpam -migrate status` (or `cloudpam.exe -migrate status` on Windows).
- Backward downgrades after applying migrations are not supported; take a file backup of your DB before major upgrades.
- Rolling upgrade (single node): stop old binary, start new; DB is reused in place.
- Compatibility: `schema_info.min_supported_schema` guards older binaries; follow release notes if a breaking schema change occurs.
- Optional: set `APP_VERSION` to stamp migrations (e.g., `APP_VERSION=v1.2.3 ./cloudpam`).

## CI and Linting
- This repo includes `.golangci.yml` and a GitHub Actions workflow at `.github/workflows/lint.yml`.
- CI pins Go `1.24.x` and golangci-lint `v2.1.6` to avoid local toolchain mismatches.

Notes
- Without the `sqlite` build tag, the server uses an in‑memory store and logs a hint if `SQLITE_DSN` is set.
- Current CIDR tools and validation target IPv4. IPv6 support is planned.

## API Reference
The REST contract is captured in `docs/openapi.yaml` (OpenAPI 3.1). Preview it locally with your preferred viewer or import it into Postman/Insomnia.

Common requests:

```bash
# Liveness probe
curl http://localhost:8080/healthz

# List pools
curl http://localhost:8080/api/v1/pools

# Create a /24 child pool under parent 1
curl -X POST http://localhost:8080/api/v1/pools \
  -H "Content-Type: application/json" \
  -d '{"name":"Prod-1","cidr":"10.0.1.0/24","parent_id":1}'

# Enumerate candidate /26 blocks for pool 1
curl "http://localhost:8080/api/v1/pools/1/blocks?new_prefix_len=26&page_size=50&page=1"

# List assigned blocks filtered by account IDs
curl "http://localhost:8080/api/v1/blocks?accounts=12,14&page_size=50&page=1"
```

Errors use the JSON envelope `{"error":"message","detail":"optional context"}`. The live server also serves the spec at `http://localhost:8080/openapi.yaml` for tooling consumption.

### OpenAPI tooling
- Validate the spec (Ruby/Psych sanity check): `just openapi-validate`
- Generate HTML docs (writes to `docs/openapi-html/`): `just openapi-html`
- Example client generation: `openapi-generator-cli generate -i docs/openapi.yaml -g typescript-fetch -o clients/ts`

## Documentation
- Project plan: `docs/PROJECT_PLAN.md`
- Changelog: `docs/CHANGELOG.md`
- OpenAPI spec: `docs/openapi.yaml`

## Screenshots
The `photos/` directory contains app screenshots (tracked via Git LFS). To enable LFS locally: `git lfs install`.

- Pools overview: `photos/pools.png`
  ![Pools](photos/pools.png)
- Pool blocks with filters and tooltips: `photos/blocks.png`
  ![Blocks](photos/blocks.png)
- IP Space visualization bar: `photos/visualization.png`
  ![Visualization](photos/visualization.png)
- Bulk actions (Pools): `photos/bulk-actions-pools.png`
  ![Bulk Pools](photos/bulk-actions-pools.png)
- Accounts list and edit: `photos/accounts.png`
  ![Accounts](photos/accounts.png)
- Analytics filters and charts: `photos/analytics.png`
  ![Analytics](photos/analytics.png)

Suggested capture set (drop files into `photos/` with these names):
- `pools.png`, `blocks.png`, `visualization.png`, `bulk-actions-pools.png`, `accounts.png`, `analytics.png`

Automated capture (Playwright)
- Prereqs: Node 18+, `git lfs install`, app running at `http://localhost:8080` (or set `APP_URL`).
- From `code/cloudpam/`:
  - `npm install`
  - `npx playwright install chromium`
  - `APP_URL=http://localhost:8080 npm run screenshots`
- Outputs to `photos/` with the filenames above.


## Roadmap (short)
- Provider abstraction and fakes
- AWS/GCP discovery and reconciliation
- Allocator service and policies (VRFs, reservations)
- AuthN/Z and audit logging
