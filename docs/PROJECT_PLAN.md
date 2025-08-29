# CloudPAM Project Plan

This document captures recommended next steps to harden, scale, and polish CloudPAM across API, storage, UI, testing, and delivery.

## Overview
CloudPAM now supports:
- Memory + SQLite stores, pool hierarchy, accounts, and block exploration.
- API endpoints for pools, accounts, and global block listing.
- Web UI with tabs (Pools, Accounts, Analytics), modals with confirmations, and toasts.
- CI for lint and tests; unit + handler tests for core behaviors.

Below is a prioritized roadmap.

## API Hardening
- Consistent JSON error envelope: `{ "error": "...", "detail": "..." }` with appropriate HTTP codes.
- RESTful paths and verbs (canonical):
  - Pools: `GET/POST /api/v1/pools`, `GET/PATCH/DELETE /api/v1/pools/{id}`
  - Accounts: `GET/POST /api/v1/accounts`, `GET/PATCH/DELETE /api/v1/accounts/{id}`
  - Blocks: `GET /api/v1/pools/{id}/blocks` and `GET /api/v1/blocks`
- Validation:
  - Pool names (length/charset), Account keys (`aws:<12 digits>`, `gcp:<project>`), `page_size` caps.
  - Enforce unique child CIDR under same parent; reject overlaps.
- AuthN/Z (phase 2): add API token or OIDC, and roles (view/manage pools/accounts).
- Rate limiting and structured request logs.

## Storage & Schema
- Enable PRAGMA foreign_keys and define FKs for `pools.parent_id` and `pools.account_id`.
- Indexes: `pools(parent_id)`, `pools(account_id)`, `pools(cidr)`; consider unique `(parent_id, cidr)`.
- Move inline schema to versioned migrations under `migrations/`; add a small migrator runner.
- Optional soft-deletes (`deleted_at`) to support undo and audit.

## Compute & Performance
- Blocks endpoint: server-side pagination and ordering (default sensible page size).
- Guard against huge ranges (e.g., /8→/24): require pagination; never generate full set in memory.
- Add caching for account lookups in blocks if needed.

## Testing & CI
- HTTP tests: expand negative cases (invalid `page`, `page_size`, missing IDs) and JSON error payloads once added.
- SQLite tests (build-tagged): CRUD + cascade + index/constraint checks on temp DB.
- CI enhancements:
  - Add `-race` test job.
  - Add coverage reporting and (optional) threshold.
  - Gradually reintroduce linters (e.g., `revive`) with a minimal rule-set compatible with the pinned golangci-lint v2.1.x.

## UI/UX
- Modals: ESC/backdrop close, focus trapping, ARIA attributes.
- Analytics: server-side pagination + multi-sort; link rows to pools; CSV keeps filters.
- Pools: inline create/edit validation; uniform error rendering alongside toasts.
- Accounts: search + provider filters.

## Operations & Delivery
- Docker: multi-stage builds (static Go binary + minimal runtime image).
- Release workflow: build cross-platform artifacts; variants with and without `-tags sqlite`.
- Config: unify flags + env (`ADDR`, `SQLITE_DSN`, `LOG_LEVEL`), log config on startup (redact secrets).
- Observability: structured logs (zerolog/zap), request IDs, optional OpenTelemetry.

## Code Quality
- Consider renaming `internal/http` package to `internal/api` to avoid shadowing std `net/http` in tests.
- Ensure contexts and timeouts on DB operations; plumb them through handlers.
- Embed UI via `embed.FS` for single-binary deployment (dev flag to serve from disk).

## Proposed Milestones
1) API & Contracts
- JSON error envelope, RESTful path normalization, updated handler tests.
- Document API (OpenAPI) and publish basic usage in README.

2) Schema & Constraints
- PRAGMA FKs ON, add indexes + uniqueness, move to migrations.
- SQLite test suite under build tag.

3) Pagination & Performance
- Server-side pagination on global blocks; cap page sizes; ordering.
- UI wired to paginated endpoints.

4) Security & Observability
- API tokens or OIDC for auth; request logging + rate limits.
- Structured logs; optional tracing.

5) Delivery
- Dockerfile + release workflow; config surfaces and docs; embed UI.

## Immediate Next Step (Recommended)
- Implement JSON error envelope and normalize REST paths for pools/accounts (Milestone 1), then update handler tests to assert error bodies and the new paths.
- Reason: This solidifies the API contract early, reduces downstream churn, and sets a consistent pattern for subsequent work (pagination, auth, etc.).

