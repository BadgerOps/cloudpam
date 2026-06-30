# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

CloudPAM is an intelligent IP Address Management (IPAM) platform designed to manage, analyze, and optimize network infrastructure across cloud providers (AWS, GCP, Azure) and on-premises environments. It features a Go backend, React/Vite/TypeScript frontend, and supports both in-memory and SQLite/PostgreSQL storage backends.

**Key Capabilities** (current + planned):
- Centralized IP address management with hierarchical pools
- Multi-cloud discovery and drift detection
- Intelligent analysis (gap analysis, fragmentation, compliance)
- AI-powered planning with LLM integration
- Enterprise features: multi-tenancy, SSO/OIDC, audit logging

## Implementation Status

The project is in **Phase 5** of a 5-phase, 20-week roadmap. See `IMPLEMENTATION_ROADMAP.md` for the complete plan.

**Current State** (Sprint 20 complete):
- SSO/OIDC: generic OIDC provider integration, JIT user provisioning, role mapping, silent session re-auth, client secret encryption, local auth toggle, provider management UI (Sprint 20)
- Auth hardening: auth-always default, CSRF protection, password policy (NIST 800-63B), session limits, login rate limiting, trusted proxies, security settings UI, API key scope elevation prevention (Sprint 19)
- AI Planning: LLM-powered conversational planning with SSE streaming, plan generation and apply (Sprints 17-18)
- AWS Organizations discovery: org-mode agent, cross-account AssumeRole, bulk ingest, Terraform/CF modules, wizard org mode (Sprint 16b)
- Recommendation generator: allocation & compliance recs, scoring, apply/dismiss workflow (Sprint 15)
- Analysis engine: gap analysis, fragmentation scoring, compliance checks (Sprint 14)
- Cloud resource discovery: AWS collector, sync service, approval workflow (Sprint 13)
- Local user management with dual auth (session cookies + API keys) (Sprint 12)
- CIDR search API with containment queries, frontend search modal (Sprint 11)
- Dark mode with three-mode toggle (Light/Dark/System) (Sprint 10)
- Unified React/Vite/TypeScript frontend replacing Alpine.js (Sprint 9)
- Schema Planner wizard with conflict detection (Sprint 8)
- OpenAPI v0.6.0 spec, SQLite-backed API Key Store (Sprint 7)
- Code quality: handler file split, sentinel errors, 80%+ HTTP coverage (Sprint 6)
- Enhanced pool model with hierarchy, stats, utilization tracking (Sprint 5)
- Auth (API keys, RBAC), audit logging, observability (Sprints 1-4)
- Dual storage backends (in-memory, SQLite) with migration system
- Graceful shutdown, Sentry integration, structured logging (`slog`)
- Input validation, rate limiting, `/healthz` + `/readyz` endpoints, Prometheus metrics

**Completed P0/P1 Issues:**
1. ~~Resource leak in SQLite store~~ - Close() method exists
2. ~~Missing graceful shutdown~~ - Implemented
3. ~~Input validation hardening~~ - `internal/validation/` implemented (Sprint 2)
4. ~~Structured logging migration to `slog`~~ - Implemented (Sprint 1)
5. ~~Rate limiting middleware~~ - Implemented (Sprint 1)
6. ~~Health/readiness endpoints (`/healthz`, `/readyz`)~~ - Implemented (Sprint 1)
7. ~~Prometheus metrics~~ - Implemented (Sprint 2)

## Planning Documentation

Recent planning session produced these design documents:

| Document | Purpose |
|----------|---------|
| `docs/IMPLEMENTATION_ROADMAP.md` | 20-week phased implementation plan |
| `docs/REVIEW.md` | Code review with prioritized issues |
| `docs/DATABASE_SCHEMA.md` | Complete PostgreSQL/SQLite schema |
| `docs/AUTH_FLOWS.md` | OAuth2/OIDC and API key authentication |
| `docs/SMART_PLANNING.md` | Analysis engine and AI planning architecture |
| `docs/OBSERVABILITY.md` | Logging, metrics, tracing, audit logging |
| `docs/API_EXAMPLES.md` | API usage examples |
| `docs/DEPLOYMENT.md` | Kubernetes and cloud deployment guides |
| `docs/DISCOVERY.md` | Cloud discovery setup, AWS config, API reference |
| `docs/DISCOVERY_AGENT_PLAN.md` | Standalone discovery agent architecture plan |

**OpenAPI**:
- `/openapi` - interactive runtime API reference
- `/openapi.yaml` - raw OpenAPI 3.1 spec generated from registered server routes
- `docs/openapi-smart-planning.yaml` and `docs/openapi-observability.yaml` - historical/design references

## Build System & Commands

The project uses [Just](https://github.com/casey/just) as its command runner. Install: `cargo install just` or see installation options at https://github.com/casey/just#installation.

### Development
```bash
just dev                         # Run server on :8080 (in-memory store)
DEV_MODE=1 go run ./cmd/cloudpam # Direct Go command (alternative)
```

### Frontend Development
```bash
cd ui && npm install        # Install frontend dependencies
cd ui && npm run dev        # Start Vite dev server (proxied to :8080)
cd ui && npm run build      # Production build → web/dist/
cd ui && npx vitest run     # Run all frontend tests
cd ui && npx tsc --noEmit   # TypeScript type check only
```

### Building
```bash
just build            # Build binary (in-memory store only)
just sqlite-build     # Build with SQLite support (-tags sqlite)
go build -tags postgres ./cmd/cloudpam  # Build with PostgreSQL support
```

### SQLite Mode
```bash
just sqlite-run                                      # Build and run with SQLite
SQLITE_DSN='file:cloudpam.db?cache=shared&_fk=1' ./cloudpam  # Run with custom DSN
./cloudpam -migrate status                           # Check migration status
./cloudpam -migrate up                               # Apply migrations
```

### Testing
```bash
just test             # Run all tests
just test-race        # Run tests with race detector
just cover            # Generate coverage report (coverage.out, coverage.html)
just cover-threshold thr=80  # Check coverage meets threshold
```

### Linting & Formatting
```bash
just fmt              # Format code with go fmt
just lint             # Run golangci-lint v2.1.6 (same as CI)
just tidy             # Run go mod tidy
just install-hooks    # Install pre-commit hook (runs golangci-lint on staged Go files)
```

### OpenAPI Tooling
```bash
just openapi-validate           # Validate generated spec with Go tests
just openapi-validate-url       # Validate a running server spec URL
```

### Screenshot Automation
```bash
npm install
npx playwright install chromium
APP_URL=http://localhost:8080 npm run screenshots  # Outputs to photos/
```

## Architecture

### Current Architecture (Phase 1)

```
┌─────────────────────────────────────────────────────────────┐
│                     CloudPAM Current                        │
├─────────────────────────────────────────────────────────────┤
│  cmd/cloudpam/main.go                                       │
│  ├── HTTP Server (net/http)                                 │
│  ├── Sentry Integration                                     │
│  └── Graceful Shutdown                                      │
├─────────────────────────────────────────────────────────────┤
│  internal/api/                                             │
│  ├── Pool, Account, Block CRUD handlers                     │
│  ├── CSV export/import handlers                             │
│  ├── Auth, audit handlers                                   │
│  └── handleSPA() — unified React SPA serving                │
├─────────────────────────────────────────────────────────────┤
│  ui/ (React/Vite/TypeScript SPA)                            │
│  ├── Pages: Dashboard, Pools, Blocks, Accounts, Audit,      │
│  │         Discovery, Schema, Recommendations               │
│  ├── Schema Planner wizard                                  │
│  ├── API hooks + shared components                          │
│  └── Built to web/dist/ → embedded via go:embed             │
├─────────────────────────────────────────────────────────────┤
│  internal/storage/                                          │
│  ├── Store interface                                        │
│  ├── MemoryStore (default)                                  │
│  └── SQLite store (-tags sqlite)                            │
└─────────────────────────────────────────────────────────────┘
```

### Planned Architecture (Phases 2-5)

```
┌─────────────────────────────────────────────────────────────┐
│                     CloudPAM Target                         │
├─────────────────────────────────────────────────────────────┤
│  internal/api/          │  internal/auth/                  │
│  ├── Middleware chain    │  ├── OIDC provider               │
│  ├── Rate limiting       │  ├── Session management          │
│  └── Request ID          │  └── RBAC authorization          │
├──────────────────────────┼──────────────────────────────────┤
│  internal/planning/      │  internal/discovery/             │
│  ├── Analysis engine     │  ├── AWS collector               │
│  ├── Recommendations     │  ├── GCP collector               │
│  ├── Schema wizard       │  ├── Azure collector             │
│  └── AI/LLM integration  │  └── Sync engine                 │
├──────────────────────────┼──────────────────────────────────┤
│  internal/observability/ │  internal/audit/                 │
│  ├── slog logger         │  ├── Event capture               │
│  ├── OpenTelemetry       │  ├── Database storage            │
│  └── Prometheus metrics  │  └── SIEM export                 │
├─────────────────────────────────────────────────────────────┤
│  internal/storage/                                          │
│  ├── PostgreSQL (production)                                │
│  └── SQLite (development/lightweight)                       │
└─────────────────────────────────────────────────────────────┘
```

### Storage Layer Architecture

The storage layer uses build tags to switch between implementations:

- **Build Tags**: The binary selects storage at compile time via Go build tags
  - Without tags: uses in-memory store (`cmd/cloudpam/store_default.go`)
  - With `-tags sqlite`: uses SQLite store (`cmd/cloudpam/store_sqlite.go`)
  - With `-tags postgres`: uses PostgreSQL store (`cmd/cloudpam/store_postgres.go`)

- **Storage Interface**: `internal/storage/store.go` defines the `Store` and `DiscoveryStore` interfaces
  - `MemoryStore`: in-memory implementation in same file
  - SQLite implementation: `internal/storage/sqlite/sqlite.go`
  - PostgreSQL implementation: `internal/storage/postgres/postgres.go`
  - All stores must implement `Close() error` to release resources on shutdown

- **Migration System**: SQLite builds embed SQL migrations from `migrations/` directory
  - Migrations apply automatically on startup
  - Forward-only; no rollback support
  - Use `./cloudpam -migrate status` to check schema version
  - Current migrations: `0001_init.sql` through `0017_oidc_providers.sql`

- **PostgreSQL Support** (`-tags postgres`)
  - Production-grade database with native CIDR operations
  - Build with `-tags postgres`: `cmd/cloudpam/store_postgres.go`
  - Implementation: `internal/storage/postgres/postgres.go`
  - Configure with `DATABASE_URL` env var
  - See `DATABASE_SCHEMA.md` for complete schema

### Implemented & Planned Packages

| Package | Purpose | Status |
|---------|---------|--------|
| `internal/auth` | Authentication, RBAC, users, sessions | Implemented |
| `internal/audit` | Audit logging | Implemented |
| `internal/discovery` | Cloud resource discovery (Collector, SyncService, AWS Org) | Implemented (AWS single + org) |
| `internal/observability` | Logging, metrics, tracing | Implemented |
| `internal/cidr` | CIDR math utilities | Implemented |
| `internal/planning` | Smart planning engine (analysis, gaps, fragmentation, compliance, recommendations) | Implemented (Phase 3 analysis + recommendations) |
| `internal/planning/llm` | LLM provider abstraction (OpenAI-compatible) | Implemented (Phase 4) |
| `internal/auth/oidc` | OIDC provider integration (discovery, exchange, claims, crypto) | Implemented (Phase 5) |

### HTTP Layer

`internal/api/server.go` implements the REST API and serves the embedded UI:

- **Server struct**: wraps `http.ServeMux` and `storage.Store`
- **Route registration**: `RegisterProtectedRoutes()` sets up all endpoints with RBAC
- **API endpoints**:
  - `/healthz` - health check endpoint
  - `/api/v1/pools` - pool CRUD
  - `/api/v1/pools/{id}` - single pool GET/PATCH/DELETE
  - `/api/v1/pools/{id}/blocks` - enumerate candidate subnets for a pool
  - `/api/v1/accounts` - account CRUD
  - `/api/v1/accounts/{id}` - single account GET/PATCH/DELETE
  - `/api/v1/blocks` - list assigned blocks (sub-pools with filters)
  - `/api/v1/export` - data export as CSV in ZIP
  - `/api/v1/import/{type}` - CSV import for accounts or pools
  - `/api/v1/schema/check` - conflict detection for schema plans
  - `/api/v1/schema/apply` - bulk pool creation from schema plans
  - `/api/v1/audit` - audit event log with pagination
  - `/api/v1/auth/keys` - API key management (CRUD)
  - `/api/v1/auth/login` - session login (POST)
  - `/api/v1/auth/logout` - session logout (POST)
  - `/api/v1/auth/me` - current user/key identity (GET)
  - `/api/v1/auth/users` - user management (GET/POST)
  - `/api/v1/auth/users/{id}` - user CRUD (GET/PATCH/DELETE)
  - `/api/v1/auth/users/{id}/revoke-sessions` - revoke all sessions for a user (POST)
  - `/api/v1/auth/setup` - first-boot admin account creation (POST)
  - `/api/v1/settings/security` - security settings (GET/PATCH)
  - `/api/v1/auth/oidc/providers` - list enabled OIDC providers (GET, public)
  - `/api/v1/auth/oidc/login` - initiate OIDC login flow (GET)
  - `/api/v1/auth/oidc/callback` - OIDC callback handler (GET)
  - `/api/v1/auth/oidc/refresh` - get OIDC refresh URL (POST)
  - `/api/v1/settings/oidc/providers` - OIDC provider admin CRUD (GET/POST)
  - `/api/v1/settings/oidc/providers/{id}` - OIDC provider admin (GET/PATCH/DELETE)
  - `/api/v1/settings/oidc/providers/{id}/test` - test OIDC provider connection (POST)
  - `/api/v1/search` - unified search with CIDR containment queries
  - `/api/v1/discovery/resources` - list discovered cloud resources (filterable)
  - `/api/v1/discovery/resources/{id}` - get single discovered resource
  - `/api/v1/discovery/resources/{id}/link` - link/unlink resource to pool (POST/DELETE)
  - `/api/v1/discovery/sync` - trigger sync (POST) or list sync jobs (GET)
  - `/api/v1/discovery/sync/{id}` - get sync job status
  - `/api/v1/discovery/ingest/org` - bulk org ingest with auto-account creation (POST)
  - `/api/v1/analysis` - full network analysis report (POST)
  - `/api/v1/analysis/gaps` - gap analysis for a pool (POST)
  - `/api/v1/analysis/fragmentation` - fragmentation scoring (POST)
  - `/api/v1/analysis/compliance` - compliance checks (POST)
  - `/api/v1/recommendations/generate` - generate recommendations for pools (POST)
  - `/api/v1/recommendations` - list recommendations with filters (GET)
  - `/api/v1/recommendations/{id}` - get single recommendation (GET)
  - `/api/v1/recommendations/{id}/apply` - apply a recommendation (POST)
  - `/api/v1/recommendations/{id}/dismiss` - dismiss a recommendation (POST)
  - `/api/v1/ai/chat` - send message and stream SSE response (POST)
  - `/api/v1/ai/sessions` - list conversations (GET), create conversation (POST)
  - `/api/v1/ai/sessions/{id}` - get conversation with messages (GET), delete conversation (DELETE)
  - `/api/v1/ai/sessions/{id}/apply-plan` - apply a generated plan as pools (POST)
  - `/api/v1/test-sentry` - Sentry integration test endpoint (use `?type=message|error|panic`)
  - `/readyz` - readiness check with database health
  - `/metrics` - Prometheus metrics endpoint
  - `/openapi` - interactive Scalar API reference
  - `/openapi.yaml` - raw OpenAPI spec generated from registered routes
  - `/` - serves unified React SPA via `handleSPA()` with client-side routing fallback
- **Middleware**: `LoggingMiddleware`, `CSRFMiddleware`, `RateLimitMiddleware`, `RequestIDMiddleware`, `DualAuthMiddleware` (session + API key), `LoginRateLimitMiddleware`
- **Error handling**: uses `apiError` struct with `error` and `detail` fields; 5xx errors are reported to Sentry

### Graceful Shutdown

The server (`cmd/cloudpam/main.go`) implements graceful shutdown:

- Listens for `SIGINT` and `SIGTERM` signals
- Initiates graceful HTTP server shutdown with 15-second timeout
- Closes the storage backend via `store.Close()` to release database connections
- Flushes Sentry events before exit
- Logs shutdown progress at each stage

### Domain Model

`internal/domain/types.go` defines core types:

- **Pool**: represents IP address pools (CIDR blocks) with optional parent/child hierarchy
  - Fields: `ID`, `Name`, `CIDR`, `ParentID` (nullable), `AccountID` (nullable), `CreatedAt`
- **Account**: represents cloud accounts/projects to which pools can be assigned
  - Fields: `ID`, `Key` (unique), `Name`, `Provider`, `ExternalID`, `Description`, `Platform`, `Tier`, `Environment`, `Regions`, `CreatedAt`
  - Supports generic shape for AWS accounts, GCP projects, etc.

### CIDR Validation & Computation

The HTTP server (`internal/api/server.go`) implements IPv4 CIDR logic:

- **Overlap detection**: `prefixesOverlapIPv4()` checks if two prefixes overlap
- **Child validation**: `validateChildCIDR()` ensures child CIDR is within parent bounds
- **Subnet enumeration**: `computeSubnetsIPv4Window()` generates candidate blocks with pagination
- **IPv4 arithmetic**: helper functions convert between `netip.Addr` and `uint32`

### Main Entrypoint

`cmd/cloudpam/main.go`:

- Parses flags: `-addr`, `-migrate`
- Initializes Sentry if `SENTRY_DSN` is set
- Calls `selectStore()` to get storage implementation (defined in build-tag files)
- Sets up HTTP server with timeouts (read: 10s, write: 15s, idle: 60s)
- Handles migration CLI commands before starting server
- Implements graceful shutdown with signal handling

## Nix Development Environment

This project has a Nix flake (`flake.nix`). **Always use `nix develop`** to enter the dev shell before running commands (Go, Node, npm, npx, etc.). This ensures the correct toolchain versions are available. For running shell commands, prefix with `nix develop --command` or enter the shell first.

## Development Guidelines

### Code Style

- Go 1.23+ required (toolchain 1.24.x)
- Use `go fmt` and pass `golangci-lint` (see `.golangci.yml` for enabled linters)
- Linters enabled: `govet`, `staticcheck`, `ineffassign`, `errcheck`, `gocritic`, `misspell`
- Keep errors lowercase and actionable
- Prefer small, focused files

### Testing

- Tests use Go's standard `testing` package
- Tests live alongside code as `*_test.go`
- Use `httptest` helpers for API testing (see `internal/api/handlers_test.go`)
- Run tests with `just test` before committing

### Storage Development

When modifying storage:

- Update the `Store` interface in `internal/storage/store.go`
- Implement methods in MemoryStore, SQLite store (`internal/storage/sqlite/`), and PostgreSQL store (`internal/storage/postgres/`)
- Ensure the `Close() error` method is implemented to release resources
- For schema changes: add new migration file to `migrations/` with sequential prefix (e.g., `0009_description.sql`)
- Test all storage backends: `just test` (in-memory), `just sqlite-build && just test` (SQLite)

### HTTP API Development

When adding endpoints:

- Add handler methods to `Server` in `internal/api/server.go`
- Register routes in `RegisterRoutes()`
- Use `writeJSON()` and `writeErr()` helpers for responses
- Follow RESTful conventions (use proper HTTP methods and status codes)
- Update the route metadata in `internal/api/openapi.go` when API behavior changes
- Validate the generated spec with `just openapi-validate` after changes

### Frontend Development

- Unified React/Vite/TypeScript SPA in `ui/` directory
- Uses `react-router-dom` for client-side routing with 7 page routes
- Tailwind CSS for styling, `lucide-react` for icons
- Static assets built to `web/dist/` and embedded at build time via `web/embed.go`
- UI is served at `/` by `handleSPA()` with SPA fallback for client-side routes
- API hooks in `ui/src/hooks/` (usePools, useAccounts, useBlocks, useAudit, useDiscovery, useAuth, useToast, useRecommendations, useOIDCProviders, useOIDCAdmin, useSessionRefresh)
- Shared types in `ui/src/api/types.ts`, API client in `ui/src/api/client.ts`
- Schema Planner wizard lives in `ui/src/wizard/` (existing from Sprint 8)
- Run `cd ui && npm run dev` for hot-reload development (proxied to Go backend)
- Run `cd ui && npm run build` to produce production bundle in `web/dist/`
- Run `cd ui && npx vitest run` for frontend tests
- For UI changes, update screenshots with `npm run screenshots` (requires app running at `http://localhost:8080`)

## Environment Variables

### Server & Storage
- `ADDR` or `PORT`: listen address (default `:8080`)
- `SQLITE_DSN`: SQLite connection string (default `file:cloudpam.db?cache=shared&_fk=1`)
- `DATABASE_URL`: PostgreSQL connection string (default `postgres://cloudpam:cloudpam@localhost:5432/cloudpam?sslmode=disable`)

### Auth
- `CLOUDPAM_ADMIN_USERNAME`: Bootstrap admin username
- `CLOUDPAM_ADMIN_PASSWORD`: Bootstrap admin password (min 12 chars)
- `CLOUDPAM_ADMIN_EMAIL`: Bootstrap admin email (default: `{username}@localhost`)
- `CLOUDPAM_TRUSTED_PROXIES`: Comma-separated CIDRs for trusted reverse proxies (e.g., `10.0.0.0/8,172.16.0.0/12`)

### OIDC/SSO
- `CLOUDPAM_OIDC_ENCRYPTION_KEY`: 32-byte hex-encoded AES-256 key for OIDC client secret encryption (auto-generated with warning if not set)
- `CLOUDPAM_OIDC_CALLBACK_URL`: OIDC callback URL (default: `http://localhost:8080/api/v1/auth/oidc/callback`)

### Observability
- `CLOUDPAM_LOG_LEVEL`: Log level - debug, info, warn, error (default: `info`)
- `CLOUDPAM_LOG_FORMAT`: Log format - json, text (default: `json`)
- `CLOUDPAM_METRICS_ENABLED`: Enable Prometheus metrics (default: `true`)
- `APP_VERSION`: version stamp for migrations and Sentry release tracking
- `SENTRY_DSN`: Sentry DSN for backend error tracking (optional)
- `SENTRY_FRONTEND_DSN`: Sentry DSN for frontend error tracking (optional)
- `SENTRY_ENVIRONMENT`: Sentry environment name (default: `production`)

### Rate Limiting
- `RATE_LIMIT_RPS`: Requests per second (default: `10.0`; set to `0` to disable)
- `RATE_LIMIT_BURST`: Burst size (default: `20`)

### Agent (Org Mode)
- `CLOUDPAM_AWS_ORG_ENABLED`: Enable AWS Organizations discovery mode
- `CLOUDPAM_AWS_ORG_ROLE_NAME`: IAM role name in member accounts (default: `CloudPAMDiscoveryRole`)
- `CLOUDPAM_AWS_ORG_EXTERNAL_ID`: External ID for STS AssumeRole (optional)
- `CLOUDPAM_AWS_ORG_REGIONS`: Comma-separated AWS regions to discover
- `CLOUDPAM_AWS_ORG_EXCLUDE_ACCOUNTS`: Comma-separated account IDs to skip

### AI Planning
- `CLOUDPAM_LLM_API_KEY`: API key for OpenAI-compatible endpoint (optional when using an authless custom endpoint)
- `CLOUDPAM_LLM_MODEL`: Model name (default: `gpt-4o`)
- `CLOUDPAM_LLM_ENDPOINT`: Base URL override for Ollama/vLLM/Azure; with no API key, a non-empty endpoint enables authless custom-provider mode
- `CLOUDPAM_LLM_MAX_TOKENS`: Max response tokens (default: `4096`)
- `CLOUDPAM_LLM_TEMPERATURE`: Temperature (default: `0.7`)

### Planned
- `CLOUDPAM_TRACING_ENABLED`: Enable distributed tracing
- `CLOUDPAM_TRACING_ENDPOINT`: Jaeger collector endpoint
- `CLOUDPAM_TRACING_SAMPLE_RATE`: Trace sampling rate

## API Contract

The REST API contract is generated at runtime from registered server routes. Use `/openapi` for the interactive reference and `/openapi.yaml` for the raw OpenAPI 3.1 document.

Common workflows:
- Health check: `GET /healthz`
- Readiness check: `GET /readyz`
- Prometheus metrics: `GET /metrics`
- List pools: `GET /api/v1/pools` (add `?include_stats=true` for utilization data)
- Get pool hierarchy: `GET /api/v1/pools/hierarchy`
- Get pool stats: `GET /api/v1/pools/{id}/stats`
- Get single pool: `GET /api/v1/pools/{id}`
- Create pool: `POST /api/v1/pools` with JSON body `{"name":"...", "cidr":"...", "parent_id":..., "account_id":...}`
- Update pool: `PATCH /api/v1/pools/{id}` with JSON body `{"name":"...", "account_id":...}`
- Delete pool: `DELETE /api/v1/pools/{id}` (add `?force=true` for cascade delete)
- Enumerate blocks: `GET /api/v1/pools/{id}/blocks?new_prefix_len=26&page_size=50&page=1`
- List accounts: `GET /api/v1/accounts`
- Create account: `POST /api/v1/accounts` with JSON body `{"key":"...", "name":"...", ...}`
- Delete account: `DELETE /api/v1/accounts/{id}` (add `?force=true` for cascade delete)
- List assigned blocks: `GET /api/v1/blocks?accounts=1,2&pools=10,11&page_size=50&page=1`
- Export data: `GET /api/v1/export?datasets=accounts,pools,blocks`
- Import CSV: `POST /api/v1/import/accounts` or `POST /api/v1/import/pools` with CSV body
- Schema check: `POST /api/v1/schema/check` with JSON body `{"pools":[...]}`
- Schema apply: `POST /api/v1/schema/apply` with JSON body `{"pools":[...], "status":"planned", "tags":{}, "skip_conflicts":false}`
- Audit log: `GET /api/v1/audit?limit=50&offset=0&action=create&resource_type=pool`
- API key management: `POST /api/v1/auth/keys`, `GET /api/v1/auth/keys`, `DELETE /api/v1/auth/keys/{id}`
- Revoke user sessions: `POST /api/v1/auth/users/{id}/revoke-sessions`
- First-boot setup: `POST /api/v1/auth/setup` with `{"username":"...","password":"...","email":"..."}`
- Security settings: `GET /api/v1/settings/security`, `PATCH /api/v1/settings/security` with JSON body
- OIDC providers (public): `GET /api/v1/auth/oidc/providers`
- OIDC login: `GET /api/v1/auth/oidc/login?provider_id={id}` (redirects to IdP)
- OIDC callback: `GET /api/v1/auth/oidc/callback` (handles IdP redirect)
- OIDC refresh: `POST /api/v1/auth/oidc/refresh` (returns redirect URL for silent re-auth)
- OIDC admin list: `GET /api/v1/settings/oidc/providers`
- OIDC admin create: `POST /api/v1/settings/oidc/providers` with `{"name":"...","issuer_url":"...","client_id":"...","client_secret":"..."}`
- OIDC admin get: `GET /api/v1/settings/oidc/providers/{id}`
- OIDC admin update: `PATCH /api/v1/settings/oidc/providers/{id}` with partial fields
- OIDC admin delete: `DELETE /api/v1/settings/oidc/providers/{id}`
- OIDC admin test: `POST /api/v1/settings/oidc/providers/{id}/test`
- Search: `GET /api/v1/search?q=prod&cidr_contains=10.1.2.5&type=pool,account`
- Discovery resources: `GET /api/v1/discovery/resources?account_id=1&status=active&resource_type=vpc`
- Discovery resource detail: `GET /api/v1/discovery/resources/{id}`
- Link resource to pool: `POST /api/v1/discovery/resources/{id}/link` with `{"pool_id":1}`
- Unlink resource: `DELETE /api/v1/discovery/resources/{id}/link`
- Trigger sync: `POST /api/v1/discovery/sync` with `{"account_id":1}`
- List sync jobs: `GET /api/v1/discovery/sync?account_id=1`
- Get sync job: `GET /api/v1/discovery/sync/{id}`
- Org bulk ingest: `POST /api/v1/discovery/ingest/org` with `{"accounts":[{"aws_account_id":"...","resources":[...]}]}`
- Full analysis: `POST /api/v1/analysis` with `{"pool_ids":[1], "include_children":true}`
- Gap analysis: `POST /api/v1/analysis/gaps` with `{"pool_id":1}`
- Fragmentation: `POST /api/v1/analysis/fragmentation` with `{"pool_ids":[1,2]}`
- Compliance: `POST /api/v1/analysis/compliance` with `{"pool_ids":[1,2], "include_children":true}`
- Generate recommendations: `POST /api/v1/recommendations/generate` with `{"pool_ids":[1], "include_children":true}`
- List recommendations: `GET /api/v1/recommendations?pool_id=1&type=allocation&status=pending`
- Get recommendation: `GET /api/v1/recommendations/{id}`
- Apply recommendation: `POST /api/v1/recommendations/{id}/apply` with `{"name":"New Subnet"}`
- Dismiss recommendation: `POST /api/v1/recommendations/{id}/dismiss` with `{"reason":"not needed"}`
- AI chat (SSE streaming): `POST /api/v1/ai/chat` with `{"session_id":"...","message":"Plan a /16 for prod"}`
- List AI sessions: `GET /api/v1/ai/sessions`
- Create AI session: `POST /api/v1/ai/sessions` with `{"title":"My Planning Session"}`
- Get AI session: `GET /api/v1/ai/sessions/{id}`
- Delete AI session: `DELETE /api/v1/ai/sessions/{id}`
- Apply AI plan: `POST /api/v1/ai/sessions/{id}/apply-plan` with `{"plan":{"name":"...","pools":[...]},"skip_conflicts":false}`
- Test Sentry: `GET /api/v1/test-sentry?type=message|error|panic`

## Testing Across Storage Backends

CloudPAM's architecture allows the same test suite to run against all three storage implementations:

1. In-memory: `just test` (default, no build tags)
2. SQLite: `just sqlite-build && just test` (`-tags sqlite`)
3. PostgreSQL: `go test -tags postgres ./...` (requires running PostgreSQL; configure `DATABASE_URL`)

When writing tests, avoid assumptions about storage persistence or specific implementation details.

## CI Configuration

GitHub Actions workflows in `.github/workflows/`:

- **test.yml**: Runs on all branches
  - `test-race` job: builds and runs tests with `-race` flag
  - `coverage` job: generates coverage report with optional threshold via `COVERAGE_THRESHOLD` repository variable
  - Uploads coverage artifacts (coverage.out, coverage.txt, coverage.html)

- **lint.yml**: Runs on main/master and PRs
  - Uses golangci-lint-action v8 with golangci-lint v2.1.6
  - 5-minute timeout

- **release-builds.yml**: Triggered on release publish
  - Builds multi-platform binaries (linux/darwin/windows on amd64/arm64)
  - Uses `-tags sqlite` for SQLite support
  - Attaches archives (.tar.gz/.zip) to the GitHub Release
  - Generates SHA256SUMS.txt checksums
  - Generates SBOM (SPDX JSON format)

- **manual-builds.yml**: Manual workflow dispatch
  - Builds the same matrix as release-builds
  - Configurable git ref and Go version
  - Includes smoke test (runs server, probes /healthz)
  - Uploads build artifacts

CI pins:
- Go `1.24.x`
- golangci-lint `v2.1.6`

## Error Tracking with Sentry

CloudPAM integrates with Sentry for error tracking and performance monitoring:

### Backend Integration
- Captures HTTP errors (5xx status codes)
- Panic recovery with stack traces
- Performance monitoring for all HTTP requests
- Automatic request context capture

### Frontend Integration
- JavaScript error tracking
- Performance monitoring
- Session replay (10% of sessions, 100% of sessions with errors)
- Breadcrumb tracking for user actions

### Setup Instructions

1. Create Sentry projects:
   - One for the backend (Go)
   - One for the frontend (JavaScript) - optional, can use same DSN

2. Set environment variables:
   ```bash
   export SENTRY_DSN="https://your-backend-dsn@sentry.io/project-id"
   export SENTRY_FRONTEND_DSN="https://your-frontend-dsn@sentry.io/project-id"
   export SENTRY_ENVIRONMENT="production"  # or staging, dev, etc.
   export APP_VERSION="v1.0.0"  # used as release identifier
   ```

3. Run the application:
   ```bash
   just dev  # or just build && ./cloudpam
   ```

4. Sentry will automatically:
   - Initialize on startup (backend logs confirmation)
   - Capture panics and 5xx errors
   - Track HTTP performance
   - Report frontend errors and replays

5. Test Sentry integration:
   ```bash
   curl "http://localhost:8080/api/v1/test-sentry?type=message"  # Send test message
   curl "http://localhost:8080/api/v1/test-sentry?type=error"    # Trigger 500 error
   curl "http://localhost:8080/api/v1/test-sentry?type=panic"    # Trigger panic
   ```

### Notes
- If `SENTRY_DSN` is not set, Sentry integration is disabled (no overhead)
- Frontend DSN is injected into HTML at runtime via meta tag
- TracesSampleRate is set to 1.0 (100%) - adjust in `cmd/cloudpam/main.go` for high-traffic environments
- Session replay samples 10% of sessions by default - adjust in `web/index.html` if needed

## Project Structure

```
cloudpam/
├── cmd/cloudpam/           # Main entrypoint and storage selection
│   ├── main.go             # Server startup, flags, graceful shutdown
│   ├── store_default.go    # In-memory store selection (default build)
│   ├── store_sqlite.go     # SQLite store selection (-tags sqlite)
│   └── store_postgres.go   # PostgreSQL store selection (-tags postgres)
├── internal/
│   ├── domain/             # Core types (Pool, Account, DiscoveredResource, SyncJob, Recommendation)
│   │   ├── types.go        # Pool, Account types
│   │   ├── discovery.go    # Discovery domain types
│   │   ├── recommendations.go # Recommendation types
│   │   ├── settings.go     # SecuritySettings type
│   │   ├── oidc.go         # OIDCProvider type
│   │   └── models.go       # Extended models (planned)
│   ├── api/                # HTTP server, routes, handlers
│   │   ├── server.go       # Server struct, route registration, helpers
│   │   ├── pool_handlers.go       # Pool CRUD, hierarchy, stats
│   │   ├── account_handlers.go    # Account CRUD
│   │   ├── block_handlers.go      # Block listing, subnet enumeration
│   │   ├── export_handlers.go     # CSV export/import
│   │   ├── system_handlers.go     # Health, readiness, Sentry, SPA
│   │   ├── discovery_handlers.go  # Discovery API (resources, sync)
│   │   ├── auth_handlers.go       # Auth (login, logout, keys, users)
│   │   ├── user_handlers.go       # User management
│   │   ├── settings_handlers.go   # Security settings API
│   │   ├── csrf.go                # CSRF double-submit cookie middleware
│   │   ├── recommendation_handlers.go # Recommendation API (generate, apply, dismiss)
│   │   ├── ai_handlers.go       # AI Planning API (chat, sessions, plan apply)
│   │   ├── oidc_handlers.go     # OIDC login/callback/refresh + admin CRUD
│   │   ├── middleware.go   # Middleware (logging, auth, rate limit, trusted proxies)
│   │   ├── context.go      # Request context helpers
│   │   ├── cidr.go         # IPv4 CIDR validation utilities
│   │   └── *_test.go       # Tests
│   ├── storage/            # Storage interface and implementations
│   │   ├── store.go        # Store interface and MemoryStore
│   │   ├── discovery.go    # DiscoveryStore interface
│   │   ├── discovery_memory.go  # In-memory DiscoveryStore
│   │   ├── recommendations.go   # RecommendationStore interface
│   │   ├── recommendations_memory.go # In-memory RecommendationStore
│   │   ├── settings.go          # SettingsStore interface
│   │   ├── settings_memory.go   # In-memory SettingsStore
│   │   ├── oidc.go              # OIDCProviderStore interface
│   │   ├── oidc_memory.go       # In-memory OIDCProviderStore
│   │   ├── errors.go       # Sentinel errors (ErrNotFound, etc.)
│   │   ├── sqlite/         # SQLite implementation
│   │   │   ├── sqlite.go
│   │   │   ├── discovery.go    # SQLite DiscoveryStore
│   │   │   ├── recommendations.go # SQLite RecommendationStore
│   │   │   ├── settings.go     # SQLite SettingsStore
│   │   │   ├── oidc.go         # SQLite OIDCProviderStore
│   │   │   └── migrator.go
│   │   └── postgres/       # PostgreSQL implementation
│   │       ├── postgres.go
│   │       └── migrator.go
│   ├── discovery/          # Cloud resource discovery
│   │   ├── collector.go    # Collector interface + SyncService
│   │   └── aws/            # AWS collector (VPCs, subnets, EIPs)
│   │       ├── collector.go
│   │       ├── org.go          # ListOrgAccounts (AWS Organizations)
│   │       └── assume_role.go  # STS AssumeRole helper
│   ├── auth/               # Authentication/authorization
│   │   ├── rbac.go         # Roles, permissions, RBAC middleware
│   │   ├── keys.go         # API key types and store interfaces
│   │   ├── users.go        # User types and store interfaces
│   │   ├── password.go     # Password hashing and validation (NIST 800-63B)
│   │   ├── sessions.go     # Session management
│   │   ├── sqlite.go       # SQLite implementations
│   │   └── oidc/            # OIDC provider integration
│   │       ├── provider.go  # OIDC discovery, auth URL, code exchange
│   │       ├── claims.go    # ID token claims and role mapping
│   │       └── crypto.go    # AES-256-GCM client secret encryption
│   ├── audit/              # Audit logging
│   ├── cidr/               # Reusable CIDR math utilities
│   ├── validation/         # Input validation
│   ├── planning/           # Smart planning engine (analysis, gaps, fragmentation, compliance, recommendations)
│   │   └── llm/            # LLM provider abstraction (OpenAI-compatible)
│   ├── observability/      # Logging, metrics, tracing
│   └── docs/               # Internal documentation handlers
├── migrations/             # SQL migrations (0001-0017)
│   ├── embed.go
│   ├── 0001_init.sql
│   ├── 0002_accounts_meta.sql
│   ├── ...
│   ├── 0008_discovered_resources.sql  # Discovery tables
│   ├── 0017_oidc_providers.sql        # OIDC providers + user OIDC columns
│   └── postgres/           # PostgreSQL migrations
├── deploy/                 # Deployment configurations
│   ├── terraform/aws-org-discovery/  # AWS Organizations discovery IAM
│   │   ├── management-policy/  # Agent role + policies (management account)
│   │   └── member-role/        # Discovery role (member accounts)
│   ├── cloudformation/         # CloudFormation templates
│   │   └── discovery-role-stackset.yaml  # StackSet for org-wide member role
│   ├── docker/             # Docker build files
│   ├── helm/               # Helm charts
│   ├── k8s/                # Kubernetes manifests (observability stack)
│   ├── vector/             # Vector log shipping config
│   └── docker-compose.observability.yml
├── ui/                     # Frontend (React/Vite/TypeScript SPA)
│   ├── src/
│   │   ├── App.tsx           # Router + layout
│   │   ├── api/              # API client + types
│   │   ├── hooks/            # React hooks (usePools, useAccounts, useDiscovery, useRecommendations, etc.)
│   │   ├── components/       # Shared components (Layout, Sidebar, modals, etc.)
│   │   ├── pages/            # Page components (Dashboard, Pools, Discovery, Recommendations, etc.)
│   │   ├── utils/            # Utility functions (format, colors)
│   │   ├── wizard/           # Schema Planner wizard
│   │   └── __tests__/        # Frontend tests
│   ├── package.json
│   └── vite.config.ts
├── web/                    # Embedded frontend assets
│   ├── embed.go              # go:embed for dist/ directory
│   └── dist/                 # Built frontend output (from ui/)
├── docs/                   # Documentation
│   ├── openapi-smart-planning.yaml  # Smart Planning API spec (planned)
│   ├── openapi-observability.yaml   # Observability API spec (planned)
│   ├── spec_embed.go
│   ├── PROJECT_PLAN.md
│   ├── IMPLEMENTATION_ROADMAP.md
│   ├── REVIEW.md
│   ├── DATABASE_SCHEMA.md
│   ├── AUTH_FLOWS.md
│   ├── SMART_PLANNING.md
│   ├── OBSERVABILITY.md
│   └── CHANGELOG.md
├── scripts/                # Utility scripts
├── photos/                 # Screenshots (Git LFS tracked)
├── .github/workflows/      # CI/CD workflows
├── Justfile                # Task runner commands
├── .golangci.yml           # Linter configuration
├── go.mod / go.sum         # Go module files
└── CLAUDE.md               # This file
```

## Roadmap Context

See `IMPLEMENTATION_ROADMAP.md` for the detailed 20-week plan. Summary:

| Phase | Weeks | Focus | Key Deliverables |
|-------|-------|-------|------------------|
| **1: Foundation** | 1-4 | Core infrastructure | Auth, database layer, basic pools |
| **2: Cloud Integration** | 5-8 | Multi-cloud | AWS/GCP/Azure discovery, sync engine |
| **3: Smart Planning** | 9-12 | Analysis | Gap analysis, recommendations, schema wizard |
| **4: AI Planning** | 13-16 | LLM integration | Conversational planning, plan generation |
| **5: Enterprise** | 17-20 | Production ready | Multi-tenancy, SSO, audit, rate limiting |

### Remaining Work

**Phase 2 gaps (Cloud Integration):**
- GCP discovery collector
- Azure discovery collector
- Drift detection (discovered vs managed state)

**Phase 5 (Enterprise):**
- Multi-tenancy enforcement (schema exists, not enforced)
- ~~SSO/OIDC integration~~ ✅ (Sprint 20)
- Log shipping / SIEM integration
- Per-org rate limiting and quotas

### Development Priorities

When implementing new features, follow this order:
1. Open GitHub issues (check `gh issue list`)
2. Phase 2 gaps (GCP/Azure discovery, drift detection)
3. Phase 5 enterprise features (multi-tenancy, SSO)

## Additional Documentation

- `docs/PROJECT_PLAN.md` - Original project roadmap
- `API_EXAMPLES.md` - API usage examples
- `DEPLOYMENT.md` - Deployment guides
- `CLEANUP.md` - Documentation consolidation notes
