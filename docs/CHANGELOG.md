# Changelog

All notable changes to CloudPAM will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added - Sprint 12: Local User Management & Dual Auth

#### User Management
- `internal/auth/` user types: `User`, `UserStore`, `SessionStore` interfaces with SQLite implementations
- Password hashing with Argon2id (`HashPassword`, `VerifyPassword`)
- Session management: create, validate, delete, delete-by-user-ID, auto-expiry cleanup
- Bootstrap admin from environment variables (`CLOUDPAM_ADMIN_USERNAME`, `CLOUDPAM_ADMIN_PASSWORD`)
- User CRUD endpoints: `GET/POST /api/v1/auth/users`, `GET/PATCH/DELETE /api/v1/auth/users/{id}`
- Self-service password change: `PATCH /api/v1/auth/users/{id}/password`
- User management page (`/settings/users`): create, edit role, deactivate (admin only)

#### Dual Authentication
- `DualAuthMiddleware`: accepts both session cookies (browser) and Bearer API keys (programmatic)
- Login endpoint (`POST /api/v1/auth/login`): validates credentials, creates HttpOnly+Secure session cookie
- Logout endpoint (`POST /api/v1/auth/logout`): invalidates session, clears cookie
- `GET /api/v1/auth/me`: returns current user or API key identity
- `/healthz` returns `local_auth_enabled` boolean when user store is configured

#### Browser Auth — Cookie-Only
- Browser authentication uses HttpOnly + Secure + SameSite=Strict session cookies exclusively
- No API keys or tokens stored in browser storage (localStorage/sessionStorage)
- Login page is username/password only — API keys are for programmatic use (curl, scripts, CI)
- API client uses `credentials: 'same-origin'` for automatic cookie handling
- `ProtectedRoute` redirects unauthenticated users when any auth mode is enabled

#### Security Hardening
- Session cookies set `Secure: true`, `HttpOnly: true`, `SameSite: Strict`
- Removed all sensitive credential storage from browser (CodeQL HIGH findings resolved)
- API key `owner_id` column links keys to users (nullable for standalone/bot keys)

### Added - Sprint 11: CIDR Search & Frontend Search

#### Server-side Search (M3)
- `internal/cidr/` package: reusable CIDR math (`PrefixContains`, `PrefixContainsAddr`, `ParseCIDROrIP`) with 26 tests
- `internal/domain/search.go`: `SearchRequest`, `SearchResultItem`, `SearchResponse` types
- `Search()` method added to Store interface, implemented in MemoryStore, SQLite, and PostgreSQL backends
- `GET /api/v1/search?q=&cidr_contains=&cidr_within=&type=&page=&page_size=` endpoint with RBAC (`pools:read`)
- PostgreSQL uses native CIDR operators (`>>=`, `<<=`); SQLite/Memory filter in Go
- OpenAPI spec bumped to v0.5.0 with Search tag, `SearchResultItem` and `SearchResponse` schemas

#### Frontend Search Upgrade
- `useSearch` hook with 300ms debounce, auto-detects CIDR/IP patterns
- `SearchModal` upgraded from client-side filtering to server-side search via `/api/v1/search`

#### Frontend Auth UI (M4)
- API key management page (`/settings/api-keys`): create (name + scopes + expiry), revoke, delete
- Sidebar: shows username + role badge for session users, API Keys link (admin only), Logout button
- `/healthz` now returns `auth_enabled` boolean field

### Added - Sprint 10: Dark Mode

#### Theme System
- Three-mode theme toggle: Light, Dark, System (follows OS preference)
- Theme state managed via React context (`useTheme` hook) with localStorage persistence
- Flash prevention script in `index.html` applies `.dark` class before first paint
- Tailwind CSS v4 class-based dark mode via `@custom-variant dark (&:where(.dark, .dark *));`
- Theme toggle button in sidebar footer with Sun/Moon/Monitor icons (lucide-react)

#### Dark Mode Styling
- All 7 pages dark-mode ready: Dashboard, Pools, Blocks, Accounts, Discovery, Audit, Schema Planner
- All shared components: Header, Sidebar, SearchModal, ToastContainer, ImportExportModal, PoolTree, PoolDetailPanel, StatusBadge
- All wizard components: SchemaPlanner, TemplateStep, StrategyStep, DimensionsStep, PreviewStep, TreeNode
- Badge utility functions updated with `dark:` variants (provider, tier, status, action badges)
- Root layout `dark:bg-gray-900` background, sidebar `dark:bg-gray-950` for depth contrast

### Added - Sprint 9: Unified React Frontend

#### Alpine.js → React Migration
- Replaced Alpine.js SPA (`web/index.html`, ~2600 lines) with unified React/Vite/TypeScript SPA
- Single React app now serves at `/` instead of separate Alpine.js (`/`) + React wizard (`/wizard/`)
- Deleted `web/index.html`; all UI served from `ui/` build output via Go `embed.FS`

#### Go Backend — Unified SPA Serving
- New `handleSPA()` handler replaces `handleIndex()` + `handleWizardAssets()`
- SPA fallback: serves `index.html` for all non-file routes (client-side routing)
- Sentry frontend DSN injection via `<meta>` tag at runtime
- Removed `/wizard/` route registration from both `RegisterRoutes()` and `RegisterProtectedRoutes()`
- Updated `web/embed.go`: removed `Index []byte`, kept `DistFS embed.FS`

#### React Router & Layout
- Added `react-router-dom` with `BrowserRouter` and 7 routes
- Sidebar navigation with `NavLink` active state (Dashboard, Pools, Blocks, Accounts, Discovery, Audit, Schema)
- Header with Cmd+K search trigger
- Layout component with sidebar + header + `<Outlet />`
- Routes: `/` (Dashboard), `/pools`, `/blocks`, `/accounts`, `/audit`, `/discovery`, `/schema`

#### Pages (7 new page components)
- **DashboardPage**: stats cards (pools, IPs, accounts, alerts), hierarchical pool tree, utilization alerts, recent activity timeline, accounts summary
- **PoolsPage**: pool CRUD with create form, search, table, detail panel with utilization stats, delete with confirmation
- **BlocksPage**: blocks table with search, account filter, summary stats (total blocks, IPs, unique accounts) (#34)
- **AccountsPage**: account CRUD with create form, search/filter by name/key/provider, provider+tier badges, delete (#36)
- **AuditPage**: audit event timeline with expandable details (before/after diffs), action/resource filters, pagination (#34)
- **DiscoveryPage**: placeholder for cloud discovery
- **SchemaPage**: wraps existing Schema Planner wizard

#### Components (9 new shared components)
- `Layout.tsx`, `Sidebar.tsx`, `Header.tsx` — app shell
- `SearchModal.tsx` — Cmd+K global search across pools and accounts
- `ImportExportModal.tsx` — export ZIP + CSV import with preview table and results (#23)
- `ToastContainer.tsx` + `useToast.ts` — toast notification system (info/error/success)
- `PoolTree.tsx` — recursive hierarchical tree with expand/collapse, type-colored dots, utilization bars
- `PoolDetailPanel.tsx` — slide-out panel with pool stats, utilization bar, child count
- `StatusBadge.tsx` — reusable badge for status/provider/tier/type

#### API Layer (hooks + types)
- 5 new React hooks: `usePools`, `useAccounts`, `useBlocks`, `useAudit`, `useToast`
- Extended `api/types.ts` with Pool, PoolWithStats, PoolStats, Account, Block, AuditEvent, BlocksListResponse, AuditListResponse, ImportResult types
- Added `patch()` and `del()` methods to API client
- `utils/format.ts` — ported helpers: `formatHostCount`, `formatTimeAgo`, `getHostCount`, color/badge class helpers

#### Schema Planner Fixes
- Fixed schema wizard generating invalid pool types (`root` → `supernet`, `account` → `vpc`)
- Updated TreeNode type colors, blueprints hierarchy levels, and test assertions

#### Tests
- 34 frontend tests pass (15 format utils + 14 CIDR + 5 schema generator)
- All Go tests pass (http, storage, auth, audit, observability, validation, domain)
- Frontend production build: 274KB JS + 25KB CSS

### Added - Sprint 8: Schema Planner Wizard + React/Vite Scaffold

#### Schema Planner Wizard
- 4-step wizard: Template → Strategy → Dimensions → Preview
- 3 blueprint templates: Enterprise Multi-Region, Medium Organization, Small Team
- 3 layout strategies: region-first, environment-first, account-first
- Configurable dimensions: regions, environments, accounts per environment, account tiers
- Real-time CIDR subdivision preview with hierarchical tree view
- Conflict detection against existing pools before apply
- Bulk pool creation with topological ordering

#### Backend — Schema Endpoints
- `POST /api/v1/schema/check` — conflict detection against existing pools
- `POST /api/v1/schema/apply` — bulk pool creation with topological ordering
- OpenAPI spec updated to v0.4.0 with Schema tag and 7 new types
- 10 new Go tests for schema handlers

#### React/Vite/TypeScript Scaffold
- React/Vite/TypeScript project in `ui/` with Tailwind CSS
- Wizard components: TemplateStep, StrategyStep, DimensionsStep, PreviewStep
- CIDR utilities (`subdivide`, `usableHosts`, `formatHostCount`)
- Hooks: `useSchemaGenerator`, `useConflictChecker`, `useApplySchema`
- 19 vitest tests (14 CIDR + 5 schema generator)

#### Infrastructure
- Node.js 22 added to Nix flake devShell
- Justfile recipes: `ui-install`, `ui-build`, `ui-dev`, `ui-test`, `build-full`, `dev-all`
- `dev-all` runs Go backend + Vite dev server concurrently
- `web/dist/index.html` placeholder for `go:embed` on clean checkout

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
