# Cloud Discovery

CloudPAM's discovery subsystem automatically finds VPCs, subnets, and Elastic IPs in your cloud accounts, then lets you link them to CloudPAM pools for unified IP address management.

## How It Works

Discovery follows an **approval workflow**: resources are discovered and stored separately from your pool hierarchy. You decide which resources to import by explicitly linking them to pools. This means discovery never modifies your existing IPAM data without your action.

```
┌──────────────┐     ┌──────────────┐     ┌──────────────────────┐
│ Cloud Account│────▶│  Collector   │────▶│ Discovered Resources │
│ (AWS/GCP/Az) │     │ (API calls)  │     │  (stored separately) │
└──────────────┘     └──────────────┘     └───────────┬──────────┘
                                                      │
                                              user action: link
                                                      │
                                                      ▼
                                          ┌──────────────────────┐
                                          │    CloudPAM Pool     │
                                          │ (your IPAM hierarchy)│
                                          └──────────────────────┘
```

### Sync Lifecycle

1. **Trigger** — you click "Sync Now" in the UI or call `POST /api/v1/discovery/sync`
2. **Discover** — the collector calls cloud APIs to enumerate resources
3. **Upsert** — new resources are created; existing resources are updated with latest data
4. **Mark stale** — resources not seen in this run are marked `stale` (they may have been deleted from the cloud)
5. **Report** — a `SyncJob` records how many resources were found, created, updated, and marked stale

### Resource Statuses

| Status | Meaning |
|--------|---------|
| `active` | Resource exists in the cloud (seen on last sync) |
| `stale` | Resource was not seen on the last sync — may have been deleted or moved |
| `deleted` | Resource confirmed deleted (reserved for future use) |

### Linking Resources to Pools

Discovered resources are **unlinked** by default. To track a cloud resource in your IPAM hierarchy:

1. Find the resource in the Discovery page
2. Click the link icon
3. Enter the pool ID you want to associate it with

The pool's CIDR should match (or contain) the resource's CIDR for the association to be meaningful. Linking is an advisory association — it doesn't modify the cloud resource.

## AWS Setup

### Prerequisites

1. **An AWS account registered in CloudPAM** — create one via `POST /api/v1/accounts` or the Accounts page:
   ```json
   {
     "key": "aws:123456789012",
     "name": "Production AWS",
     "provider": "aws",
     "regions": ["us-east-1"]
   }
   ```
   The `regions` field determines which region the collector queries. Currently the collector uses the **first** region in the list.

2. **AWS credentials** — the collector uses the standard AWS SDK credential chain. Configure one of:

   | Method | How |
   |--------|-----|
   | Environment variables | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN` |
   | Shared credentials file | `~/.aws/credentials` with a `[default]` profile |
   | EC2 instance profile | Runs on EC2 with an attached IAM role |
   | ECS task role | Runs in ECS with a task IAM role |
   | SSO / `aws sso login` | Configure with `aws configure sso` |

### Required IAM Permissions

The collector needs read-only access to EC2 networking resources:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeVpcs",
        "ec2:DescribeSubnets",
        "ec2:DescribeAddresses"
      ],
      "Resource": "*"
    }
  ]
}
```

### What Gets Discovered

| Resource Type | AWS API | Fields Captured |
|---------------|---------|-----------------|
| VPC | `ec2:DescribeVpcs` | VPC ID, CIDR block, Name tag, state, is_default |
| Subnet | `ec2:DescribeSubnets` | Subnet ID, CIDR block, Name tag, VPC ID (parent), AZ, state, available IPs |
| Elastic IP | `ec2:DescribeAddresses` | Allocation ID, public IP (/32 CIDR), Name tag, domain, instance/association |

Tags from all resources are extracted. The `Name` tag becomes the resource's display name.

## API Reference

All discovery endpoints require `account_id` and support RBAC when auth is enabled.

### Trigger a Sync

```bash
curl -X POST http://localhost:8080/api/v1/discovery/sync \
  -H 'Content-Type: application/json' \
  -d '{"account_id": 1}'
```

Returns the completed (or failed) `SyncJob`:

```json
{
  "id": "a1b2c3d4-...",
  "account_id": 1,
  "status": "completed",
  "started_at": "2025-01-15T10:30:00Z",
  "completed_at": "2025-01-15T10:30:02Z",
  "resources_found": 12,
  "resources_created": 12,
  "resources_updated": 0,
  "resources_deleted": 0,
  "created_at": "2025-01-15T10:30:00Z"
}
```

### List Discovered Resources

```bash
# All resources for account 1
curl 'http://localhost:8080/api/v1/discovery/resources?account_id=1'

# Filter by type
curl 'http://localhost:8080/api/v1/discovery/resources?account_id=1&resource_type=vpc'

# Only unlinked resources
curl 'http://localhost:8080/api/v1/discovery/resources?account_id=1&linked=false'

# Combined filters
curl 'http://localhost:8080/api/v1/discovery/resources?account_id=1&resource_type=subnet&status=active&linked=false'
```

### Link a Resource to a Pool

```bash
curl -X POST http://localhost:8080/api/v1/discovery/resources/{resource-uuid}/link \
  -H 'Content-Type: application/json' \
  -d '{"pool_id": 5}'
```

### Unlink a Resource

```bash
curl -X DELETE http://localhost:8080/api/v1/discovery/resources/{resource-uuid}/link
```

### List Sync Jobs

```bash
curl 'http://localhost:8080/api/v1/discovery/sync?account_id=1&limit=10'
```

### Get Sync Job Details

```bash
curl http://localhost:8080/api/v1/discovery/sync/{job-uuid}
```

## RBAC Permissions

When `CLOUDPAM_AUTH_ENABLED=true`, discovery endpoints require the `discovery` resource permission:

| Action | Required Permission | Roles |
|--------|-------------------|-------|
| List resources, view sync jobs | `discovery:read` | admin, operator, viewer |
| Trigger sync, link/unlink | `discovery:create` / `discovery:update` | admin, operator |

## Frontend

The Discovery page is at `/discovery` in the CloudPAM UI. It has two tabs:

- **Resources** — table of discovered cloud resources with search + filters (type, status, linked). Click the link/unlink icons in the Actions column to manage pool associations.
- **Sync History** — table of past sync jobs showing status, timing, and resource counts.

Select a cloud account from the dropdown, then click **Sync Now** to run discovery.

## Architecture

### Collector Interface

```go
type Collector interface {
    Provider() string
    Discover(ctx context.Context, account domain.Account) ([]domain.DiscoveredResource, error)
}
```

Each cloud provider implements this interface. The AWS collector is registered at startup in `cmd/cloudpam/main.go`.

### SyncService

`internal/discovery/collector.go` contains the `SyncService` which orchestrates sync runs:

1. Creates a `SyncJob` record (status=running)
2. Calls the appropriate `Collector.Discover()` based on `account.Provider`
3. Upserts each returned resource (insert or update by resource_id + account_id)
4. Marks resources not seen in this run as stale
5. Updates the `SyncJob` with final counts and status

### Storage

Discovery data lives in two tables (`migrations/0008_discovered_resources.sql`):

- `discovered_resources` — cloud resources with UUID primary keys, unique on (resource_id, account_id)
- `sync_jobs` — sync run history with UUID primary keys

The `DiscoveryStore` interface (`internal/storage/discovery.go`) defines 11 methods. Implementations exist for in-memory and SQLite backends.

## Adding a New Cloud Provider

To add GCP or Azure discovery (Sprint 14):

1. Create `internal/discovery/gcp/collector.go` (or `azure/`)
2. Implement the `Collector` interface
3. Register the collector in `cmd/cloudpam/main.go`:
   ```go
   syncService.RegisterCollector(gcpcollector.New())
   ```
4. Create accounts with `"provider": "gcp"` — the sync service auto-selects the right collector

No changes needed to the API, frontend, or storage layer — they're provider-agnostic.
