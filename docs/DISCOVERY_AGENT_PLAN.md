# Discovery Agent Separation Plan

## Goal

Extract the discovery subsystem from the monolithic CloudPAM binary into a standalone **discovery agent** that can be deployed independently — close to cloud credentials, in different regions, or scaled horizontally — while the main CloudPAM instance remains the single source of truth for IPAM data.

## Why Separate?

| Problem | Solved By |
|---------|-----------|
| AWS credentials must be present on the CloudPAM server | Agent runs where credentials live (EC2, ECS, Kubernetes with IRSA) |
| Single-region discovery bottleneck | Deploy one agent per region or per account |
| Long-running syncs block the API server | Agent runs syncs independently on its own schedule |
| Cloud SDK dependencies bloat the main binary | Agent binary includes cloud SDKs; main binary stays lean |
| Multi-cloud support adds complexity | Each agent handles one provider; plug-and-play |

## Architecture

```
                       ┌──────────────────────┐
                       │   CloudPAM Server     │
                       │   (IPAM + UI + API)   │
                       │                       │
                       │  POST /api/v1/        │
                       │    discovery/ingest    │◄─────────┐
                       │  GET  /api/v1/        │          │
                       │    discovery/resources │          │
                       │                       │          │ HTTPS + API key
                       │  Pool/Account CRUD    │          │
                       │  Discovery Store      │          │
                       └──────────────────────┘          │
                                                          │
               ┌──────────────────────────────────────────┤
               │                    │                     │
       ┌───────┴───────┐   ┌───────┴───────┐   ┌────────┴──────┐
       │ Discovery Agent│   │ Discovery Agent│   │ Discovery Agent│
       │  (AWS us-east) │   │  (AWS eu-west) │   │   (GCP)       │
       │               │   │               │   │               │
       │ Collector      │   │ Collector      │   │ Collector      │
       │ Scheduler      │   │ Scheduler      │   │ Scheduler      │
       │ Result Pusher  │   │ Result Pusher  │   │ Result Pusher  │
       └───────────────┘   └───────────────┘   └───────────────┘
```

### Communication Model

**Push-based**: the agent discovers resources and pushes results to the CloudPAM server via a new **ingest API**. This is simpler than pull-based and works through firewalls/NAT since the agent initiates all connections outward.

```
Agent                                     Server
  │                                          │
  ├─── POST /api/v1/discovery/ingest ───────►│
  │    {account_id, resources[], agent_id}   │
  │                                          │
  │◄── 200 {job_id, created, updated, stale} │
  │                                          │
  ├─── POST /api/v1/discovery/heartbeat ────►│  (periodic)
  │    {agent_id, status, last_sync}         │
  │                                          │
```

**Authentication**: agents authenticate to the server using a **scoped API key** with only `discovery:create` and `discovery:read` permissions. No admin access, no pool/account mutation.

### What Stays on the Server

- Discovery store (database tables for `discovered_resources`, `sync_jobs`)
- Discovery API for reading/listing/linking resources (existing endpoints)
- The ingest endpoint (new) that accepts bulk resource pushes
- Agent registry (new) to track connected agents and health
- UI (Discovery page, setup guide)

### What Moves to the Agent

- `internal/discovery/collector.go` — `Collector` interface, `SyncService` logic
- `internal/discovery/aws/collector.go` — AWS SDK calls
- Future GCP/Azure collectors
- Scheduling logic (cron-like sync intervals)
- Result serialization and HTTP push to server

## Detailed Design

### Phase 1: Ingest API on Server

Add a new endpoint that accepts discovery results from an external agent.

**`POST /api/v1/discovery/ingest`**

Request:
```json
{
  "agent_id": "agent-us-east-1-abc",
  "account_id": 1,
  "resources": [
    {
      "provider": "aws",
      "region": "us-east-1",
      "resource_type": "vpc",
      "resource_id": "vpc-0123456789abcdef0",
      "name": "Production VPC",
      "cidr": "10.0.0.0/16",
      "parent_resource_id": null,
      "status": "active",
      "metadata": {"state": "available", "is_default": "false"}
    }
  ]
}
```

Response:
```json
{
  "job_id": "uuid",
  "resources_found": 12,
  "resources_created": 3,
  "resources_updated": 9,
  "resources_stale": 1
}
```

The server-side ingest handler:
1. Validates the API key has `discovery:create` permission
2. Validates the `account_id` exists
3. Creates a `SyncJob` record
4. Upserts each resource (same logic as current `SyncService.Sync`)
5. Marks stale resources for this account
6. Returns the job summary

This reuses the existing `DiscoveryStore` methods — the storage layer doesn't change.

**`POST /api/v1/discovery/heartbeat`**

Request:
```json
{
  "agent_id": "agent-us-east-1-abc",
  "version": "0.6.0",
  "provider": "aws",
  "accounts": [1, 3],
  "last_sync_at": "2025-01-15T10:30:00Z",
  "status": "healthy"
}
```

The server stores agent metadata for the UI to show agent health. This is optional but useful for operations.

### Phase 2: Agent Binary

New binary at `cmd/cloudpam-agent/main.go`:

```
cmd/cloudpam-agent/
├── main.go           # CLI entrypoint, config, scheduler
├── config.go         # Agent configuration (server URL, API key, intervals)
└── pusher.go         # HTTP client that pushes results to server
```

**Configuration** (env vars or config file):

```bash
# Required
CLOUDPAM_SERVER_URL=https://cloudpam.internal:8080
CLOUDPAM_API_KEY=cpam_agent_xxx
CLOUDPAM_ACCOUNT_ID=1

# Optional
CLOUDPAM_SYNC_INTERVAL=5m          # default: 5 minutes
CLOUDPAM_AGENT_ID=agent-us-east-1  # default: auto-generated hostname-based
CLOUDPAM_PROVIDER=aws              # default: auto-detect from account
CLOUDPAM_REGIONS=us-east-1,us-west-2  # override account regions
```

**Agent lifecycle:**

```
main()
 ├── Load config
 ├── Validate server connectivity (GET /healthz)
 ├── Register with server (POST /heartbeat)
 ├── Start scheduler loop
 │   ├── Run collector.Discover(account)
 │   ├── Push results (POST /ingest)
 │   ├── Log summary
 │   └── Sleep(interval)
 └── Graceful shutdown on SIGTERM
```

The agent has **no database** — it's stateless. It discovers, pushes, and forgets. The server handles all persistence, dedup, and stale-marking.

### Phase 3: Decouple Existing In-Process Discovery

Once the ingest API exists and the agent binary works, the existing in-process `SyncService` becomes optional:

1. Keep `SyncService` in the main binary as a "local agent" for simple deployments
2. The `POST /api/v1/discovery/sync` (trigger sync) endpoint either:
   - Runs the local `SyncService` if collectors are registered (current behavior)
   - Returns 501 with a message if no local collectors (agent-only mode)
3. The agent binary replaces the local sync for production deployments

**No breaking changes** — existing users who run discovery in-process continue to work.

### Phase 4: Multi-Region & Multi-Account

The agent supports discovering multiple regions for one account:

```bash
CLOUDPAM_REGIONS=us-east-1,us-west-2,eu-west-1
```

The collector runs `DescribeVpcs`, `DescribeSubnets`, `DescribeAddresses` for each region and pushes all results in a single ingest call.

For multi-account, deploy one agent per account (or one agent with multiple account configs via a YAML config file):

```yaml
server:
  url: https://cloudpam.internal:8080
  api_key: cpam_agent_xxx

agents:
  - account_id: 1
    provider: aws
    regions: [us-east-1, us-west-2]
    interval: 5m
  - account_id: 2
    provider: aws
    regions: [eu-west-1]
    interval: 10m
```

## Breaking Down the Couplings

Current tight couplings that need resolution:

| Coupling | Current | After Separation |
|----------|---------|-----------------|
| `d.srv.store.GetAccount()` in triggerSync | Discovery handler reads main store | Ingest endpoint validates account_id directly |
| `d.srv.store.GetPool()` in handleLink | Link handler validates pool | Stays on server — linking is a server-side action |
| `d.srv.writeErr()` | Discovery uses server error helpers | Ingest handler uses same helpers (it's a server endpoint) |
| `awscollector.New()` in main.go | Hardwired in monolith | Moved to agent binary; server has no cloud SDKs |
| Auth middleware on discovery routes | RBAC wraps all handlers | Ingest uses API key auth; existing read endpoints keep RBAC |

## Migration Strategy

This is **additive** — no existing functionality is removed.

```
Sprint 14-15:
  [Issue 1] Add ingest API endpoint on server
  [Issue 2] Add agent registry + heartbeat
  [Issue 3] Create agent binary with scheduler
  [Issue 4] Agent Dockerfile + Helm chart

Sprint 16:
  [Issue 5] Multi-region support in agent
  [Issue 6] Agent config file (multi-account YAML)
  [Issue 7] UI: agent health dashboard
```

## Security Considerations

- **Scoped API keys**: agents get a key with only `discovery:create` and `discovery:read`. They cannot modify pools, accounts, or users.
- **TLS**: agent-to-server communication must be over HTTPS in production.
- **Input validation**: the ingest endpoint validates all resource fields (CIDR format, resource type enum, string lengths) before storage.
- **Rate limiting**: ingest calls are rate-limited to prevent accidental floods from misconfigured agents.
- **No cloud credentials on server**: the whole point — credentials stay with the agent.

## Go Module Structure

The agent shares domain types but not the full server:

```
cloudpam/
├── internal/domain/         # Shared: DiscoveredResource, SyncJob types
├── internal/discovery/      # Shared: Collector interface
├── internal/discovery/aws/  # Agent-only: AWS collector
├── cmd/cloudpam/            # Server binary (no cloud SDKs)
└── cmd/cloudpam-agent/      # Agent binary (no database, no HTTP server)
```

Both binaries live in the same Go module. The agent imports `internal/domain` and `internal/discovery` but NOT `internal/storage`, `internal/api`, or `internal/auth`.

## Testing Strategy

- **Server ingest endpoint**: standard `httptest` tests with mock data
- **Agent pusher**: test against a mock HTTP server
- **Integration**: start both server and agent in `TestMain`, run a sync cycle, verify resources appear in server store
- **E2E with LocalStack**: agent discovers from LocalStack EC2 mock, pushes to server
