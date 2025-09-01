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

Define interface (conceptual):

```go
type Provider interface {
    Name() string
    Validate(ctx context.Context) error
    // Discovery
    ListVPCs(ctx context.Context, account AccountRef) ([]VPC, error)
    ListSubnets(ctx context.Context, vpc VPCRef) ([]Subnet, error)
    // Actuation
    CreateSubnet(ctx context.Context, vpc VPCRef, cidr netip.Prefix, tags Tags) (Subnet, error)
    DeleteSubnet(ctx context.Context, subnet SubnetRef) error
    // Credentials & auth are provided via env/metadata (IRSA/Workload Identity) or static.
}
```

Implementation notes:
- AWS: Integrate with EC2/VPC APIs; optionally read AWS VPC IPAM state for awareness.
- GCP: Use Compute Engine networks/subnetworks; region awareness and secondary ranges.
- Rate limits & retries: exponential backoff with jitter; idempotency keys.

## Reconciliation Loops

- Controllers compare desired state (DB) with observed cloud state.
- Sources: periodic polling + event subscriptions (CloudWatch Events/EventBridge, GCP Pub/Sub) where practical; fallback to polling if events not available.
- Idempotent operations; record checkpoints to `sync_state` for incremental scans.
- Conflict handling: mark drift items, create remediation tasks; never delete unmanaged resources unless explicitly permitted by policy.

## API Design

- REST/JSON with OpenAPI spec; versioned base path `/api/v1`.
- Key endpoints: pools, prefixes, allocations, reservations, providers, accounts, recon status, events.
- Webhooks: emit allocation/release events for integration.
- AuthN/AuthZ: JWT/OIDC (e.g., with an OIDC provider); RBAC roles for read/write/admin.

## Security & Secrets

- Credentials via environment/metadata: AWS IAM Role (IRSA) and GCP Workload Identity when running in K8s; static keys only for local/dev.
- SQLite encryption if needed: consider `sqlcipher` or rely on disk encryption.
- Input validation and strong typing (`netip`), least privilege policies for cloud roles.
- Audit log for all mutating operations.

## Deployment

- Single Docker image; multi-stage build. Healthz/readyz endpoints.
- Kubernetes: Deployment, Service, ConfigMap/Secret, ServiceAccount with IRSA/Workload Identity; optional Helm chart.
- Local dev: `docker-compose` or just `go run`; SQLite file persisted to volume.

## Observability

- Logs: structured with request IDs and actor info.
- Metrics: Prometheus counters/histograms (allocations, failures, cloud API latency, recon lag).
- Tracing: OpenTelemetry optional; spans around provider calls and allocators.

## Testing Strategy

- Unit tests: allocator/prefix tree, policy, repository methods.
- Property‑based tests: CIDR allocations (no overlap, coverage of edge cases).
- Contract tests: provider interface with fakes/mocks.
- E2E (optional): against emulators (LocalStack for basic AWS VPC; limited), or sandbox accounts with CI tags; keep opt‑in.

## Initial Milestones

1) Bootstrap repo
   - Go module, basic `cmd/cloudpam` server, health endpoints
   - SQLite setup with migrations, repository scaffolding
   - Minimal web UI shell with Alpine.js

2) Core domain & allocator
   - Entities: Pool/Prefix/Allocation/Reservation
   - Prefix tree allocator with smallest‑fit strategy
   - REST endpoints for pools and allocations

3) Provider interface
   - Define `Provider` interface and fakes
   - Wire repositories + allocator to provider actions

4) AWS provider (read‑only → write)
   - Discover VPCs/Subnets and import observed state
   - Create/Delete subnet operations with tags
   - Reconciliation loop for subnets

5) GCP provider (read‑only → write)
   - Discover networks/subnetworks
   - Create/Delete subnetwork and secondary ranges
   - Reconciliation loop

6) Policies & safety
   - Overlap detection, scopes/VRFs, reservations
   - Approvals/confirmation for destructive ops

7) UI build‑out
   - Pools, allocations, drift, events
   - Inline create/release flows with Alpine.js

8) Packaging & ops
   - Dockerfile, Helm chart, example configs
   - Metrics, dashboards, runbooks

## Risks & Mitigations

- Cloud API rate limits: batch operations, backoff, controller concurrency limits.
- Drift and unmanaged resources: strict policies; default to safe/observe‑only.
- SQLite concurrency: use WAL mode; connection limits; consider move to Postgres if scale requires.
- IPv6 complexity: build with `netip` types from the start; test dual‑stack.

## Open Questions

- Should we integrate with AWS VPC IPAM as a source of truth or remain independent and reconcile only subnets? (Phase 2 decision.)
- Do we need IP‑per‑pod or IPAM for Kubernetes clusters? If yes, consider CNI integrations later.
- Multi‑tenancy boundaries: single DB with scoped RBAC vs. separate instances.

---

## Suggested Next Steps (Week 1)

- Initialize Go module and basic HTTP server skeleton.
- Add SQLite migrations for `pools`, `prefixes`, `allocations`, `events`.
- Implement minimal allocator (in‑memory) and wire to endpoints.
- Build a simple Alpine.js page to create/list pools and allocations.

## Next Options (UI, Analytics, Ops)

- Analytics Slices & Filters
  - Add filters for Account ID/Name, Platform, Environment/Tier (SDLC: dev, stg, sbx, prd), Regions (multi‑select), and CIDR search/containment.
  - Plumb account metadata into API rows (platform, environment/tier, regions) and CSV export.
  - Support CIDR containment filter (e.g., “within 10.0.0.0/16”), not just substring.
- Charts & Visualization
  - Add ApexCharts (or Chart.js) dashboards in Analytics: blocks by parent pool (bar), address space by account (donut), creations over time (line).
  - Color by account/platform and provide a legend; click‑through to filtered table.
  - Pool view: compact IP space bar showing used blocks regardless of size; hover tooltips; filters by account and search.
- Usability
  - Show “why unavailable” tooltips when a block cannot be created (overlap with sibling).
  - Toggle to hide unavailable/used blocks; status column filterable.
- Overlap & Safety
  - Enforce partial‑overlap protection across siblings (same parent) in create path.
  - Future: IPv6 overlap and visualization support.
- Accounts Metadata
  - Extend accounts with optional fields: `platform`, `tier`, `environment`, and `regions` (multi).
  - UI to create/edit these fields; validate tier enum (dev, stg, sbx, prd) and region formats.
- Migrations & Versioning (proposed direction)
  - Keep forward‑only, file‑based migrations with `schema_migrations(version, name, applied_at)`.
  - Embed migrations with `go:embed` to avoid path issues in packaged binaries.
  - Add `schema_info` table tracking `schema_version`, `app_version`, `min_supported_schema`, `applied_at`.
  - On startup: run pending migrations; validate `schema_version >= min_supported_schema`.
  - Provide `cloudpam migrate {status|up|stamp}` and a release checklist for DB changes.
