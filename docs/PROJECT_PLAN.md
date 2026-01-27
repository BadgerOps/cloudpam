# CloudPAM Project Plan

This document captures the roadmap for CloudPAM, a cloud-native IP Address Management (IPAM) system for AWS and GCP.

---

## Current State (v0.1)

### Implemented Features

**Core IPAM:**
- Pool CRUD with hierarchical parent-child relationships
- CIDR validation (IPv4 only, prefix 8-32, reserved ranges blocked)
- Overlap detection within same parent scope
- Block enumeration with pagination (compute candidate subnets)
- Account management with cloud provider metadata

**Storage:**
- In-memory store (default, for development/testing)
- SQLite store (`-tags sqlite`) with migrations
- Both implement identical `Store` interface

**API:**
- RESTful JSON API with OpenAPI 3.1 spec
- Pools: CRUD, cascade delete, block enumeration
- Accounts: CRUD with metadata (platform, tier, environment, regions)
- Blocks: List assigned sub-pools with filters
- Export: CSV in ZIP format

**Operational:**
- Request ID middleware
- Structured logging with slog
- Rate limiting per IP
- Sentry integration (errors + performance)
- Graceful shutdown
- Embedded Alpine.js UI

### Not Yet Implemented

- Cloud provider integration (AWS/GCP API calls)
- Discovery of existing cloud resources
- Multi-instance synchronization
- Authentication and authorization
- Audit logging
- IPv6 support
- VLAN/VRF tracking

---

## Architecture: Cloud Discovery

### Design Goals

1. **Minimal permissions** - Read-only access to network resources only
2. **Multi-cloud support** - AWS and GCP from day one
3. **Decoupled collectors** - Can run in each cloud independently
4. **Non-destructive** - Discovery only; no cloud mutations initially

### Deployment Model

```
┌─────────────────────────────────────────────────────────────────┐
│                     Central CloudPAM Server                      │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │   REST API  │  │  PostgreSQL │  │  Reconciliation Engine  │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
         ▲                                    ▲
         │ HTTPS POST /api/v1/discovery       │
         │                                    │
    ┌────┴────┐                          ┌────┴────┐
    │   AWS   │                          │   GCP   │
    │Collector│                          │Collector│
    └─────────┘                          └─────────┘
    (Lambda or ECS)                    (Cloud Run or GKE)
```

**Central Server:**
- Runs anywhere (cloud, on-prem, laptop)
- PostgreSQL for durability and concurrent access
- Receives discovery data from collectors
- All planning and management UI

**Collectors:**
- Lightweight, stateless binaries
- Run in each cloud environment
- Discover VPCs/Subnets using native IAM
- POST results to central server
- Can be scheduled (cron, Cloud Scheduler, EventBridge)

### AWS Discovery: Minimal Permissions

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Sid": "CloudPAMReadOnlyNetworkDiscovery",
    "Effect": "Allow",
    "Action": [
      "ec2:DescribeVpcs",
      "ec2:DescribeSubnets",
      "ec2:DescribeVpcAttribute"
    ],
    "Resource": "*"
  }]
}
```

**Notes:**
- `ec2:Describe*` actions do not support resource-level permissions
- For multi-account: add `sts:AssumeRole` and create role in each member account
- Use IRSA (IAM Roles for Service Accounts) when running on EKS
- Alternatively, use AWS Organizations with delegated admin

**Cross-account discovery (optional):**
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AssumeRoleInMemberAccounts",
      "Effect": "Allow",
      "Action": "sts:AssumeRole",
      "Resource": "arn:aws:iam::*:role/CloudPAMDiscoveryRole"
    },
    {
      "Sid": "ListOrganizationAccounts",
      "Effect": "Allow",
      "Action": [
        "organizations:ListAccounts",
        "organizations:DescribeAccount"
      ],
      "Resource": "*"
    }
  ]
}
```

### GCP Discovery: Minimal Permissions

**Custom role (recommended):**
```yaml
title: CloudPAM Discovery
description: Read-only access for network discovery
includedPermissions:
  - compute.networks.list
  - compute.networks.get
  - compute.subnetworks.list
  - compute.subnetworks.get
  - resourcemanager.projects.get
```

**Or use predefined role:**
- `roles/compute.networkViewer` (includes more than needed but simpler)

**Cross-project discovery:**
- Grant role at folder or organization level
- Use Workload Identity when running on GKE/Cloud Run
- Or create service account with above permissions

### Discovery Data Model

```go
// Discovered resources from cloud APIs
type DiscoveredVPC struct {
    Provider    string            // "aws" or "gcp"
    AccountID   string            // AWS account ID or GCP project ID
    Region      string            // e.g., "us-east-1" or "us-central1"
    VPCID       string            // AWS VPC ID or GCP network name
    VPCName     string
    CIDR        string            // Primary CIDR block
    Tags        map[string]string
    DiscoveredAt time.Time
}

type DiscoveredSubnet struct {
    Provider    string
    AccountID   string
    Region      string
    VPCID       string
    SubnetID    string            // AWS subnet ID or GCP subnetwork self-link
    SubnetName  string
    CIDR        string
    AvailabilityZone string       // AWS AZ or GCP zone (if applicable)
    Tags        map[string]string
    DiscoveredAt time.Time
}

// Import request from collector
type DiscoveryPayload struct {
    CollectorID   string
    CollectorVersion string
    Timestamp     time.Time
    VPCs          []DiscoveredVPC
    Subnets       []DiscoveredSubnet
}
```

---

## Architecture: Multi-Instance Synchronization

### Option A: Single Primary with Collectors (Recommended for v1)

**Architecture:**
- One central CloudPAM server with PostgreSQL
- Collectors are stateless and push data via API
- All writes go to central server
- Simple, clear consistency model

**Pros:**
- No distributed consensus needed
- Standard PostgreSQL HA (Cloud SQL, RDS, AlloyDB)
- Collectors can be intermittently connected

**Cons:**
- Central server must be reachable from all clouds
- Single point of failure (mitigated by managed DB HA)

**Network connectivity:**
- VPN/Peering between clouds, OR
- Public endpoint with mTLS authentication, OR
- Collector pushes to object storage, server polls

### Option B: PostgreSQL Logical Replication (Future)

For scenarios requiring write access in multiple regions:

**Architecture:**
- PostgreSQL instances in each cloud
- Bi-directional logical replication (PostgreSQL 16+)
- Each instance can accept writes locally

**Challenges:**
- DDL changes must be applied manually to all nodes
- Sequence/ID collision risk (use UUIDs or regional prefixes)
- Conflict resolution for concurrent updates

**When to use:**
- Air-gapped environments
- Strict data residency requirements
- Need for local writes without central connectivity

### Option C: Event-Sourced with Message Queue (Future)

**Architecture:**
- Each instance publishes changes as events
- Central event store (Kafka, Pub/Sub, EventBridge)
- Instances consume and apply events
- Eventual consistency

**When to use:**
- Complex multi-region requirements
- Need for complete audit trail
- Integration with other event-driven systems

### Recommended Path

1. **v1.0:** Single primary server with collector agents (Option A)
2. **v2.0:** Add PostgreSQL read replicas for query scaling
3. **v3.0:** Evaluate bi-directional replication if needed

---

## Architecture: App Structure Changes

### New Package Structure

```
cloudpam/
├── cmd/
│   ├── cloudpam/           # Main server (existing)
│   └── cloudpam-collector/ # NEW: Discovery collector agent
├── internal/
│   ├── domain/             # Core types (existing)
│   │   └── types.go        # Extended with discovery fields
│   ├── http/               # HTTP server (existing)
│   │   └── server.go       # Add discovery endpoints
│   ├── storage/            # Storage interface (existing)
│   │   ├── store.go
│   │   ├── sqlite/
│   │   └── postgres/       # NEW: PostgreSQL implementation
│   ├── provider/           # NEW: Cloud provider abstraction
│   │   ├── provider.go     # Provider interface
│   │   ├── aws/            # AWS implementation
│   │   │   └── discovery.go
│   │   └── gcp/            # GCP implementation
│   │       └── discovery.go
│   ├── discovery/          # NEW: Discovery orchestration
│   │   ├── collector.go    # Runs in collector agent
│   │   └── reconciler.go   # Runs in server, merges data
│   ├── audit/              # NEW: Audit logging
│   │   └── logger.go
│   └── validation/         # Validation (existing)
└── migrations/
    ├── sqlite/             # SQLite migrations (existing)
    └── postgres/           # NEW: PostgreSQL migrations
```

### Domain Model Extensions

```go
// Extended Pool with discovery metadata
type Pool struct {
    ID          int64
    Name        string
    CIDR        string
    ParentID    *int64
    AccountID   *int64
    CreatedAt   time.Time

    // NEW: Discovery metadata
    Source      string     // "manual" | "discovered"
    ExternalID  string     // Cloud resource ID (subnet-xxx, projects/x/...)
    ExternalVPC string     // Parent VPC/Network ID
    Region      string     // Cloud region
    LastSyncAt  *time.Time // Last discovery sync
    SyncStatus  string     // "synced" | "drift" | "orphaned"
}

// NEW: Tracks discovery state per account
type DiscoveryState struct {
    ID          int64
    AccountID   int64
    LastRunAt   time.Time
    Status      string     // "success" | "failed" | "running"
    VPCCount    int
    SubnetCount int
    ErrorMsg    string
}

// NEW: Audit log entry
type AuditEntry struct {
    ID          int64
    Timestamp   time.Time
    Actor       string     // User/service making change
    Action      string     // "create" | "update" | "delete" | "import"
    EntityType  string     // "pool" | "account"
    EntityID    int64
    Changes     string     // JSON diff
    RequestID   string
}
```

### Provider Interface

```go
// internal/provider/provider.go

type Provider interface {
    // Name returns the provider identifier ("aws" or "gcp")
    Name() string

    // DiscoverVPCs returns all VPCs/Networks visible to credentials
    DiscoverVPCs(ctx context.Context, opts DiscoveryOptions) ([]DiscoveredVPC, error)

    // DiscoverSubnets returns all subnets in specified VPCs
    DiscoverSubnets(ctx context.Context, opts DiscoveryOptions) ([]DiscoveredSubnet, error)
}

type DiscoveryOptions struct {
    AccountID string   // Specific account/project (optional)
    Regions   []string // Filter to specific regions (optional)
    VPCIDs    []string // Filter to specific VPCs (optional)
}

// Registry for provider implementations
type Registry struct {
    providers map[string]Provider
}

func (r *Registry) Register(p Provider) { r.providers[p.Name()] = p }
func (r *Registry) Get(name string) (Provider, bool) { ... }
```

### New API Endpoints

```yaml
# Discovery endpoints
POST /api/v1/discovery:
  description: Receive discovery data from collector
  body: DiscoveryPayload
  response: { imported: int, updated: int, errors: [] }

GET /api/v1/discovery/status:
  description: Get discovery status per account
  response: [DiscoveryState]

POST /api/v1/discovery/trigger:
  description: Trigger on-demand discovery for an account
  body: { account_id: int }

# Audit endpoints
GET /api/v1/audit:
  description: List audit entries with filters
  params: entity_type, entity_id, actor, since, until, page, page_size
  response: { entries: [AuditEntry], total: int }

# Enhanced search
GET /api/v1/search:
  description: Search across pools, accounts, blocks
  params: q (query), type (pool|account|block), cidr_contains, cidr_within
  response: { results: [...], total: int }
```

### Storage Interface Extensions

```go
type Store interface {
    // Existing methods...

    // NEW: Discovery state
    UpsertDiscoveryState(ctx context.Context, state *DiscoveryState) error
    GetDiscoveryState(ctx context.Context, accountID int64) (*DiscoveryState, error)
    ListDiscoveryStates(ctx context.Context) ([]DiscoveryState, error)

    // NEW: Bulk operations for import
    BulkUpsertPools(ctx context.Context, pools []Pool) (created, updated int, err error)

    // NEW: Find by external ID
    GetPoolByExternalID(ctx context.Context, externalID string) (*Pool, error)

    // NEW: Audit logging
    CreateAuditEntry(ctx context.Context, entry *AuditEntry) error
    ListAuditEntries(ctx context.Context, filter AuditFilter) ([]AuditEntry, int, error)

    // NEW: Search
    SearchPools(ctx context.Context, query SearchQuery) ([]Pool, int, error)
}
```

---

## Feature Gap Analysis

### Must Have (v1.0)

| Feature | Status | Priority | Notes |
|---------|--------|----------|-------|
| Import from AWS | Not started | P0 | VPCs and Subnets via DescribeVpcs/DescribeSubnets |
| Import from GCP | Not started | P0 | Networks and Subnetworks via Compute API |
| PostgreSQL storage | Not started | P0 | Required for production; SQLite not suitable for concurrent access |
| Search by CIDR | Not started | P0 | "Find all pools containing 10.1.2.0/24" |
| Utilization metrics | Not started | P1 | % allocated per pool, rollup to parent |
| Change history | Not started | P1 | Audit log for all mutations |
| Basic authentication | Not started | P1 | API tokens for collectors and users |

### Should Have (v1.x)

| Feature | Status | Priority | Notes |
|---------|--------|----------|-------|
| IPAM reservations | Not started | P2 | Hold space without cloud resource |
| Tags/labels on pools | Not started | P2 | Arbitrary key-value metadata |
| VLAN tracking | Not started | P2 | Associate VLAN IDs with pools |
| VRF support | Not started | P2 | Overlapping IP spaces in different domains |
| Bulk import (CSV) | Not started | P2 | Seed from spreadsheet |
| Webhook notifications | Not started | P2 | Alert on allocations, conflicts, drift |
| IPv6 support | Not started | P2 | Dual-stack networks |

### Nice to Have (v2.0+)

| Feature | Status | Priority | Notes |
|---------|--------|----------|-------|
| Live network scanning | Not started | P3 | Discover actual host usage |
| Approval workflows | Not started | P3 | Require approval for production allocations |
| Terraform provider | Not started | P3 | IaC integration |
| Cloud provisioning | Not started | P3 | Create subnets via CloudPAM |
| DNS integration | Not started | P3 | Link IP allocations to DNS records |
| DHCP integration | Not started | P3 | Sync with DHCP scopes |

### Comparison with NetBox/phpIPAM

| Capability | CloudPAM | NetBox | phpIPAM |
|------------|----------|--------|---------|
| IP address tracking | Yes | Yes | Yes |
| Hierarchical pools | Yes | Yes | Yes |
| Cloud discovery | Planned | Via plugins | No |
| DCIM (racks, devices) | No | Yes | No |
| VRF support | Planned | Yes | Yes |
| Network scanning | Planned | No | Yes |
| REST API | Yes | Yes | Yes |
| Lightweight deployment | Yes | No (Django) | Moderate |

**CloudPAM differentiator:** Cloud-native, minimal footprint, built-in cloud discovery.

---

## Milestone Plan

### M1: Cloud Discovery Foundation (4-6 weeks)

**Goal:** Import existing AWS and GCP networks into CloudPAM.

**Deliverables:**
1. Provider interface and AWS implementation
   - `internal/provider/provider.go`
   - `internal/provider/aws/discovery.go`
   - Unit tests with mock AWS SDK

2. Provider interface and GCP implementation
   - `internal/provider/gcp/discovery.go`
   - Unit tests with mock GCP SDK

3. Discovery collector binary
   - `cmd/cloudpam-collector/main.go`
   - Configurable: provider, regions, API endpoint
   - Docker image for deployment

4. Discovery API endpoints
   - `POST /api/v1/discovery` - receive collector data
   - `GET /api/v1/discovery/status` - view sync status

5. Domain model extensions
   - Add discovery fields to Pool
   - Migration for new columns

6. Reconciliation logic
   - Match discovered subnets to existing pools by CIDR
   - Create new pools for unmanaged subnets
   - Mark pools as "orphaned" when cloud resource deleted

**Acceptance Criteria:**
- [ ] Collector discovers VPCs/Subnets from AWS test account
- [ ] Collector discovers Networks/Subnetworks from GCP test project
- [ ] Discovered resources appear in CloudPAM UI
- [ ] Subsequent runs update existing records (idempotent)
- [ ] IAM policies documented with minimal permissions

### M2: PostgreSQL and Production Readiness (2-3 weeks)

**Goal:** Support PostgreSQL for production deployments.

**Deliverables:**
1. PostgreSQL storage implementation
   - `internal/storage/postgres/postgres.go`
   - Build tag `-tags postgres`
   - Connection pooling with pgx

2. PostgreSQL migrations
   - `migrations/postgres/0001_init.sql`
   - `migrations/postgres/0002_discovery.sql`

3. Configuration updates
   - `DATABASE_URL` env var support
   - Auto-detect SQLite vs PostgreSQL from DSN

4. Docker Compose for local development
   - PostgreSQL service
   - CloudPAM server
   - Example collector

**Acceptance Criteria:**
- [ ] All existing tests pass with PostgreSQL backend
- [ ] Migrations apply cleanly to fresh database
- [ ] `docker-compose up` starts working system
- [ ] Performance acceptable with 10k pools

### M3: Search and Audit (2-3 weeks)

**Goal:** Find pools by CIDR and track all changes.

**Deliverables:**
1. CIDR search
   - `GET /api/v1/search?cidr_contains=10.1.2.0/24`
   - `GET /api/v1/search?cidr_within=10.0.0.0/8`
   - UI search box with auto-complete

2. Audit logging
   - Record all create/update/delete operations
   - Include actor, timestamp, before/after state
   - `GET /api/v1/audit` with filters

3. UI enhancements
   - Search bar in header
   - Audit log viewer
   - Pool detail page with change history

**Acceptance Criteria:**
- [ ] Can find "all subnets within 10.0.0.0/8"
- [ ] Can find "which pool contains 10.1.2.5"
- [ ] All API mutations logged
- [ ] Audit entries visible in UI

### M4: Authentication and Multi-tenancy (3-4 weeks)

**Goal:** Secure API access with tokens and optional OIDC.

**Deliverables:**
1. API token authentication
   - Generate tokens via CLI or API
   - Token stored hashed in database
   - Middleware validates `Authorization: Bearer <token>`

2. Role-based access control
   - Roles: `viewer`, `editor`, `admin`
   - Permissions: read pools, write pools, manage accounts, etc.

3. Collector authentication
   - Service tokens for collectors
   - Scoped to specific accounts

4. Optional OIDC integration
   - Support external identity provider
   - Map OIDC groups to roles

**Acceptance Criteria:**
- [ ] Unauthenticated requests rejected (except /healthz)
- [ ] Tokens can be scoped to read-only
- [ ] Admin can create/revoke tokens
- [ ] OIDC login flow works with test provider

### M5: Cloud Run Deployment (2 weeks)

**Goal:** One-click deployment to GCP Cloud Run.

**Deliverables:**
- Dockerfile (multi-stage, distroless)
- Terraform modules (Cloud Run, Artifact Registry, Secrets)
- GitHub Actions workflow
- Documentation

(See `docs/issues/cloud-run-deployment.md` for detailed plan)

### M6: Advanced IPAM Features (4-6 weeks)

**Goal:** Feature parity with traditional IPAM tools.

**Deliverables:**
1. Reservations
   - Reserve IP space without creating cloud resource
   - Expiration dates optional

2. Tags/Labels
   - Arbitrary metadata on pools
   - Filter and search by tags

3. Utilization reporting
   - % allocated per pool
   - Dashboard charts (allocated vs free)
   - Forecasting based on allocation rate

4. VLAN tracking
   - Associate VLAN ID with pool
   - Validate VLAN uniqueness within scope

5. VRF support
   - Allow overlapping CIDRs in different VRFs
   - Scope all operations to VRF

---

## Technical Decisions

### Why Collectors Instead of Direct API Calls?

1. **Credential isolation** - Collector runs with cloud creds, server doesn't need them
2. **Network simplicity** - Collector only needs outbound HTTPS to server
3. **Failure isolation** - Collector issues don't affect server
4. **Scalability** - Add collectors without changing server

### Why PostgreSQL for Production?

1. **Concurrent access** - SQLite has write locking issues
2. **Managed options** - Cloud SQL, RDS, AlloyDB for HA
3. **Replication** - Logical replication for multi-region future
4. **Full-text search** - Better search capabilities

### Why Not Create Cloud Resources (Initially)?

1. **Safety** - Mutations have real cost/impact
2. **Permissions** - Write access is harder to scope safely
3. **Complexity** - Error handling, rollback, drift detection
4. **MVP focus** - Discovery and planning first, provisioning later

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Cloud API rate limits | Discovery fails | Implement backoff, batch requests, respect limits |
| Credential management | Security breach | Use IAM roles/Workload Identity, no static keys |
| Data consistency | Stale/wrong data | Timestamp all records, show "last synced" in UI |
| Large scale (10k+ pools) | Performance issues | Pagination, indexes, consider read replicas |
| Schema migrations | Downtime/data loss | Forward-only migrations, backup before upgrade |

---

## Open Questions

1. **Should discovered subnets auto-create pools, or require approval?**
   - Option A: Auto-create with "discovered" source tag
   - Option B: Create "pending" records, require user approval
   - Recommendation: Option A for v1, Option B as configuration

2. **How to handle cloud resources not in CloudPAM?**
   - Option A: Ignore (show only what's in CloudPAM)
   - Option B: Show as "unmanaged" with option to import
   - Recommendation: Option B for visibility

3. **Should we support AWS VPC IPAM integration?**
   - AWS has native IPAM service
   - Could sync with it instead of raw VPC APIs
   - Deferred to v2.0 based on user demand

4. **Multi-tenant SaaS vs single-tenant?**
   - Current design assumes single-tenant
   - Multi-tenant would need org/tenant scoping
   - Deferred based on deployment model needs

---

## References

- [AWS EC2 IAM Permissions](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/iam-policies-ec2-console.html)
- [GCP Compute IAM Roles](https://cloud.google.com/compute/docs/access/iam)
- [PostgreSQL Bi-Directional Replication](https://severalnines.com/blog/postgresql-bi-directional-logical-replication-deep-dive/)
- [NetBox IPAM Features](https://netbox.readthedocs.io/en/stable/features/ipam/)
- [phpIPAM Documentation](https://phpipam.net/documents/)
