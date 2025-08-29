# CloudPAM

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

## CI and Linting
- This repo includes `.golangci.yml` and a GitHub Actions workflow at `.github/workflows/lint.yml`.
- CI pins Go `1.24.x` and golangci-lint `v2.1.6` to avoid local toolchain mismatches.

Notes
- Without the `sqlite` build tag, the server uses an in‑memory store and logs a hint if `SQLITE_DSN` is set.
- Current CIDR tools and validation target IPv4. IPv6 support is planned.

## API Endpoints (early)
- `GET /healthz`: health check
- `GET /api/v1/pools`: list pools
- `POST /api/v1/pools`: create pool `{name, cidr, parent_id?}`
- `GET /api/v1/pools/{id}/blocks?new_prefix_len=24&page_size=50&page=1`: paged block listing with hosts and Used/Free flags
- `GET /api/v1/blocks?accounts=1,2&pools=10,11&page_size=50&page=1`: paginated list of assigned sub-pools across all parents (envelope: `{items,total,page,page_size}`).

## Documentation
- Project plan: `docs/PROJECT_PLAN.md`
- Changelog: `docs/CHANGELOG.md`

## Roadmap (short)
- Provider abstraction and fakes
- AWS/GCP discovery and reconciliation
- Allocator service and policies (VRFs, reservations)
- AuthN/Z and audit logging
