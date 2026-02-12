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

The project is currently at **Phase 1** of a 5-phase, 20-week roadmap. See `IMPLEMENTATION_ROADMAP.md` for the complete plan.

**Current State** (Sprint 9 complete):
- Unified React/Vite/TypeScript frontend replacing Alpine.js (Sprint 9)
- Schema Planner wizard with conflict detection (Sprint 8)
- OpenAPI v0.4.0 spec, SQLite-backed API Key Store (Sprint 7)
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
| `IMPLEMENTATION_ROADMAP.md` | 20-week phased implementation plan |
| `REVIEW.md` | Code review with prioritized issues |
| `DATABASE_SCHEMA.md` | Complete PostgreSQL/SQLite schema |
| `AUTH_FLOWS.md` | OAuth2/OIDC and API key authentication |
| `SMART_PLANNING.md` | Analysis engine and AI planning architecture |
| `OBSERVABILITY.md` | Logging, metrics, tracing, audit logging |
| `API_EXAMPLES.md` | API usage examples |
| `DEPLOYMENT.md` | Kubernetes and cloud deployment guides |
| `CLEANUP.md` | Documentation consolidation notes |

**OpenAPI Specifications**:
- `docs/openapi.yaml` - Core IPAM API (current)
- `openapi-smart-planning.yaml` - Smart Planning API (planned)
- `openapi-observability.yaml` - Audit/observability API (planned)

## Build System & Commands

The project uses [Just](https://github.com/casey/just) as its command runner. Install: `cargo install just` or see installation options at https://github.com/casey/just#installation.

### Development
```bash
just dev              # Run server on :8080 (in-memory store)
go run ./cmd/cloudpam # Direct Go command (alternative)
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
just build            # Build binary without SQLite
just sqlite-build     # Build with SQLite support (-tags sqlite)
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
just lint             # Run golangci-lint (requires v1.61.0+ for Go 1.24)
just tidy             # Run go mod tidy
```

### OpenAPI Tooling
```bash
just openapi-validate           # Validate spec with Ruby/Psych
just openapi-html               # Generate HTML docs to docs/openapi-html/
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
│  internal/http/                                             │
│  ├── Pool, Account, Block CRUD handlers                     │
│  ├── CSV export/import handlers                             │
│  ├── Auth, audit handlers                                   │
│  └── handleSPA() — unified React SPA serving                │
├─────────────────────────────────────────────────────────────┤
│  ui/ (React/Vite/TypeScript SPA)                            │
│  ├── Pages: Dashboard, Pools, Blocks, Accounts, Audit       │
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
│  internal/http/          │  internal/auth/                  │
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
  - Without `-tags sqlite`: uses in-memory store (`cmd/cloudpam/store_default.go`)
  - With `-tags sqlite`: uses SQLite store (`cmd/cloudpam/store_sqlite.go`)

- **Storage Interface**: `internal/storage/store.go` defines the `Store` interface
  - `MemoryStore`: in-memory implementation in same file
  - SQLite implementation: `internal/storage/sqlite/sqlite.go`
  - All stores must implement `Close() error` to release resources on shutdown

- **Migration System**: SQLite builds embed SQL migrations from `migrations/` directory
  - Migrations apply automatically on startup
  - Forward-only; no rollback support
  - Use `./cloudpam -migrate status` to check schema version
  - Current migrations: `0001_init.sql`, `0002_accounts_meta.sql`

- **Planned: PostgreSQL Support** (Phase 1-2)
  - Production-grade database with native CIDR operations
  - Separate migration files in `migrations/postgres/`
  - Row-level security for multi-tenancy
  - See `DATABASE_SCHEMA.md` for complete schema

### Planned Packages

New Go packages defined with interfaces (see `internal/*/interfaces.go`):

| Package | Purpose | Key Interfaces |
|---------|---------|----------------|
| `internal/planning` | Smart planning engine | `DiscoveryService`, `AnalysisService`, `RecommendationService`, `AIPlanningService`, `SchemaWizardService`, `LLMProvider` |
| `internal/observability` | Logging, metrics, tracing | `Logger`, `Metrics`, `Tracer`, `AuditLogger`, `HealthChecker` |
| `internal/auth` | Authentication/authorization | (planned) `AuthProvider`, `SessionStore`, `Authorizer` |
| `internal/discovery` | Cloud resource discovery | (planned) `Collector`, `SyncEngine` |

These interfaces are defined and ready for implementation.

### HTTP Layer

`internal/http/server.go` implements the REST API and serves the embedded UI:

- **Server struct**: wraps `http.ServeMux` and `storage.Store`
- **Route registration**: `RegisterRoutes()` sets up all endpoints
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
  - `/api/v1/test-sentry` - Sentry integration test endpoint (use `?type=message|error|panic`)
  - `/readyz` - readiness check with database health
  - `/metrics` - Prometheus metrics endpoint
  - `/openapi.yaml` - OpenAPI spec served from embedded `docs/spec_embed.go`
  - `/` - serves unified React SPA via `handleSPA()` with client-side routing fallback
- **Middleware**: `LoggingMiddleware` logs requests and captures Sentry performance traces
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

The HTTP server (`internal/http/server.go`) implements IPv4 CIDR logic:

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
- Use `httptest` helpers for API testing (see `internal/http/handlers_test.go`)
- Run tests with `just test` before committing

### Storage Development

When modifying storage:

- Update the `Store` interface in `internal/storage/store.go`
- Implement methods in both `MemoryStore` (same file) and SQLite store (`internal/storage/sqlite/sqlite.go`)
- Ensure the `Close() error` method is implemented to release resources
- For SQLite schema changes: add new migration file to `migrations/` with sequential prefix (e.g., `0003_description.sql`)
- Test both storage backends: run `just test` (in-memory) and `just sqlite-build && just test` (SQLite)

### HTTP API Development

When adding endpoints:

- Add handler methods to `Server` in `internal/http/server.go`
- Register routes in `RegisterRoutes()`
- Use `writeJSON()` and `writeErr()` helpers for responses
- Follow RESTful conventions (use proper HTTP methods and status codes)
- Update `docs/openapi.yaml` to reflect API changes
- Validate spec with `just openapi-validate` after changes

### Frontend Development

- Unified React/Vite/TypeScript SPA in `ui/` directory
- Uses `react-router-dom` for client-side routing with 7 page routes
- Tailwind CSS for styling, `lucide-react` for icons
- Static assets built to `web/dist/` and embedded at build time via `web/embed.go`
- UI is served at `/` by `handleSPA()` with SPA fallback for client-side routes
- API hooks in `ui/src/hooks/` (usePools, useAccounts, useBlocks, useAudit, useToast)
- Shared types in `ui/src/api/types.ts`, API client in `ui/src/api/client.ts`
- Schema Planner wizard lives in `ui/src/wizard/` (existing from Sprint 8)
- Run `cd ui && npm run dev` for hot-reload development (proxied to Go backend)
- Run `cd ui && npm run build` to produce production bundle in `web/dist/`
- Run `cd ui && npx vitest run` for frontend tests
- For UI changes, update screenshots with `npm run screenshots` (requires app running at `http://localhost:8080`)

## Environment Variables

### Current
- `ADDR` or `PORT`: listen address (default `:8080`)
- `SQLITE_DSN`: SQLite connection string (default `file:cloudpam.db?cache=shared&_fk=1`)
- `APP_VERSION`: optional version stamp for migrations and Sentry release tracking
- `SENTRY_DSN`: Sentry DSN for backend error tracking (optional)
- `SENTRY_FRONTEND_DSN`: Sentry DSN for frontend error tracking (optional, can be different from backend DSN)
- `SENTRY_ENVIRONMENT`: Sentry environment name (default: `production`)

### Planned (from OBSERVABILITY.md)
- `CLOUDPAM_LOG_LEVEL`: Log level - debug, info, warn, error (default: `info`)
- `CLOUDPAM_LOG_FORMAT`: Log format - json, text (default: `json`)
- `CLOUDPAM_METRICS_ENABLED`: Enable Prometheus metrics (default: `true`)
- `CLOUDPAM_METRICS_PORT`: Metrics endpoint port (default: `8889`)
- `CLOUDPAM_TRACING_ENABLED`: Enable distributed tracing (default: `true`)
- `CLOUDPAM_TRACING_ENDPOINT`: Jaeger collector endpoint
- `CLOUDPAM_TRACING_SAMPLE_RATE`: Trace sampling rate (default: `0.01`)
- `DATABASE_URL`: PostgreSQL connection string (planned)
- `RATE_LIMIT_RPS`: Rate limit requests per second (planned)
- `RATE_LIMIT_BURST`: Rate limit burst size (planned)

## API Contract

The REST API contract is captured in `docs/openapi.yaml` (OpenAPI 3.1). The spec is also served at `/openapi.yaml` when the server is running.

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
- Test Sentry: `GET /api/v1/test-sentry?type=message|error|panic`

## Testing Across Storage Backends

CloudPAM's architecture allows the same test suite to run against both storage implementations:

1. Run without SQLite: `just test` (tests use in-memory store)
2. Run with SQLite: `just sqlite-build && just test` (tests use SQLite store if available)

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
│   └── store_sqlite.go     # SQLite store selection (-tags sqlite)
├── internal/
│   ├── domain/             # Core types (Pool, Account)
│   │   ├── types.go        # Current types
│   │   └── models.go       # Extended models (planned)
│   ├── http/               # HTTP server, routes, handlers
│   │   ├── server.go       # Server implementation and all handlers
│   │   ├── middleware.go   # Middleware (logging, auth, rate limit)
│   │   ├── context.go      # Request context helpers
│   │   └── *_test.go       # Tests
│   ├── storage/            # Storage interface and implementations
│   │   ├── store.go        # Store interface and MemoryStore
│   │   ├── interfaces.go   # Extended storage interfaces (planned)
│   │   ├── cidr.go         # CIDR utility functions (planned)
│   │   └── sqlite/         # SQLite implementation
│   │       ├── sqlite.go
│   │       └── migrator.go
│   ├── validation/         # Input validation (partial)
│   │   └── validation.go
│   ├── planning/           # Smart planning engine (interfaces defined)
│   │   └── interfaces.go   # DiscoveryService, AnalysisService, etc.
│   ├── observability/      # Logging, metrics, tracing (interfaces defined)
│   │   └── interfaces.go   # Logger, Metrics, Tracer, AuditLogger
│   └── docs/               # Internal documentation handlers
│       └── handler.go
├── migrations/             # SQL migrations
│   ├── embed.go
│   ├── 0001_init.sql       # Current SQLite migrations
│   ├── 0002_accounts_meta.sql
│   ├── postgres/           # PostgreSQL migrations (planned)
│   │   ├── 001_initial_schema.up.sql
│   │   └── 001_initial_schema.down.sql
│   └── sqlite/             # SQLite migrations (new structure)
│       ├── 001_initial_schema.up.sql
│       └── 001_initial_schema.down.sql
├── deploy/                 # Deployment configurations (planned)
│   ├── k8s/                # Kubernetes manifests
│   │   ├── observability-stack.yaml
│   │   └── vector-daemonset.yaml
│   ├── vector/             # Vector log shipping config
│   │   └── vector.toml
│   └── docker-compose.observability.yml
├── ui/                     # Frontend (React/Vite/TypeScript SPA)
│   ├── src/
│   │   ├── App.tsx           # Router + layout
│   │   ├── api/              # API client + types
│   │   ├── hooks/            # React hooks (usePools, useAccounts, etc.)
│   │   ├── components/       # Shared components (Layout, Sidebar, modals, etc.)
│   │   ├── pages/            # Page components (Dashboard, Pools, Blocks, etc.)
│   │   ├── utils/            # Utility functions (format, colors)
│   │   ├── wizard/           # Schema Planner wizard
│   │   └── __tests__/        # Frontend tests
│   ├── package.json
│   └── vite.config.ts
├── web/                    # Embedded frontend assets
│   ├── embed.go              # go:embed for dist/ directory
│   └── dist/                 # Built frontend output (from ui/)
├── docs/                   # Documentation
│   ├── openapi.yaml        # Core API spec
│   ├── spec_embed.go
│   ├── PROJECT_PLAN.md
│   └── CHANGELOG.md
├── scripts/                # Utility scripts
├── photos/                 # Screenshots (Git LFS tracked)
├── .github/workflows/      # CI/CD workflows
├── Justfile                # Task runner commands
├── .golangci.yml           # Linter configuration
├── go.mod / go.sum         # Go module files
├── CLAUDE.md               # This file
├── IMPLEMENTATION_ROADMAP.md  # 20-week implementation plan
├── REVIEW.md               # Code review with prioritized issues
├── DATABASE_SCHEMA.md      # Complete database schema
├── AUTH_FLOWS.md           # Authentication architecture
├── SMART_PLANNING.md       # Smart planning architecture
├── OBSERVABILITY.md        # Observability architecture
├── openapi-smart-planning.yaml   # Smart Planning API spec
└── openapi-observability.yaml    # Observability API spec
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

### Immediate Next Steps (from REVIEW.md)

**Sprint 1 - Critical Fixes:**
1. Add input validation package (`internal/validation/`)
2. Migrate to structured logging with `slog`
3. Add `/readyz` endpoint with database health check
4. Implement rate limiting middleware

**Sprint 2 - Observability:**
5. Implement `internal/observability/` interfaces
6. Add Prometheus metrics endpoint
7. Add request ID middleware
8. Increase test coverage to 65%+

### Development Priorities

When implementing new features, follow this order:
1. **P0 issues** from REVIEW.md (critical bugs/gaps)
2. **P1 issues** (production readiness)
3. **Phase 1** roadmap items (foundation)
4. **Phase 2+** features (cloud integration, planning)

## Additional Documentation

- `docs/PROJECT_PLAN.md` - Original project roadmap
- `API_EXAMPLES.md` - API usage examples
- `DEPLOYMENT.md` - Deployment guides
- `CLEANUP.md` - Documentation consolidation notes
