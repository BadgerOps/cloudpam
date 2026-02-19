# Changelog

All notable changes to CloudPAM will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.8.0] - SSO/OIDC Integration Sprint 20

### Added

#### SSO/OIDC Authentication
- Generic OIDC provider integration using `coreos/go-oidc/v3` and `golang.org/x/oauth2`
- OIDC Authorization Code Flow with PKCE-ready architecture
- OIDC Discovery (`.well-known/openid-configuration`) for automatic endpoint configuration
- ID Token verification via JWKS with RSA-signed JWTs
- JIT (Just-In-Time) user provisioning from IdP claims with configurable role mapping
- Claim-to-role mapping: IdP groups mapped to CloudPAM roles using `auth.RoleLevel` for priority
- Silent session re-authentication via `prompt=none` hidden iframe with `Sec-Fetch-Dest: iframe` detection
- Client secret encryption at rest using AES-256-GCM (`internal/auth/oidc/crypto.go`)
- Local auth toggle: disable password login when OIDC is configured (break-glass admin remains)
- User active-status cache in `DualAuthMiddleware` with 30-second TTL (`sync.Map`)

#### OIDC API Endpoints
- `GET /api/v1/auth/oidc/providers` — list enabled providers (public, no auth required)
- `GET /api/v1/auth/oidc/login` — initiate OIDC login flow with state cookie
- `GET /api/v1/auth/oidc/callback` — handle IdP callback, exchange code, JIT provision user
- `POST /api/v1/auth/oidc/refresh` — get refresh redirect URL for silent re-auth
- `GET /api/v1/settings/oidc/providers` — list all providers (admin)
- `POST /api/v1/settings/oidc/providers` — create provider (admin)
- `GET /api/v1/settings/oidc/providers/{id}` — get provider with masked secret (admin)
- `PATCH /api/v1/settings/oidc/providers/{id}` — update provider (admin)
- `DELETE /api/v1/settings/oidc/providers/{id}` — delete provider (admin)
- `POST /api/v1/settings/oidc/providers/{id}/test` — test provider discovery (admin)

#### OIDC Storage & Domain
- `OIDCProvider` domain type with encrypted client secret storage
- `OIDCProviderStore` interface (7 methods: Create/Get/GetByIssuer/List/ListEnabled/Update/Delete)
- In-memory and SQLite `OIDCProviderStore` implementations
- `GetByOIDCIdentity` method added to `UserStore` interface (memory, SQLite, PostgreSQL)
- Migration `0017_oidc_providers.sql` (oidc_providers table + user OIDC columns)

#### OIDC Frontend
- SSO login buttons on login page (one per enabled provider)
- OIDC provider management UI in Config > Security settings
  - Provider list table with name, issuer URL, enabled status
  - Add/Edit/Delete provider modals with full configuration
  - Test Connection button for provider discovery validation
  - Role mapping editor (IdP group → CloudPAM role)
  - Local auth toggle with confirmation modal
- Silent session re-auth hook (`useSessionRefresh`) for OIDC users
  - Polls `/auth/me` every 60s, triggers re-auth in last 20% of session lifetime
  - Hidden iframe with `prompt=none` for seamless token refresh

#### OIDC Package (`internal/auth/oidc/`)
- `provider.go` — Provider struct wrapping go-oidc + oauth2, NewProvider/AuthCodeURL/Exchange methods
- `claims.go` — Claims struct and MapRole function for group-to-role mapping
- `crypto.go` — AES-256-GCM Encrypt/Decrypt for client secrets

#### New Environment Variables
- `CLOUDPAM_OIDC_ENCRYPTION_KEY` — 32-byte hex-encoded AES key for client secret encryption (auto-generated if not set)
- `CLOUDPAM_OIDC_CALLBACK_URL` — OIDC callback URL (default: `http://localhost:8080/api/v1/auth/oidc/callback`)

#### Dependencies
- `github.com/coreos/go-oidc/v3` v3.17.0
- `golang.org/x/oauth2` v0.35.0
- `github.com/go-jose/go-jose/v4` v4.1.3

### Changed
- `DualAuthMiddleware` now includes user active-status cache (30s TTL) to avoid per-request DB lookup
- `handleMe` response extended with `auth_provider` and `session_expires_at` fields
- CSRF middleware exempts `/api/v1/auth/oidc/*` paths (OIDC uses state parameter for security)
- User domain type extended with `AuthProvider`, `OIDCSubject`, `OIDCIssuer` fields

## [0.7.0] - Auth Hardening Sprint 19

### BREAKING
- `CLOUDPAM_AUTH_ENABLED` env var removed — authentication is always enforced
- First boot always shows setup wizard; use `CLOUDPAM_ADMIN_USERNAME`/`CLOUDPAM_ADMIN_PASSWORD` to auto-seed admin
- Password minimum length increased from 8 to 12 characters
- Bearer tokens without `cpam_` prefix no longer accepted as session auth

### Added
- Security settings API (`GET/PATCH /api/v1/settings/security`) with admin-only RBAC
- Security settings UI page under Config > Security (session, password, login, network sections)
- CSRF protection middleware (double-submit cookie pattern for session-authenticated requests)
- Per-IP login rate limiting (5 attempts/minute default) on `/api/v1/auth/login`
- Trusted proxy configuration via `CLOUDPAM_TRUSTED_PROXIES` env var
- Configurable session duration and max concurrent sessions per user (default: 24h, 10 sessions)
- `POST /api/v1/auth/users/{id}/revoke-sessions` endpoint (admin or self-service)
- `ListByUserID` added to `SessionStore` interface (memory, SQLite, PostgreSQL)
- `SettingsStore` interface with memory and SQLite implementations
- Migration `0016_settings.sql` (settings table)
- `ValidatePassword()` with configurable min (12) and max (72, bcrypt limit)
- `RoleLevel()` helper for privilege comparison
- Coming soon placeholders for Roles & Permissions and SSO/OIDC in settings UI

### Fixed
- API key scope elevation: callers can no longer create keys with higher privileges than their own role
- Audit actor attribution: `logAudit()` extracts user/API key from auth context instead of hardcoding "anonymous"
- Import routes (`/api/v1/import/accounts`, `/api/v1/import/pools`) now registered in protected mode
- `X-Forwarded-For` no longer trusted blindly — only from configured trusted proxies

### Removed
- `RegisterRoutes()` (unprotected route variant)
- Bearer-as-session-token auth path (Strategy 3 in `DualAuthMiddleware`)

### Security
- CSRF token validation on all session-authenticated state-changing requests
- Login rate limiting prevents brute-force attacks
- Session eviction enforces max concurrent sessions per user
- Password max length enforced at 72 chars to prevent bcrypt truncation

## [0.6.1] - Rename internal/http to internal/api

### Changed
- Renamed `internal/http` package to `internal/api` to avoid shadowing Go's standard library `net/http` package (#42)
- Updated all import aliases (`ih`, `cloudpamhttp`) to use the clean `api` package name directly
- Updated all documentation references to reflect the new package path

## [0.6.0] - Soft-Deletes, Connection Pooling & Utilization Snapshots

### Added
- Soft-delete support for pools and accounts — records are marked with `deleted_at` instead of being permanently removed, enabling future restore/audit capabilities (#18)
- SQLite connection pool configuration via environment variables: `SQLITE_MAX_OPEN_CONNS`, `SQLITE_MAX_IDLE_CONNS`, `SQLITE_CONN_MAX_LIFETIME_SECS`, `SQLITE_CONN_MAX_IDLE_SECS` (#71)
- Utilization snapshot table (`utilization_snapshots`) for tracking pool usage over time, enabling growth projections and capacity planning (#117)
- `UtilizationStore` interface with in-memory and SQLite implementations (`RecordSnapshot`, `ListSnapshots`, `LatestSnapshot`)
- `UtilizationSnapshot` domain type capturing pool utilization at a point in time
- SQLite migrations: `0014_soft_deletes.sql` (adds `deleted_at` column + indexes), `0015_utilization_snapshots.sql` (snapshot table)

### Changed
- All pool and account queries now filter on `deleted_at IS NULL` across MemoryStore, SQLite, and PostgreSQL backends
- Delete operations (`DeletePool`, `DeleteAccount`, `DeletePoolCascade`, `DeleteAccountCascade`) now perform soft-deletes (SET `deleted_at`) instead of hard deletes
- Search queries filter out soft-deleted records in all storage backends

### Noted
- Test coverage is at 53.4% overall — below the 80% target tracked in #67; coverage improvement deferred to a dedicated sprint

## [0.5.0] - Configuration Section & User Menu

### Changed
- Sidebar nav restructured into labeled sections: IPAM, Operations, Planning, Configuration
- Sidebar "Settings" section renamed to "Configuration"; routes updated from `/settings/*` to `/config/*`
- Theme toggle moved from sidebar footer to header as a Sun/Moon icon button (simple light/dark toggle)
- User info badge and logout button removed from sidebar footer (now handled by header dropdown)

### Added
- Header user dropdown menu: shows authenticated user info (username, role badge), with links to Profile, My API Keys, and Log out
- Profile page (`/profile`): view user info (username, email, role) and change password
- Log Destinations stub page (`/config/log-destinations`): placeholder for future syslog, webhook, S3, and SIEM integrations

## [0.4.1] - First-Boot Setup & Auth Bugfixes

### Added
- First-boot admin setup wizard: fresh installs prompt for admin account creation at `/setup`
- `POST /api/v1/auth/setup` endpoint for creating the initial admin user
- `/healthz` now returns `needs_setup` boolean for fresh install detection
- `SetupPage` frontend component with username, email, password, and confirm password fields
- `ProtectedRoute` redirects to `/setup` on fresh installs before any other page loads

### Fixed
- Session cookie `Secure` flag now adapts to HTTP vs HTTPS — cookies work on `http://localhost` during development
- Session cookie `SameSite` set to `Lax` on HTTP (was `Strict`, which blocked cookie on navigation)
- `GET /api/v1/accounts` no longer returns JSON `null` on empty database (returns `[]`)
- `GET /api/v1/pools` no longer returns JSON `null` on empty database (returns `[]`)
- Frontend `useAccounts` hook guards against null API response with `?? []`

## [0.4.0] - Phase 4: AI Planning

### Added - Sprint 17: AI Planning Backend
- LLM provider abstraction (`internal/planning/llm/`) with OpenAI-compatible implementation
- Supports OpenAI, Ollama, vLLM, Azure, and any OpenAI-compatible endpoint via `CLOUDPAM_LLM_ENDPOINT`
- Conversation storage (`ConversationStore`) with in-memory, SQLite, and PostgreSQL implementations
- AI planning service (`AIPlanningService`) with context-aware system prompts (pool hierarchy, gap analysis, compliance)
- SSE streaming chat endpoint (`POST /api/v1/ai/chat`) with 5-minute write deadline
- Session CRUD endpoints (`/api/v1/ai/sessions`)
- Plan apply endpoint (`POST /api/v1/ai/sessions/{id}/apply-plan`) reusing schema apply logic
- SQLite migration `0013_conversations.sql` and PostgreSQL migration `002_conversations.up.sql`
- Environment variables: `CLOUDPAM_LLM_API_KEY`, `CLOUDPAM_LLM_MODEL`, `CLOUDPAM_LLM_ENDPOINT`, `CLOUDPAM_LLM_MAX_TOKENS`, `CLOUDPAM_LLM_TEMPERATURE`

### Added - Sprint 18: AI Planner Frontend
- AI Planner page with session sidebar and chat interface
- SSE streaming client helper (`streamPost`) for real-time response display
- Plan extraction from assistant markdown responses (`planParser.ts`)
- Apply Plan button on detected plan cards to create pools directly
- `Bot` icon nav item in sidebar
- Frontend tests for plan parser (5 tests)

## [Unreleased]

### Changed — Build Pipeline & Container Hardening

#### Container Images
- Runtime base image changed from `alpine:3.21` to `cgr.dev/chainguard/static:latest` in both server and agent Dockerfiles — zero OS packages, ~15MB images
- Removed `apk add ca-certificates tzdata curl` (ca-certs and tzdata provided by Chainguard base)
- Removed manual user creation (`addgroup`/`adduser`) — uses Chainguard built-in `nonroot` user (UID 65532)
- Removed `HEALTHCHECK` instructions (Kubernetes liveness/readiness probes handle health checks; Docker Compose users can add externally)
- Helm chart `podSecurityContext` updated: `runAsUser` and `fsGroup` changed from 1000 to 65532 to match Chainguard `nonroot` UID

#### Frontend Build Optimization
- Vite build config: explicit `sourcemap: false`, `target: 'es2020'`, `cssMinify: true`
- Rollup `manualChunks` splits vendor-react and vendor-icons into separate cacheable chunks
- Hash-based chunk/entry/asset file naming for cache busting
- Added `rollup-plugin-visualizer` devDep and `build:analyze` script for bundle analysis

#### CI/CD Hardening
- All GitHub Actions pinned to immutable commit SHAs across 6 workflow files (test, lint, container-images, release-builds, release, manual-builds)
- New `govulncheck` job in `test.yml` — scans Go dependencies for known vulnerabilities on every push/PR
- New Trivy container scan step in `container-images.yml` — scans server and agent images for HIGH/CRITICAL vulnerabilities after build (non-blocking)

### Added - Sprint 16b: AWS Organizations Discovery

#### Org-Mode Agent (`cmd/cloudpam-agent/`)
- `config.go`: `AWSOrg` config struct with `enabled`, `role_name`, `external_id`, `regions`, `exclude_accounts` — env vars `CLOUDPAM_AWS_ORG_*`
- `main.go`: `runOrgSync()` — enumerates org accounts, filters excludes, AssumeRole per member, discovers, builds `BulkIngestRequest`
- `pusher.go`: `PushOrgResources()` — pushes bulk ingest payload to server with retry/backoff

#### AWS SDK Extensions (`internal/discovery/aws/`)
- `org.go`: `ListOrgAccounts()` — enumerates active AWS Organization accounts via `organizations:ListAccounts` paginator
- `assume_role.go`: `AssumeRole()` — returns `aws.CredentialsProvider` via `stscreds.NewAssumeRoleProvider` with optional ExternalID
- `collector.go`: `NewWithCredentials()` constructor — injects cross-account credentials into the existing collector

#### Bulk Org Ingest API
- `POST /api/v1/discovery/ingest/org` — accepts `BulkIngestRequest`, auto-creates CloudPAM Account records for new AWS accounts, upserts resources per account
- `internal/domain/discovery.go`: `OrgAccountIngest`, `BulkIngestRequest`, `BulkIngestResponse` domain types
- `internal/storage/store.go`: `GetAccountByKey(ctx, key)` on Store interface — lookup accounts by unique key (e.g. `aws:123456789012`)
- `internal/storage/sqlite/sqlite.go`: SQLite `GetAccountByKey` implementation
- `internal/storage/postgres/postgres.go`: PostgreSQL `GetAccountByKey` implementation
- `migrations/0012_account_key_unique.sql`: unique index on `accounts.key`

#### Infrastructure as Code (`deploy/`)
- `deploy/terraform/aws-org-discovery/management-policy/`: Terraform module creating IAM role (EC2+ECS trust), instance profile, 3 policies (org discovery, EC2 read-only, STS identity)
- `deploy/terraform/aws-org-discovery/member-role/`: Terraform module creating cross-account discovery role with least-privilege trust policy
- `deploy/cloudformation/discovery-role-stackset.yaml`: CloudFormation StackSet template for deploying member role across all org accounts

#### Frontend — Discovery Wizard Org Mode (`ui/src/components/DiscoveryWizard.tsx`)
- Discovery mode toggle: "Single Account" vs "AWS Organization" radio cards
- Org mode fields: Role Name, External ID, Regions, Exclude Accounts
- Org-aware config generation for all deployment tabs (Shell, YAML, Terraform, Docker)
- New "IAM Setup" tab (org-mode only) with Terraform snippets for member role + management policy
- Agent connection polling: wizard polls for agent heartbeats every 5s, shows spinner while waiting, green status when connected
- `onComplete` callback navigates to Agents tab on completion

#### Dependencies
- `github.com/aws/aws-sdk-go-v2/service/organizations` v1.50.2
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds` (STS AssumeRole)

#### Documentation
- `docs/DISCOVERY.md`: AWS Organizations discovery section — architecture, agent config, Terraform modules, bulk ingest API
- `docs/CHANGELOG.md`: this entry
- `CLAUDE.md`: updated with org discovery endpoint, env vars, migration, deployment modules

## [0.3.2] - 2026-02-15

### Fixed
- Fix `go build -tags 'sqlite postgres'` compilation errors — extracted shared auth helpers (`isUniqueViolation`, `contains`, `boolToInt`, `defaultOrgID`) into build-tag-guarded helper files so each tag combination gets exactly one definition
- Added `cmd/cloudpam/store_both.go` (`//go:build sqlite && postgres`) to select storage backend at runtime via `DATABASE_URL` env var when both tags are active
- Made `store_sqlite.go` and `store_postgres.go` build tags mutually exclusive (`sqlite && !postgres` / `postgres && !sqlite`) to avoid `selectStore` redeclaration

### Added
- Agent binary (`cloudpam-agent`) now built and released alongside server for all 6 platform/arch combinations (linux/darwin/windows on amd64/arm64)
- SHA256 checksums now cover both server and agent release archives

## [0.3.1] - 2026-02-15

### Fixed
- Auto-release workflow now chains container image and binary builds via `workflow_call` instead of relying on the `release: [published]` event, which is silently ignored when created by `GITHUB_TOKEN` (GitHub Actions limitation)
- `release-builds.yml` and `container-images.yml` accept both `release: [published]` (manual releases) and `workflow_call` (auto-release), resolving the tag via `inputs.tag || github.event.release.tag_name`

## [0.3.0] - 2026-02-14

### Added - Release Infrastructure

#### Auto-Release Workflow (`.github/workflows/release.yml`)
- Triggers on push to `master` when `docs/CHANGELOG.md` changes
- Parses latest `## [x.y.z]` version from changelog, creates GitHub Release + tag if not already tagged
- Changelog body for the version is included as release notes

#### Container Image Workflow (`.github/workflows/container-images.yml`)
- Triggers on `release: [published]` (alongside existing `release-builds.yml`)
- Builds and pushes multi-platform images (`linux/amd64`, `linux/arm64`) to GHCR
- `ghcr.io/badgerops/cloudpam/server:<tag>` — server image built with `-tags 'sqlite postgres'`
- `ghcr.io/badgerops/cloudpam/agent:<tag>` — agent image with version injection via ldflags
- Uses Docker Buildx with QEMU for cross-platform builds, GHA cache for layer reuse

#### Server Helm Chart (`deploy/helm/cloudpam-server/`)
- Full Helm chart for deploying CloudPAM server to Kubernetes
- Configurable storage backend: PostgreSQL (default) or SQLite with optional PVC
- Auth bootstrap, observability (Sentry, log level/format, metrics), rate limiting via values
- Liveness (`/healthz`) and readiness (`/readyz`) probes
- Optional Ingress resource, ServiceAccount, and Secret management
- Resource defaults: 500m CPU / 512Mi memory limits

#### Dockerfile Improvements
- Server Dockerfile: build tags changed from `sqlite` to `'sqlite postgres'`, added `HEALTHCHECK` instruction
- Agent Dockerfile: added `VERSION` ARG with `-trimpath` and `-ldflags` for version injection, pinned Alpine to `3.21`

#### Agent Version Injection
- Changed `const version = "dev"` to `var version = "dev"` in `cmd/cloudpam-agent/main.go` so `-ldflags -X main.version=...` works at build time

#### Agent Helm Chart Fixes
- Fixed image repository from `ghcr.io/yourorg/cloudpam-agent` to `ghcr.io/badgerops/cloudpam/agent`
- Fixed `Chart.yaml` home URL to `https://github.com/BadgerOps/cloudpam`

#### Justfile
- Added `docker-build-agent` recipe for building agent container image
- Added `docker-build-all` recipe for building both server and agent images

### Added - Sprint 14: Analysis Engine (Phase 3 — Smart Planning)

#### Analysis Package (`internal/planning/`)
- `types.go`: request/response types — `AnalysisRequest`, `GapAnalysis`, `FragmentationAnalysis`, `ComplianceReport`, `NetworkAnalysisReport`, `AnalysisSummary`
- `cidr_helpers.go`: CIDR math utilities — `ipv4ToUint32`, `uint32ToIPv4`, `rangeToCIDRs` (range → minimal CIDR decomposition), `isRFC1918`, `prefixesOverlap`, `prefixAddressCount`
- `gaps.go`: gap analysis via interval subtraction on uint32 address space — finds unused CIDRs within a parent pool by comparing against children
- `fragmentation.go`: fragmentation scoring (0–100) with 4 weighted factors — scattered (40%), oversized (20%), undersized (20%), misaligned (20%) — plus actionable recommendations
- `compliance.go`: 5 compliance rules: `OVERLAP-001` (sibling overlap, error), `RFC1918-001` (non-private space, warning), `EMPTY-001` (empty parent pool, warning), `NAME-001`/`NAME-002` (missing name/description, info)
- `analysis.go`: `AnalysisService` orchestrator combining gaps + fragmentation + compliance into a health-scored `NetworkAnalysisReport`

#### Analysis API Endpoints
- `POST /api/v1/analysis` — full network analysis report (gap + fragmentation + compliance + health score)
- `POST /api/v1/analysis/gaps` — gap analysis for a single pool (body: `{"pool_id": 1}`)
- `POST /api/v1/analysis/fragmentation` — fragmentation scoring (body: `{"pool_ids": [1,2]}`)
- `POST /api/v1/analysis/compliance` — compliance checks (body: `{"pool_ids": [1,2], "include_children": true}`)
- RBAC: requires `pools:read` permission (reuses existing pool RBAC)

#### Tests
- `gaps_test.go`: `rangeToCIDRs` unit tests (aligned/non-aligned/edge), `findFreeRanges`, full `AnalyzeGaps` integration
- `fragmentation_test.go`: score calculation, scattered/misaligned issue detection
- `compliance_test.go`: overlap detection, RFC1918, empty pool, naming checks
- `analysis_test.go`: full `Analyze()` with realistic hierarchy, health score deduction
- `analysis_handlers_test.go`: HTTP endpoint tests (200/400/404/405 cases)

#### Documentation
- `CLAUDE.md` updated: Phase 3 status, analysis endpoints in API list, planning package status

### Added - Sprint 13: Cloud Discovery — Data Model, AWS Collector & API

#### Discovery Domain & Storage
- `internal/domain/discovery.go`: `DiscoveredResource`, `SyncJob`, `DiscoveryFilters`, status enums (active/stale/deleted, pending/running/completed/failed)
- `internal/storage/discovery.go`: `DiscoveryStore` interface with 11 methods (CRUD, upsert, link/unlink, mark stale, sync jobs)
- `internal/storage/discovery_memory.go`: in-memory `DiscoveryStore` implementation
- `internal/storage/sqlite/discovery.go`: SQLite `DiscoveryStore` with `ON CONFLICT` upserts
- `migrations/0008_discovered_resources.sql`: `discovered_resources` and `sync_jobs` tables with UUID primary keys

#### Collector Framework & AWS Collector
- `internal/discovery/collector.go`: `Collector` interface + `SyncService` orchestrator
- `internal/discovery/aws/collector.go`: AWS collector using `aws-sdk-go-v2` — discovers VPCs, subnets, and Elastic IPs via `ec2:DescribeVpcs`, `ec2:DescribeSubnets`, `ec2:DescribeAddresses`
- Sync flow: create job → discover → upsert resources → mark stale → update job with counts

#### Discovery API Endpoints
- `GET /api/v1/discovery/resources` — list discovered resources with filters (account, provider, type, status, linked)
- `GET /api/v1/discovery/resources/{id}` — get single resource
- `POST /api/v1/discovery/resources/{id}/link` — link resource to a pool
- `DELETE /api/v1/discovery/resources/{id}/link` — unlink resource from pool
- `POST /api/v1/discovery/sync` — trigger discovery sync for an account
- `GET /api/v1/discovery/sync` — list sync jobs
- `GET /api/v1/discovery/sync/{id}` — get sync job status
- RBAC: `discovery` resource with read/create/update permissions for admin, operator, viewer roles

#### Frontend — Discovery Page
- `ui/src/pages/DiscoveryPage.tsx`: replaced placeholder with full discovery UI
  - Account selector, Sync Now button, Resources tab (table with filters, link/unlink actions), Sync History tab
  - `ResourceTypeBadge` component with color-coded badges for VPC/Subnet/EIP/NIC
- `ui/src/hooks/useDiscovery.ts`: `useDiscoveryResources` and `useSyncJobs` hooks
- Discovery types added to `ui/src/api/types.ts`
- Status badge colors added for discovery/sync statuses (stale, deleted, completed, running, pending, failed)
- API client improved: detects non-JSON responses and gives clear error messages

#### In-App Setup Guide
- Discovery page shows an interactive setup guide with collapsible sections:
  - How Discovery Works (sync/review/link workflow)
  - AWS Configuration (step-by-step: account, credentials, IAM, sync)
  - What Gets Discovered (VPC/Subnet/EIP cards with field details)
  - Linking Resources to Pools (link/unlink instructions)
- Guide auto-opens when no resources are discovered; accessible anytime via book icon in header

#### Documentation
- `docs/DISCOVERY.md`: comprehensive discovery documentation — architecture, AWS setup, IAM permissions, API reference with curl examples, RBAC, frontend walkthrough, how to add new providers
- `docs/DISCOVERY_AGENT_PLAN.md`: architecture plan for deploying the discovery agent as a separate binary — push-based ingest API, agent binary design, multi-region, Docker/Helm, security model
- GitHub issues #107–#112: phased implementation plan for agent separation

#### OpenAPI & Docs
- `docs/openapi.yaml` bumped to v0.6.0 with Discovery tag, 7 new endpoints, and schemas
- `CLAUDE.md` updated for Phase 2 state: PostgreSQL support, auth env vars, all new API endpoints, discovery/auth/cidr packages
- New dependencies: `aws-sdk-go-v2` (config, credentials, ec2 service)

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

- Raise `internal/api` test coverage from 60% to 80.6% (#67, #32, #33):
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
- Split `internal/api/server.go` (2277 lines) into 7 focused handler files (#68):
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

[0.8.0]: https://github.com/BadgerOps/cloudpam/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/BadgerOps/cloudpam/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/BadgerOps/cloudpam/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/BadgerOps/cloudpam/compare/v0.5.0...v0.6.0
[Unreleased]: https://github.com/BadgerOps/cloudpam/compare/v0.3.2...HEAD
[0.3.2]: https://github.com/BadgerOps/cloudpam/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/BadgerOps/cloudpam/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/BadgerOps/cloudpam/releases/tag/v0.3.0
[0.2.0]: https://github.com/BadgerOps/cloudpam/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/BadgerOps/cloudpam/releases/tag/v0.1.0
