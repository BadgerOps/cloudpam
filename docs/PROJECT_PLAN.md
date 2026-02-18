# CloudPAM Project Plan

This document captures the roadmap for CloudPAM, a cloud-native IP Address Management (IPAM) system for AWS and GCP.

---

## Current State (v0.4.1 — Sprint 18 Complete)

### Implemented Features

**Core IPAM:**
- Pool CRUD with hierarchical parent-child relationships, tags, type/status/source metadata
- CIDR validation (IPv4 only, prefix 8-32, reserved ranges blocked)
- Overlap detection within same parent scope
- Block enumeration with pagination (compute candidate subnets)
- Account management with cloud provider metadata
- CIDR search with containment queries (`/api/v1/search`)

**Storage:**
- In-memory store (default, for development/testing)
- SQLite store (`-tags sqlite`) with 13 migrations
- PostgreSQL store (`-tags postgres`) with pgx/v5
- All three implement identical `Store` interface

**Authentication & Authorization:**
- API key authentication with Argon2id hashing
- RBAC with 4 roles: admin, operator, viewer, auditor
- Local user management with password authentication
- Session management (HttpOnly + Secure cookies)
- Dual auth: session cookies (browser) + Bearer tokens (API)
- Bootstrap admin user and first-boot setup wizard

**Cloud Discovery (AWS):**
- AWS collector: VPCs, subnets, Elastic IPs
- AWS Organizations discovery with cross-account AssumeRole
- Standalone discovery agent (`cmd/cloudpam-agent/`)
- Bulk org ingest API, resource linking/unlinking
- Terraform modules and CloudFormation StackSet for IAM

**Smart Planning:**
- Gap analysis, fragmentation scoring, compliance checks
- Recommendation engine with apply/dismiss workflow
- Schema Planner wizard (4-step: Template → Strategy → Dimensions → Preview)

**AI Planning:**
- LLM provider abstraction (OpenAI/Ollama/vLLM/Azure compatible)
- Conversational planning with SSE streaming
- Plan extraction from LLM responses, plan apply

**API:**
- RESTful JSON API with OpenAPI 3.1 spec (v0.7.0)
- 30+ endpoints across pools, accounts, blocks, discovery, analysis, recommendations, AI, auth, audit

**Frontend:**
- Unified React/Vite/TypeScript SPA with 13 pages
- Dark mode with three-mode toggle
- Cmd+K search, hierarchical pool tree

**Operational:**
- Request ID middleware, structured logging with slog
- Rate limiting per IP, Prometheus metrics
- Sentry integration (errors + performance)
- Graceful shutdown, health/readiness endpoints
- Audit logging for all mutations
- Docker, Nix flake, CI/CD with GitHub Actions

### Not Yet Implemented

- GCP/Azure cloud discovery collectors
- Drift detection (discovered vs managed state)
- SSO/OIDC integration
- Multi-tenancy enforcement (schema exists, not active)
- IPv6 support
- VLAN/VRF tracking
- Host/Address tracking (deferred to M7+)

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

// FUTURE: Address/Host tracking (see "Host/Address Tracking" section)
type Address struct {
    ID           int64
    PoolID       int64      // Parent subnet
    IP           string     // Single IP address (e.g., "10.1.2.45")
    Type         string     // "private" | "public" | "elastic"
    Status       string     // "allocated" | "reserved" | "available"

    // Host information (if allocated)
    HostType     string     // "ec2" | "eni" | "rds" | "elb" | "lambda" | "gce" | etc.
    HostID       string     // Cloud resource ID (i-xxx, eni-xxx, etc.)
    HostName     string     // Name tag or resource name

    // Metadata
    ExternalID   string     // ENI ID or network interface ID
    Tags         string     // JSON tags from cloud
    LastSyncAt   *time.Time
    CreatedAt    time.Time
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

// FUTURE: Host discovery extension (see "Host/Address Tracking" section)
type HostDiscoveryProvider interface {
    Provider
    // DiscoverHosts returns all network interfaces/IPs in specified subnets
    DiscoverHosts(ctx context.Context, opts DiscoveryOptions) ([]DiscoveredHost, error)
}
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

## Architecture: Host/Address Tracking (Future)

### Overview

Host/Address tracking extends CloudPAM from subnet-level IPAM to individual IP address management. This is a **separate feature track** that builds on the subnet discovery foundation.

**Key distinction:**
- **Subnet tracking** (M1): "What CIDRs exist?" → Thousands of records, rarely change
- **Host tracking** (M7+): "What IPs are in use?" → Potentially millions of records, frequent changes

### Data Available from Cloud APIs

#### AWS: `ec2:DescribeNetworkInterfaces`

| Data | Description | Use Case |
|------|-------------|----------|
| Private IPs | Primary + secondary IPs per ENI | Utilization tracking |
| Elastic IPs | Associated EIPs | Public IP inventory |
| Instance ID | Attached EC2 instance | "What's using this IP?" |
| Interface type | `ec2`, `lambda`, `nat_gateway`, `efa`, etc. | Resource categorization |
| Security groups | Attached SGs | Security analysis |
| Availability zone | ENI placement | Capacity by AZ |
| Status | `in-use`, `available` | Orphan detection |

**Additional AWS permissions required:**
```json
{
  "Action": [
    "ec2:DescribeNetworkInterfaces",
    "ec2:DescribeInstances",
    "ec2:DescribeAddresses",
    "rds:DescribeDBInstances",
    "elasticloadbalancing:DescribeLoadBalancers"
  ],
  "Resource": "*"
}
```

#### GCP: `compute.instances.list` + `compute.addresses.list`

| Data | Description | Use Case |
|------|-------------|----------|
| Internal IPs | Per network interface | Utilization tracking |
| External IPs | Static + ephemeral | Public IP inventory |
| Instance name | VM identifier | "What's using this IP?" |
| Machine type | `n2-standard-4`, etc. | Capacity planning |
| Network tags | Firewall groupings | Security analysis |
| Zone | Instance placement | Capacity by zone |
| Status | `RUNNING`, `TERMINATED`, etc. | State tracking |

**Additional GCP permissions required:**
```yaml
includedPermissions:
  - compute.instances.list
  - compute.instances.get
  - compute.addresses.list
  - compute.addresses.get
  - compute.forwardingRules.list
```

### Why Separate Feature Track?

| Factor | Subnet Discovery | Host Discovery |
|--------|-----------------|----------------|
| **Scale** | ~1K-10K records | ~100K-1M records |
| **Change rate** | Days/weeks | Hours/minutes |
| **Permissions** | 3-5 per cloud | 10-15 per cloud |
| **Storage** | KB-MB | MB-GB |
| **Sync frequency** | Daily | Hourly or real-time |
| **Query patterns** | Hierarchical browse | Search/lookup |
| **Complexity** | Moderate | High (many resource types) |

### Value Proposition

1. **Subnet utilization** - "10.1.2.0/24 is 78% full (198/254 IPs used)"
2. **Capacity forecasting** - "At current growth rate, subnet exhausts in 47 days"
3. **IP lookup** - "Who owns 10.1.2.45?" → "prod-api-server-3 (i-0abc123) in aws:123456789012"
4. **Orphan detection** - ENIs/IPs allocated but not attached to running resources
5. **Compliance reporting** - "List all IPs in PCI-scope subnets with their workloads"
6. **Cost attribution** - Map IP consumption to teams/applications via tags

### Architectural Foundations (Build Now)

To enable host tracking in the future without rework, M1-M4 should include:

1. **Pool.ID as stable foreign key**
   - Addresses will reference `pool_id`
   - Pool deletion must consider address references

2. **Provider interface extensibility**
   ```go
   // Base interface for subnet discovery (M1)
   type Provider interface { ... }

   // Extended interface for host discovery (M7+)
   type HostDiscoveryProvider interface {
       Provider
       DiscoverHosts(ctx context.Context, opts DiscoveryOptions) ([]DiscoveredHost, error)
   }
   ```

3. **Collector capability flags**
   ```json
   {
     "collector_id": "aws-prod-1",
     "capabilities": ["subnets"],  // Future: ["subnets", "hosts"]
     "version": "1.0.0"
   }
   ```

4. **API namespace reservation**
   - `/api/v1/addresses` - reserved for host/IP endpoints
   - `/api/v1/pools/{id}/addresses` - addresses within a subnet

5. **Storage schema design**
   - Separate `addresses` table (not embedded in pools)
   - Indexed by `pool_id`, `ip`, `host_id`, `external_id`
   - Partitioning consideration for scale

6. **Discovery payload versioning**
   ```go
   type DiscoveryPayload struct {
       Version   string             // "1.0" = subnets only, "2.0" = + hosts
       Subnets   []DiscoveredSubnet
       Hosts     []DiscoveredHost   // Optional, v2.0+
   }
   ```

### Implementation Approach (M7+)

**Phase 1: Read-only host inventory**
- Discover ENIs/instances and store in `addresses` table
- Display in UI: pool detail → addresses tab
- Search: "find IP 10.1.2.45"

**Phase 2: Utilization metrics**
- Calculate: `pool.used_ips` / `pool.total_ips`
- Dashboard: utilization heatmap by pool
- Alerts: "Pool X is 90% full"

**Phase 3: Change tracking**
- Detect new/removed hosts between syncs
- Timeline: "10.1.2.45 allocated to i-abc123 on 2024-01-15"
- Audit: who/what created this instance?

**Phase 4: Forecasting**
- Trend analysis on utilization over time
- Predict exhaustion dates
- Recommend pool expansions

### Resource Type Coverage

| Provider | Resource Type | API | Priority |
|----------|--------------|-----|----------|
| AWS | EC2 instances | DescribeInstances | P1 |
| AWS | ENIs (all types) | DescribeNetworkInterfaces | P1 |
| AWS | Elastic IPs | DescribeAddresses | P1 |
| AWS | RDS instances | DescribeDBInstances | P2 |
| AWS | ELB/ALB/NLB | DescribeLoadBalancers | P2 |
| AWS | Lambda (VPC) | ListFunctions | P3 |
| AWS | ECS tasks | DescribeTasks | P3 |
| GCP | Compute instances | instances.list | P1 |
| GCP | Reserved addresses | addresses.list | P1 |
| GCP | Cloud SQL | instances.list | P2 |
| GCP | Load balancers | forwardingRules.list | P2 |
| GCP | GKE pods | (via k8s API) | P3 |

---

## Feature Gap Analysis

### Must Have (v1.0)

| Feature | Status | Priority | Notes |
|---------|--------|----------|-------|
| Import from AWS | ✅ Done | P0 | VPCs, Subnets, EIPs + AWS Organizations cross-account |
| Import from GCP | Not started | P0 | Networks and Subnetworks via Compute API |
| PostgreSQL storage | ✅ Done | P0 | Full pgx/v5 implementation with migrations |
| Search by CIDR | ✅ Done | P0 | `/api/v1/search` with `cidr_contains` and `cidr_within` |
| Utilization metrics | ✅ Done | P1 | Pool stats with allocation tracking |
| Change history | ✅ Done | P1 | Comprehensive audit logging |
| Basic authentication | ✅ Done | P1 | API keys (Argon2id), RBAC, local users, sessions |

### Should Have (v1.x)

| Feature | Status | Priority | Notes |
|---------|--------|----------|-------|
| IPAM reservations | Not started | P2 | Hold space without cloud resource |
| Tags/labels on pools | ✅ Done | P2 | JSON tags field on Pool model |
| VLAN tracking | Not started | P2 | Associate VLAN IDs with pools |
| VRF support | Not started | P2 | Overlapping IP spaces in different domains |
| Bulk import (CSV) | ✅ Done | P2 | `/api/v1/import/accounts` and `/api/v1/import/pools` |
| Webhook notifications | Not started | P2 | Alert on allocations, conflicts, drift |
| IPv6 support | Not started | P2 | Dual-stack networks |

### Nice to Have (v2.0+)

| Feature | Status | Priority | Notes |
|---------|--------|----------|-------|
| **Host/Address tracking** | Not started | P2 | Individual IP inventory from ENIs/instances (deferred M7+) |
| **Subnet utilization** | Not started | P2 | % used based on discovered hosts |
| **IP lookup** | Not started | P2 | "What's using 10.1.2.45?" |
| **Capacity forecasting** | Not started | P3 | Predict subnet exhaustion |
| Live network scanning | Not started | P3 | Discover hosts via ICMP/ARP (on-prem) |
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

### M7: Host/Address Tracking (4-6 weeks)

**Goal:** Track individual IP addresses and their associated cloud resources.

**Prerequisites:** M1 (Cloud Discovery), M2 (PostgreSQL)

**Deliverables:**
1. Address domain model and storage
   - `addresses` table with pool_id foreign key
   - Indexes for IP lookup, host_id, external_id
   - Consider partitioning for scale

2. Extended provider interface
   - `HostDiscoveryProvider` interface
   - AWS implementation: DescribeNetworkInterfaces, DescribeInstances
   - GCP implementation: instances.list, addresses.list

3. Discovery collector extension
   - `--enable-hosts` flag for collectors
   - Incremental sync (only changed hosts)
   - Configurable sync frequency

4. API endpoints
   - `GET /api/v1/addresses` - list with filters
   - `GET /api/v1/addresses/{ip}` - lookup single IP
   - `GET /api/v1/pools/{id}/addresses` - addresses in subnet
   - `GET /api/v1/pools/{id}/utilization` - usage metrics

5. UI enhancements
   - Pool detail: Addresses tab with host list
   - Global IP search in header
   - Utilization bar on pool cards

6. Utilization metrics
   - Calculate used/total per pool
   - Aggregate to parent pools
   - Dashboard utilization heatmap

**Acceptance Criteria:**
- [ ] Discover all ENIs from AWS test account
- [ ] Discover all VM network interfaces from GCP test project
- [ ] "Find IP 10.1.2.45" returns host details
- [ ] Pool shows "198/254 IPs used (78%)"
- [ ] Sync handles 10k+ addresses without timeout

**Additional Permissions Required:**

AWS:
```json
{
  "Action": [
    "ec2:DescribeNetworkInterfaces",
    "ec2:DescribeInstances"
  ],
  "Resource": "*"
}
```

GCP:
```yaml
includedPermissions:
  - compute.instances.list
  - compute.instances.get
```

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

5. **Host tracking: How frequently to sync?**
   - Option A: Same schedule as subnets (daily)
   - Option B: More frequent (hourly) for accurate utilization
   - Option C: Event-driven via CloudWatch Events / Pub/Sub
   - Recommendation: Start with hourly, evaluate event-driven for v2

6. **Host tracking: Which resource types to include?**
   - Core: EC2/GCE instances, ENIs, reserved IPs
   - Extended: RDS, ELB, Lambda, ECS, Cloud SQL, GKE
   - Recommendation: Core for M7, Extended as opt-in modules

7. **Host tracking: How to handle ephemeral IPs?**
   - Kubernetes pods, Lambda, Fargate tasks get short-lived IPs
   - Option A: Track all (high volume, noisy)
   - Option B: Track only "stable" resources (instances, ENIs, reserved)
   - Option C: Configurable per resource type
   - Recommendation: Option B for M7, Option C for later

8. **Host tracking: Storage strategy for scale?**
   - Could reach millions of address records
   - Option A: Single `addresses` table with indexes
   - Option B: Partitioned by account or pool
   - Option C: Time-series DB for historical data
   - Recommendation: Option A with archival for historical (>90 days)

---

## References

**Cloud APIs:**
- [AWS EC2 IAM Permissions](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/iam-policies-ec2-console.html)
- [AWS DescribeNetworkInterfaces](https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_DescribeNetworkInterfaces.html)
- [AWS Elastic Network Interfaces](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html)
- [GCP Compute IAM Roles](https://cloud.google.com/compute/docs/access/iam)
- [GCP View Network Properties](https://cloud.google.com/compute/docs/instances/view-network-properties)
- [GCP IP Addresses](https://cloud.google.com/compute/docs/ip-addresses)

**Database & Sync:**
- [PostgreSQL Bi-Directional Replication](https://severalnines.com/blog/postgresql-bi-directional-logical-replication-deep-dive/)

**IPAM Tools:**
- [NetBox IPAM Features](https://netbox.readthedocs.io/en/stable/features/ipam/)
- [phpIPAM Documentation](https://phpipam.net/documents/)
- [NetBox vs phpIPAM Comparison](https://www.saashub.com/compare-netbox-vs-phpipam)
