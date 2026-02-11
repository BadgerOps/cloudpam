# Changelog

All notable changes to CloudPAM will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added - Sprint 7: Production Readiness & API Documentation

#### OpenAPI Spec v0.3.0
- Updated OpenAPI spec from v0.1.0 to v0.3.0 with all implemented endpoints
- Added 12+ missing endpoint definitions: `/readyz`, `/metrics`, `/api/v1/test-sentry`,
  auth key management, audit log queries, CSV import, pool hierarchy, pool stats
- Added `BearerAuth` security scheme for API key authentication
- Updated Pool schema with Sprint 5 fields: `type`, `status`, `source`, `description`, `tags`, `updated_at`
- Added new schemas: `ReadinessResponse`, `PoolStats`, `PoolWithStats`, `ImportResult`,
  `CreateAPIKey`, `APIKeyCreated`, `APIKeyInfo`, `AuditEvent`, `AuditListResponse`
- Added `include_stats` query parameter to pool list endpoint
- Added tag definitions for all endpoint groups (System, Pools, Accounts, Blocks, Export, Import, Auth, Audit)

#### SQLite-backed API Key Store
- Migration `0005_api_keys.sql`: persistent `api_keys` table with prefix index
- `internal/auth/sqlite.go`: full `KeyStore` interface implementation backed by SQLite
  (Create, GetByPrefix, GetByID, List, Revoke, UpdateLastUsed, Delete)
- `selectKeyStore()` added to both build-tag files for automatic backend selection
- AuthServer and auth routes wired into `main.go` startup for both build modes
- `CLOUDPAM_AUTH_ENABLED=true` env var to toggle RBAC enforcement
- Comprehensive SQLite KeyStore test suite (CRUD, not-found, duplicate prefix, expiration, multiple keys)

### Improved - Sprint 6: UI Accessibility

- Modal accessibility and focus trapping (#22):
  - Added `@alpinejs/focus` plugin for `x-trap` focus trapping in modals
  - Global search modal: `role="dialog"`, `aria-modal`, `aria-label`, `x-trap.noscroll`, search input `aria-label`
  - Data I/O modal: `role="dialog"`, `aria-modal`, `aria-label`, `x-trap.noscroll`, close/fullscreen `aria-label`
  - Pool detail slide panel: ESC-to-close, `role="complementary"`, `aria-label`, close button `aria-label`
  - Sidebar navigation: `aria-label="Main navigation"`, `:aria-current="page"` on active tab buttons

### Added - Sprint 6: Docker & Infrastructure

- Multi-stage Docker build (#37):
  - `Dockerfile` with `golang:1.24-alpine` build stage and `alpine:3.21` runtime stage
  - Builds static binary with `-tags sqlite -trimpath -ldflags "-s -w"`
  - Non-root `cloudpam` user in runtime image
  - `.dockerignore` excludes `.git`, `node_modules`, `photos`, coverage files
  - Added `just docker-build` and `just docker-run` recipes

### Fixed - Sprint 6: Code Quality & API Hardening

- Raise `internal/http` test coverage from 60% to 80.6% (#67, #32, #33):
  - Added `import_test.go` — 20+ tests covering CSV import handlers, `writeStoreErr`, `NewServerWithSlog`, `handleTestSentry`, and force-delete paths
  - Added `protected_handlers_test.go` — 30+ tests exercising RBAC-protected pool, account, and auth handlers with admin and viewer API keys
  - Tests cover `protectedPoolsHandler`, `protectedPoolsSubroutesHandler`, `protectedAccountsHandler`, `protectedAccountsSubroutesHandler`, `protectedAPIKeysHandler`, `protectedAPIKeyByIDHandler`, `RegisterProtectedRoutes`, `RegisterProtectedAuthRoutes`, `AuthServer.handleAuditList`, and `parseInt`
  - Per-package coverage: http 80.6%, storage 91.8%, auth 96.6%, observability 96.8%, validation 100%, domain 100%
- Standardize error handling with typed sentinel errors (#69):
  - Added `internal/storage/errors.go` with `ErrNotFound`, `ErrConflict`, `ErrValidation` sentinels
  - Added `WrapIfConflict()` helper to detect SQLite UNIQUE constraint violations
  - Replaced fragile `strings.Contains(err.Error(), "not found")` with `errors.Is(err, storage.ErrNotFound)` in HTTP handlers
  - Replaced `strings.Contains(err.Error(), "UNIQUE"/"duplicate")` with `errors.Is(err, storage.ErrConflict)` in import handlers
  - Both MemoryStore and SQLite store now wrap errors with sentinel types via `fmt.Errorf("...: %w", ErrXxx)`
  - Added `writeStoreErr()` helper in HTTP server for centralized error-to-status-code mapping
  - Renamed `errors` local variables to `errs` in export handlers to avoid shadowing the `errors` package
  - Added 9 new tests: sentinel error assertions for each error path, plus `WrapIfConflict` table-driven test
- Split `internal/http/server.go` (2277 lines) into 7 focused handler files (#68):
  - `server.go` (185 lines) — Server struct, constructors, route registration, helpers
  - `pool_handlers.go` (561 lines) — Pool CRUD, hierarchy, stats, RBAC handlers
  - `account_handlers.go` (287 lines) — Account CRUD and RBAC handlers
  - `block_handlers.go` (325 lines) — Block listing and subnet enumeration
  - `export_handlers.go` (687 lines) — CSV export/import handlers
  - `system_handlers.go` (169 lines) — Health, readiness, Sentry, OpenAPI, UI
  - `cidr.go` (124 lines) — IPv4 CIDR validation and arithmetic utilities
- Remove confusing `|| true` dead-code condition in `UpdatePoolMeta` (#70)
  - Affected both MemoryStore (`internal/storage/store.go`) and SQLite store (`internal/storage/sqlite/sqlite.go`)
  - The condition `accountID != nil || true` was always true, making the `if` guard misleading
  - Replaced with unconditional assignment matching the newer `UpdatePool` method's behavior
  - Added explicit set-and-clear test (`TestMemoryStore_UpdatePoolMetaSetAndClearAccount`)

### Added - Sprint 5: Enhanced Pool Model & UI

#### Domain Model Enhancements
- Pool types: supernet, region, environment, vpc, subnet
- Pool status: planned, active, deprecated
- Pool source: manual, discovered, imported
- New fields: Description, Tags, UpdatedAt
- PoolStats struct for utilization tracking (TotalIPs, UsedIPs, Utilization)
- PoolWithStats for hierarchy responses with nested children

#### Storage Layer
- New methods: GetPoolWithStats, GetPoolHierarchy, GetPoolChildren
- CalculatePoolUtilization with automatic child CIDR aggregation
- UpdatePool method for modifying pool properties
- SQLite migration (0003_enhanced_pools.sql) for new columns

#### API Enhancements
- `GET /api/v1/pools/hierarchy` - Nested tree structure with stats
- `GET /api/v1/pools/{id}/stats` - Utilization details for a pool
- `GET /api/v1/pools?include_stats=true` - Include stats in pool list
- Updated POST/PATCH endpoints for new pool fields

#### Frontend Overhaul
- Dark sidebar navigation with icons (Dashboard, Pools, Accounts, Discovery, Audit)
- Dashboard view with stats cards and alerts panel
- Hierarchical tree view with expandable nodes
- Pool type indicators (colored dots by type)
- Utilization bars with color coding (green/amber/red)
- Status badges (synced, drift, planned)
- Pool detail slide-out panel

## [0.2.0] - 2026-01-31

### Added - Sprint 1-4: Foundation & Observability

#### Observability (Sprint 1)
- Structured logging with `slog` package (JSON output, request context)
- Request ID middleware for distributed tracing
- Rate limiting middleware with token bucket algorithm
- `/readyz` endpoint with database health check
- Health check improvements

#### Metrics & Validation (Sprint 2)
- Prometheus metrics endpoint (`/metrics`)
- HTTP request counters and latency histograms
- Rate limit metrics (allowed/rejected)
- Active connections gauge
- Input validation hardening for all API inputs
- CIDR validation with IPv4/IPv6 support
- Name and identifier validation

#### Authentication & Audit (Sprint 3)
- API key authentication with Argon2id hashing
- Secure key generation with `cpam_` prefix
- Auth middleware for request validation
- Audit logging infrastructure (`internal/audit/`)
- Memory-backed audit store with filtering
- Key management endpoints:
  - `POST /api/v1/auth/keys` - Create API key
  - `GET /api/v1/auth/keys` - List API keys
  - `DELETE /api/v1/auth/keys/{id}` - Delete API key
  - `POST /api/v1/auth/keys/{id}/revoke` - Revoke API key
  - `GET /api/v1/audit` - Query audit log

#### Authorization & Testing (Sprint 4)
- Role-based access control (RBAC)
  - Roles: admin, operator, viewer, auditor
  - Granular permissions for pools, accounts, apikeys, audit
- Session store interface for future OIDC support
- Authorization middleware (`RequirePermission`, `RequireRole`)
- Comprehensive integration test suite
- Storage interface extensions for PostgreSQL preparation
- Test utilities package (`internal/testutil/`)

### Changed
- Improved error handling with structured error responses
- Enhanced middleware chain with proper ordering
- Updated documentation in CLAUDE.md and README.md

### Test Coverage
- auth: 96.6%
- observability: 96.8%
- storage: 89.6%
- validation: 100%
- audit: 70.9%
- http: 67.0%

## [0.1.0] - 2024-11-04

### Added - Phase 1: Core IPAM

#### Domain Model
- `Pool` entity with hierarchical support (`parent_id`)
- `Account` entity for cloud account management
- CIDR validation and IPv4 block computation

#### Storage Layer
- In-memory store (default)
- SQLite store (build tag `-tags sqlite`)
- Migration system for schema versioning

#### API Endpoints
- `GET /api/v1/pools` - List all pools
- `POST /api/v1/pools` - Create pool
- `GET /api/v1/pools/{id}` - Get pool by ID
- `PATCH /api/v1/pools/{id}` - Update pool
- `DELETE /api/v1/pools/{id}` - Delete pool
- `GET /api/v1/pools/{id}/blocks` - Enumerate available blocks
- `GET /api/v1/accounts` - List accounts
- `POST /api/v1/accounts` - Create account
- `GET /api/v1/accounts/{id}` - Get account
- `PATCH /api/v1/accounts/{id}` - Update account
- `DELETE /api/v1/accounts/{id}` - Delete account
- `GET /api/v1/blocks` - List assigned blocks
- `GET /api/v1/export` - Export data as CSV

#### UI (Alpine.js)
- Pool management table with CRUD operations
- Block browser with prefix selection
- Sub-pool allocation from available blocks
- Account management
- Data export functionality

#### Infrastructure
- Graceful shutdown with signal handling
- Sentry integration for error tracking
- Configurable via environment variables

### Notes
- IPv4 only (IPv6 planned)
- Block detection marks exact CIDR matches as used

[Unreleased]: https://github.com/BadgerOps/cloudpam/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/BadgerOps/cloudpam/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/BadgerOps/cloudpam/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/BadgerOps/cloudpam/releases/tag/v0.1.0
