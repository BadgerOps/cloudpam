# Cloud Discovery

CloudPAM's discovery subsystem automatically finds VPCs, subnets, and Elastic IPs in your cloud accounts, then lets you link them to CloudPAM pools for unified IP address management.

## How It Works

Discovery follows an **approval workflow**: resources are discovered and stored separately from your pool hierarchy. You decide which resources to import by explicitly linking them to pools. This means discovery never modifies your existing IPAM data without your action.

There are two ways to run discovery:

| Mode | How | Best For |
|------|-----|----------|
| **Server-side sync** | Click "Sync Now" or `POST /api/v1/discovery/sync` | Quick setup, CloudPAM server has cloud credentials |
| **Standalone agent** | Deploy `cloudpam-agent` near your cloud resources | Production, multi-region, least-privilege |

```
┌──────────────┐     ┌──────────────┐     ┌──────────────────────┐
│ Cloud Account│────>│  Collector   │────>│ Discovered Resources │
│ (AWS/GCP/Az) │     │ (API calls)  │     │  (stored separately) │
└──────────────┘     └──────────────┘     └───────────┬──────────┘
                                                      │
                                              user action: link
                                                      │
                                                      v
                                          ┌──────────────────────┐
                                          │    CloudPAM Pool     │
                                          │ (your IPAM hierarchy)│
                                          └──────────────────────┘
```

### Sync Lifecycle

1. **Trigger** — you click "Sync Now" in the UI, call `POST /api/v1/discovery/sync`, or the agent runs on its schedule
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
   | EKS IRSA | EKS pod with IAM Roles for Service Accounts |
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

A ready-to-use Terraform module is available in `deploy/terraform/aws-discovery/`. See [Terraform Example](#terraform-example) below.

### What Gets Discovered

| Resource Type | AWS API | Fields Captured |
|---------------|---------|-----------------|
| VPC | `ec2:DescribeVpcs` | VPC ID, CIDR block, Name tag, state, is_default |
| Subnet | `ec2:DescribeSubnets` | Subnet ID, CIDR block, Name tag, VPC ID (parent), AZ, state, available IPs |
| Elastic IP | `ec2:DescribeAddresses` | Allocation ID, public IP (/32 CIDR), Name tag, domain, instance/association |

Tags from all resources are extracted. The `Name` tag becomes the resource's display name.

## Standalone Discovery Agent

The `cloudpam-agent` binary runs alongside your cloud resources and pushes discovered data to the CloudPAM server. This is the recommended approach for production: the agent runs with minimal IAM permissions in your cloud environment, while the CloudPAM server stays outside your cloud boundary.

### Agent Architecture

```
┌─────────────────────────────┐          ┌─────────────────────────┐
│       Your AWS Account      │          │    CloudPAM Server      │
│                             │          │                         │
│  ┌───────────────────────┐  │  HTTPS   │  POST /discovery/ingest │
│  │   cloudpam-agent      │──┼─────────>│  POST /agents/heartbeat │
│  │  - AWS SDK creds      │  │          │  POST /agents/register  │
│  │  - Bootstrap token    │  │          │                         │
│  └───────────────────────┘  │          └─────────────────────────┘
└─────────────────────────────┘
```

### Quick Start (Bootstrap Token)

The fastest way to deploy an agent is with a **provisioning bundle** — a single base64-encoded token that contains the agent's name, API key, and server URL.

**Step 1: Provision the agent** (on the CloudPAM server)

```bash
curl -X POST http://localhost:8080/api/v1/discovery/agents/provision \
  -H 'Content-Type: application/json' \
  -d '{"name": "prod-us-east-1"}'
```

Response:

```json
{
  "agent_name": "prod-us-east-1",
  "api_key": "cpk_abc123...",
  "api_key_id": "key-uuid",
  "server_url": "http://localhost:8080",
  "token": "eyJhZ2VudF9uYW1lIjoi..."
}
```

Save the `token` value — it's shown only once.

**Step 2: Deploy the agent** with just two environment variables:

```bash
CLOUDPAM_BOOTSTRAP_TOKEN=eyJhZ2VudF9uYW1lIjoi... \
CLOUDPAM_ACCOUNT_ID=1 \
./cloudpam-agent
```

The agent will:
1. Decode the token to extract `server_url`, `api_key`, and `agent_name`
2. Register itself with the server (`POST /api/v1/discovery/agents/register`)
3. Start the discovery loop (default: every 15 minutes)
4. Send heartbeats (default: every 1 minute)

### Manual Configuration (Without Token)

You can also configure the agent explicitly using environment variables or a YAML config file. In this mode the agent skips registration and starts discovery immediately.

**Environment variables:**

```bash
CLOUDPAM_SERVER_URL=https://cloudpam.example.com \
CLOUDPAM_API_KEY=cpk_abc123... \
CLOUDPAM_AGENT_NAME=prod-us-east-1 \
CLOUDPAM_ACCOUNT_ID=1 \
CLOUDPAM_AWS_REGIONS=us-east-1,us-west-2 \
./cloudpam-agent
```

**YAML config file:**

```yaml
# agent.yaml
server_url: https://cloudpam.example.com
api_key: cpk_abc123...
agent_name: prod-us-east-1
account_id: 1
aws_regions:
  - us-east-1
  - us-west-2
sync_interval: 15m
heartbeat_interval: 1m
```

```bash
./cloudpam-agent -config agent.yaml
```

### Configuration Reference

| Field | Env Var | Default | Description |
|-------|---------|---------|-------------|
| `server_url` | `CLOUDPAM_SERVER_URL` | (required) | CloudPAM server URL |
| `api_key` | `CLOUDPAM_API_KEY` | (required) | API key with `discovery:write` scope |
| `agent_name` | `CLOUDPAM_AGENT_NAME` | (required) | Human-readable agent name |
| `account_id` | `CLOUDPAM_ACCOUNT_ID` | (required) | CloudPAM account ID to discover for |
| `bootstrap_token` | `CLOUDPAM_BOOTSTRAP_TOKEN` | | Base64 provisioning bundle (replaces server_url, api_key, agent_name) |
| `aws_regions` | `CLOUDPAM_AWS_REGIONS` | SDK default | Comma-separated AWS regions |
| `sync_interval` | `CLOUDPAM_SYNC_INTERVAL` | `15m` | How often to run discovery |
| `heartbeat_interval` | `CLOUDPAM_HEARTBEAT_INTERVAL` | `1m` | How often to send heartbeats |
| `max_retries` | | `3` | Push retry attempts on server error |
| `retry_backoff` | | `5s` | Initial retry delay (exponential) |
| `request_timeout` | | `30s` | HTTP request timeout |

When `bootstrap_token` is set and `api_key` is not, the token is decoded and its fields populate `server_url`, `api_key`, and `agent_name`. If `api_key` is set explicitly, the token is ignored.

### Deploying with Docker

```bash
docker build -f deploy/docker/Dockerfile.agent -t cloudpam-agent .

docker run -e CLOUDPAM_BOOTSTRAP_TOKEN=eyJhZ2VudF9uYW1lIjoi... \
           -e CLOUDPAM_ACCOUNT_ID=1 \
           -e AWS_REGION=us-east-1 \
           cloudpam-agent
```

### Deploying with Helm (Kubernetes)

A Helm chart is available in `deploy/helm/cloudpam-agent/`:

```bash
helm install cloudpam-agent deploy/helm/cloudpam-agent/ \
  --set config.serverUrl=https://cloudpam.example.com \
  --set config.accountId=1 \
  --set config.awsRegions='{us-east-1,us-west-2}' \
  --set apiKey=cpk_abc123... \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::123456789012:role/cloudpam-discovery
```

### Agent Health & Monitoring

Agents send periodic heartbeats. Their health status is computed based on last heartbeat:

| Status | Meaning |
|--------|---------|
| `healthy` | Heartbeat received within the last 5 minutes |
| `stale` | No heartbeat for 5-15 minutes |
| `offline` | No heartbeat for over 15 minutes |

View agent status in the **Agents** tab on the Discovery page, or via:

```bash
curl http://localhost:8080/api/v1/discovery/agents
```

## Terraform Example

The `deploy/terraform/aws-discovery/` directory contains a Terraform module that creates the IAM role and policy needed for the discovery agent. It supports both EC2 instance profiles and EKS IRSA (IAM Roles for Service Accounts).

```bash
cd deploy/terraform/aws-discovery

# For EKS IRSA
terraform init
terraform apply \
  -var="oidc_provider_arn=arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE" \
  -var="oidc_provider_url=oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE" \
  -var="namespace=cloudpam" \
  -var="service_account_name=cloudpam-agent"

# For EC2 instance profile (no OIDC vars needed)
terraform init
terraform apply
```

The module outputs the IAM role ARN, which you pass to the Helm chart or attach to your EC2 instances.

See `deploy/terraform/aws-discovery/main.tf` for the full configuration.

## API Reference

All discovery endpoints require `account_id` and support RBAC when auth is enabled.

### Trigger a Sync (Server-Side)

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

### Provision an Agent

Creates an API key and returns a bootstrap token for agent deployment.

```bash
curl -X POST http://localhost:8080/api/v1/discovery/agents/provision \
  -H 'Content-Type: application/json' \
  -d '{"name": "prod-us-east-1"}'
```

### Register an Agent

Called by the agent on first startup (when using a bootstrap token). Not typically called manually.

```bash
curl -X POST http://localhost:8080/api/v1/discovery/agents/register \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer cpk_abc123...' \
  -d '{
    "agent_id": "uuid",
    "name": "prod-us-east-1",
    "account_id": 1,
    "version": "dev",
    "hostname": "ip-10-0-1-42"
  }'
```

### Agent Heartbeat

Sent periodically by the agent. Not typically called manually.

```bash
curl -X POST http://localhost:8080/api/v1/discovery/agents/heartbeat \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer cpk_abc123...' \
  -d '{
    "agent_id": "uuid",
    "name": "prod-us-east-1",
    "account_id": 1,
    "version": "dev",
    "hostname": "ip-10-0-1-42"
  }'
```

### List Agents

```bash
curl 'http://localhost:8080/api/v1/discovery/agents'
curl 'http://localhost:8080/api/v1/discovery/agents?account_id=1'
```

### Approve / Reject an Agent

```bash
# Approve
curl -X POST http://localhost:8080/api/v1/discovery/agents/{agent-uuid}/approve

# Reject
curl -X POST http://localhost:8080/api/v1/discovery/agents/{agent-uuid}/reject
```

### Ingest Resources (Agent Push)

Called by the agent to push discovered resources. Not typically called manually.

```bash
curl -X POST http://localhost:8080/api/v1/discovery/ingest \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer cpk_abc123...' \
  -d '{
    "account_id": 1,
    "agent_id": "uuid",
    "resources": [...]
  }'
```

## RBAC Permissions

When `CLOUDPAM_AUTH_ENABLED=true`, discovery endpoints require the `discovery` resource permission:

| Action | Required Permission | Roles |
|--------|-------------------|-------|
| List resources, view sync jobs, list agents | `discovery:read` | admin, operator, viewer |
| Trigger sync, provision agents | `discovery:create` | admin, operator |
| Link/unlink, approve/reject agents | `discovery:update` | admin, operator |

## Frontend

The Discovery page is at `/discovery` in the CloudPAM UI. It has three tabs:

- **Resources** — table of discovered cloud resources with search + filters (type, status, linked). Click the link/unlink icons in the Actions column to manage pool associations.
- **Sync History** — table of past sync jobs showing status, timing, and resource counts.
- **Agents** — table of registered discovery agents with health status, version, hostname, and last heartbeat time. Auto-refreshes every 30 seconds.

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

The same `ProcessResources()` method is used by both server-side sync and agent ingest, ensuring consistent resource handling regardless of discovery mode.

### Storage

Discovery data lives in these tables:

| Table | Migration | Purpose |
|-------|-----------|---------|
| `discovered_resources` | `0008_discovered_resources.sql` | Cloud resources with UUID PKs, unique on (resource_id, account_id) |
| `sync_jobs` | `0008_discovered_resources.sql` | Sync run history with source (local/agent) and agent_id |
| `discovery_agents` | `0009_discovery_agents.sql` | Registered agents with version, hostname, last_seen_at |
| (approval columns) | `0010_agent_registration.sql` | Agent approval workflow: status, registered_at, approved_at |

The `DiscoveryStore` interface (`internal/storage/discovery.go`) defines 15 methods. Implementations exist for in-memory and SQLite backends.

## Adding a New Cloud Provider

To add GCP or Azure discovery:

1. Create `internal/discovery/gcp/collector.go` (or `azure/`)
2. Implement the `Collector` interface
3. Register the collector in `cmd/cloudpam/main.go`:
   ```go
   syncService.RegisterCollector(gcpcollector.New())
   ```
4. Create accounts with `"provider": "gcp"` — the sync service auto-selects the right collector

No changes needed to the API, frontend, or storage layer — they're provider-agnostic.
