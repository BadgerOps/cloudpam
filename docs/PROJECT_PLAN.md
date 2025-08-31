# CloudPAM: Cloud-Native IPAM (AWS, GCP, Extensible)

## Vision

A lightweight, cloud‑native IP Address Management (IPAM) service that centrally plans, allocates, and audits IP space across AWS and GCP, with a clean provider interface to add more clouds later. Backend in Go, frontend in Alpine.js, and SQLite for storage (embeddable, simple ops, optional migration to external DB later).

## Core Requirements

- Multi-cloud: manage address pools, prefixes, and allocations across AWS and GCP.
- Extensible providers: add Azure/On‑prem by implementing a clear interface.
- IP plan & allocation: hierarchical pools, reservations, automatic CIDR/IP allocation, release, and GC.
- Reconciliation: detect drift vs. cloud state; idempotent controllers to converge to desired state.
- Safety & policy: prevent overlaps, enforce ranges/tenancy/ownership, approvals where needed.
- Auditability: change history, who/what/when, event log of allocations and syncs.
- Simple UI: fast CRUD for pools/allocations, insights, and drift reports (Alpine.js).
- Deployable: single container, runs locally, or in Kubernetes; config via env.

## High-Level Architecture

- API server (Go): REST/JSON and server-rendered HTML for the Alpine.js UI (or static assets).
- Allocator engine: pure Go library for CIDR/IP selection with a prefix tree.
- Provider layer: `Provider` interface with concrete implementations for AWS and GCP.
- Controller loop: reconciliation workers that poll/provider‑event and converge desired vs. observed.
- Storage: SQLite with migrations; repository layer isolates SQL from domain logic.
- Background jobs: compaction, GC of expired leases/reservations, sync scheduling, metrics.
- Observability: structured logs, Prometheus metrics, OpenTelemetry traces (optional).

## Backend (Go)

- Version: Go 1.22+; use `net/netip` for IPv4/IPv6 types; consider `go4.org/netipx` for prefix ops.
- Project layout:
  - `cmd/cloudpam/` main server
  - `internal/domain/` entities, services (allocator, policy)
  - `internal/providers/{aws,gcp}/` cloud providers
  - `internal/recon/` controllers & schedulers
  - `internal/storage/` repositories, migrations
  - `internal/http/` handlers, router, middlewares
  - `web/` static assets, templates (Alpine.js)
- Dependencies: prefer stdlib; use `chi` or `gorilla/mux` for routing; `zerolog` or `zap` for logs; `goose` or `golang-migrate` for migrations; `modernc.org/sqlite` for CGO‑less builds (or `mattn/go-sqlite3` with CGO).

## Frontend (Alpine.js)

- Minimal HTML templates rendered by Go, Alpine.js sprinkles for interactivity.
- Pages: Dashboard, Pools & Prefixes, Allocations, Providers/Accounts, Drift, Audit log.
- API consumption: JSON endpoints; use fetch; avoid heavy build steps if possible.
- Styling: Basic CSS or Tailwind (optional); keep bundle small.

## Data Model (initial)

- `providers` (id, type: aws|gcp|custom, name, credentials_ref, created_at)
- `accounts` (id, provider_id, ext_id [AWS account ID / GCP project], name, labels)
- `pools` (id, name, parent_pool_id NULLABLE, ip_version, cidr, scope [global|account], owner, labels)
- `prefixes` (id, pool_id, cidr, status [free|reserved|allocated|blocked], source [desired|observed], account_id NULLABLE)
- `allocations` (id, pool_id, account_id, cidr_or_ip, kind [subnet|ip], owner, purpose, ttl NULLABLE, created_at, deleted_at NULLABLE)
- `reservations` (id, pool_id, cidr_or_ip, owner, reason, expires_at NULLABLE)
- `sync_state` (id, provider_id, account_id, resource_kind, checkpoint, updated_at)
- `events` (id, ts, actor, action, object_kind, object_id, data JSON)
- `users`/`roles` (optional; if OIDC/JWT, map claims to roles)

Notes:
- Separate desired vs observed state for prefixes to support reconciliation and drift reports.
- Store IPs/CIDRs using canonical strings and/or integer ranges for efficient queries.

## Allocation & Policy

- Allocation strategies: smallest‑fit, largest‑fit, or policy‑driven; avoid overlaps via prefix tree.
- Support VRF/Scopes: isolate pools per tenant or network scope.
- Reservations: manual holds or automatic during provisioning workflows.
- Reuse: release and defragmentation strategies; soft‑delete allocations prior to GC.

## Provider Abstraction

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
