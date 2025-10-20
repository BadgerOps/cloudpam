# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

CloudPAM is a lightweight, cloud-native IP Address Management (IPAM) system for AWS and GCP with an extensible provider model. It features a Go backend, Alpine.js frontend, and supports both in-memory and SQLite storage backends.

## Build System & Commands

The project uses [Just](https://github.com/casey/just) as its command runner. Install: `cargo install just` or see installation options at https://github.com/casey/just#installation.

### Development
```bash
just dev              # Run server on :8080 (in-memory store)
go run ./cmd/cloudpam # Direct Go command (alternative)
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

### Storage Layer Architecture

The storage layer uses build tags to switch between implementations:

- **Build Tags**: The binary selects storage at compile time via Go build tags
  - Without `-tags sqlite`: uses in-memory store (`cmd/cloudpam/store_default.go`)
  - With `-tags sqlite`: uses SQLite store (`cmd/cloudpam/store_sqlite.go`)

- **Storage Interface**: `internal/storage/store.go` defines the `Store` interface
  - `MemoryStore`: in-memory implementation in same file
  - SQLite implementation: `internal/storage/sqlite/sqlite.go`

- **Migration System**: SQLite builds embed SQL migrations from `migrations/` directory
  - Migrations apply automatically on startup
  - Forward-only; no rollback support
  - Use `./cloudpam -migrate status` to check schema version

### HTTP Layer

`internal/http/server.go` implements the REST API and serves the embedded UI:

- **Server struct**: wraps `http.ServeMux` and `storage.Store`
- **Route registration**: `RegisterRoutes()` sets up all endpoints
- **API endpoints**:
  - `/api/v1/pools` - pool CRUD
  - `/api/v1/pools/{id}/blocks` - enumerate candidate subnets for a pool
  - `/api/v1/accounts` - account CRUD
  - `/api/v1/blocks` - list assigned blocks (sub-pools with filters)
  - `/api/v1/export` - data export as CSV in ZIP
  - `/openapi.yaml` - OpenAPI spec served from embedded `docs/spec_embed.go`
  - `/` - serves embedded UI from `web/embed.go`
- **Middleware**: `LoggingMiddleware` logs requests
- **Error handling**: uses `apiError` struct with `error` and `detail` fields

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
- Calls `selectStore()` to get storage implementation (defined in build-tag files)
- Sets up HTTP server with timeouts
- Handles migration CLI commands before starting server

## Development Guidelines

### Code Style

- Go 1.24+ required
- Use `go fmt` and pass `golangci-lint` (see `.golangci.yml` for enabled linters)
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

- Single-page UI uses Alpine.js
- Static assets embedded at build time via `web/embed.go`
- UI is served at `/` by `handleIndex()`
- For UI changes, update screenshots with `npm run screenshots` (requires app running at `http://localhost:8080`)

## Environment Variables

- `ADDR` or `PORT`: listen address (default `:8080`)
- `SQLITE_DSN`: SQLite connection string (default `file:cloudpam.db?cache=shared&_fk=1`)
- `APP_VERSION`: optional version stamp for migrations and Sentry release tracking
- `SENTRY_DSN`: Sentry DSN for backend error tracking (optional)
- `SENTRY_FRONTEND_DSN`: Sentry DSN for frontend error tracking (optional, can be different from backend DSN)
- `SENTRY_ENVIRONMENT`: Sentry environment name (default: `production`)

## API Contract

The REST API contract is captured in `docs/openapi.yaml` (OpenAPI 3.1). The spec is also served at `/openapi.yaml` when the server is running.

Common workflows:
- Health check: `GET /healthz`
- List pools: `GET /api/v1/pools`
- Create pool: `POST /api/v1/pools` with JSON body `{"name":"...", "cidr":"...", "parent_id":..., "account_id":...}`
- Enumerate blocks: `GET /api/v1/pools/{id}/blocks?new_prefix_len=26&page_size=50&page=1`
- List assigned blocks: `GET /api/v1/blocks?accounts=1,2&pools=10,11&page_size=50&page=1`

## Testing Across Storage Backends

CloudPAM's architecture allows the same test suite to run against both storage implementations:

1. Run without SQLite: `just test` (tests use in-memory store)
2. Run with SQLite: `just sqlite-build && just test` (tests use SQLite store if available)

When writing tests, avoid assumptions about storage persistence or specific implementation details.

## CI Configuration

- GitHub Actions: `.github/workflows/test.yml` and `.github/workflows/lint.yml`
- CI pins Go `1.24.x` and golangci-lint `v2.1.6`
- Tests run with `-race` flag in CI

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

### Notes
- If `SENTRY_DSN` is not set, Sentry integration is disabled (no overhead)
- Frontend DSN is injected into HTML at runtime via meta tag
- TracesSampleRate is set to 1.0 (100%) - adjust in `cmd/cloudpam/main.go` for high-traffic environments
- Session replay samples 10% of sessions by default - adjust in `web/index.html` if needed

## Roadmap Context

See `docs/PROJECT_PLAN.md` for future work. Key upcoming features:
- Provider abstraction and fakes
- AWS/GCP discovery and reconciliation
- Allocator service and policies (VRFs, reservations)
- AuthN/Z and audit logging
