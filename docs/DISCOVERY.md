# Cloud Discovery

CloudPAM's discovery subsystem automatically finds VPCs, subnets, and Elastic IPs in your cloud accounts, then lets you link them to CloudPAM pools for unified IP address management.

## Vocabulary

CloudPAM keeps designed IPAM state separate from observed cloud state:

| Term | Meaning |
|------|---------|
| Address pool | A CloudPAM-managed container for address space. |
| Allocated block | Approved or designed IPAM intent shown in the Allocated Blocks view. |
| Discovered resource | Observed cloud state from a provider scan. |
| Network object | A durable managed cloud/networking object such as a VPC, subnet, EIP, public IP, network, or placeholder parent. |
| Soft link | A non-destructive association between a discovered resource and a managed pool. |

VPCs and subnets can be converted into discovered-source pools when an operator explicitly imports them. EIPs, public IPs, placeholder parents, and other provider-neutral resources can be represented as managed network objects without becoming allocated blocks.

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

### Import Preview and Apply

The Discovery page supports checkbox multi-select before import. Select active, unlinked resources and choose **Preview Import** to review the proposed action for each resource before any pool is created.

Preview can return:

| Status | Meaning |
|--------|---------|
| `importable` | CloudPAM can create or link a discovered-source pool for the selected resource. |
| `conflict` | Import needs operator review because of duplicate CIDR or an overlapping managed pool. |
| `blocked` | Import cannot proceed, for example because the CIDR is invalid, parent VPC is missing, or the selected pool does not contain the resource. |
| `linked_only` | The resource is a network-object candidate, such as an EIP or NIC, and is not converted into a pool. |
| `already_linked` | The discovered resource already has a soft link to a pool. |

API callers can use the same flow:

```bash
curl -X POST http://localhost:8080/api/v1/discovery/import/preview \
  -H 'Content-Type: application/json' \
  -d '{"account_id":1,"resource_ids":["00000000-0000-0000-0000-000000000001"]}'
```

```bash
curl -X POST http://localhost:8080/api/v1/discovery/import/apply \
  -H 'Content-Type: application/json' \
  -d '{"account_id":1,"resource_ids":["00000000-0000-0000-0000-000000000001"]}'
```

`POST /api/v1/discovery/import` remains available for compatibility, but new integrations should use preview/apply so conflicts and non-pool network objects are visible before conversion.

## Merged Network Views

CloudPAM exposes merged network views that combine managed pools, linked discovered resources, durable managed network objects, discovered-only network objects, explicit relationships, and computed conflict evidence.

The Discovery page includes a **Merged Network** tab with:

| Mode | Use |
|------|-----|
| Hierarchy | Day-to-day review of pools, VPCs, subnets, and child objects. |
| Flat | Audit/search view with filters for object type and issue type. |
| Conflicts | Evidence panel for duplicate CIDRs, missing parents, invalid nesting, outside-pool links, and managed overlaps. |

API callers can use:

```bash
curl http://localhost:8080/api/v1/network/hierarchy
curl http://localhost:8080/api/v1/network/flat?object_type=vpc
curl http://localhost:8080/api/v1/network/conflicts?conflict_type=duplicate_cidr
curl http://localhost:8080/api/v1/network/objects?object_type=eip
curl http://localhost:8080/api/v1/network/relationships?type=matches
```

Conflict responses include stable IDs, severity, affected discovered resource IDs, affected pool IDs, account/region metadata, evidence lines, explicit relationship records when present, and available review decisions: `skip`, `ignore`, and `defer`.

```bash
curl -X POST http://localhost:8080/api/v1/network/conflicts/duplicate-cidr:10.0.0.0_16/resolve \
  -H 'Content-Type: application/json' \
  -d '{"decision":"skip","reason":"reviewed duplicate lab account"}'
```

Conflict resolution requests for computed conflicts are persisted as drift records keyed by the stable conflict ID where the configured drift store is durable, such as SQLite or PostgreSQL. Computed conflicts also write explicit relationship records when network relationship storage is available, and merged views rehydrate those relationships when conflicts are recomputed.

### Managed Network Objects

Managed network objects are for cloud/network entities that should be queryable without being treated as approved IPAM allocations. Use them for EIPs/public IPs, imported-but-not-allocated resources, placeholders for missing parents, or provider-neutral objects that do not belong in the pool hierarchy.

```bash
curl -X POST http://localhost:8080/api/v1/network/objects \
  -H 'Content-Type: application/json' \
  -d '{"object_type":"public_ip","provider":"aws","account_id":1,"region":"us-east-1","name":"nat-eip","ip_address":"198.51.100.10","provider_resource_id":"eipalloc-123"}'
```

Relationships connect pools, discovered resources, managed network objects, and conflict evidence without changing import state. Relationship types include `contains`, `matches`, `conflicts`, `missing_parent`, `candidate_import`, `imported_as`, and `duplicate_of`.

Relationship resolution can be updated by ID in the request body. Use the body form when a caller-supplied relationship ID contains URL path separators such as `/`.

```bash
curl -X POST http://localhost:8080/api/v1/network/relationships/resolve \
  -H 'Content-Type: application/json' \
  -d '{"id":"tenant/a","resolution_state":"resolved","reason":"accepted"}'
```

### Schema Policy

Duplicate and hierarchy evidence can be evaluated with a schema policy. If no
query override is supplied, CloudPAM uses the persisted default from
`/api/v1/settings/network-schema-policy`; if that setting does not exist, it
falls back to `account_level`.

Persist the default policy:

```bash
curl -X PATCH http://localhost:8080/api/v1/settings/network-schema-policy \
  -H 'Content-Type: application/json' \
  -d '{"name":"global"}'
```

Read the active persisted default:

```bash
curl http://localhost:8080/api/v1/settings/network-schema-policy
```

Override the policy for one request:

```bash
curl http://localhost:8080/api/v1/network/conflicts?schema_policy=account_level
curl http://localhost:8080/api/v1/network/conflicts?schema_policy=region_level
curl http://localhost:8080/api/v1/network/conflicts?schema_policy=global
curl http://localhost:8080/api/v1/network/conflicts?schema_policy=manual
```

`account_level` is the default behavior and scopes duplicate CIDR checks and
discovered parent lookup to each account. `region_level` scopes duplicate and
parent checks to each account/region pair. `global` treats duplicate CIDRs and
discovered parent IDs anywhere as shared. `manual` suppresses inferred duplicate
and parent-placement conflicts unless callers pass an explicit duplicate
override such as `duplicates=global`.

## Operator Examples

Use these examples as a guide for deciding whether discovered cloud resources
should stay observed-only, be linked to an existing pool, become managed network
objects, or become allocated blocks.

| Resource state | Use when | CloudPAM behavior |
|----------------|----------|-------------------|
| Discovered-only | The object is real cloud state, but you are not ready to attach it to the IPAM plan. | The resource appears in Discovery and merged network views with provider/account/region evidence, but no pool link or managed record is created. |
| Link-only | The object should be visible in context but should not reserve address space as approved IPAM intent. | CloudPAM records a soft relationship to the containing pool or related record without creating an allocated block. EIPs and public IPs usually land here. |
| Imported network object | The cloud object should be durable and queryable as a VPC, subnet, EIP, public IP, or placeholder parent. | CloudPAM creates or updates a managed network object and relationships while preserving the discovered source. |
| Allocated block | The CIDR is approved or designed address intent that should appear in Allocated Blocks. | CloudPAM represents the address space as managed IPAM intent. Use this for planned allocations, not every discovered cloud object. |

### VPC Inside a Pool

Example layout:

```text
Pool: prod-aws-account-a       10.40.0.0/12
  Discovered AWS VPC: prod-vpc  10.40.0.0/16  vpc-0a111111
    Discovered subnet: app-a    10.40.1.0/24  subnet-0a222222
```

In the Discovery UI, the VPC can remain discovered-only while operators review
it in **Merged Network**. If the VPC is expected and the containing pool is
correct, link it to the pool or import it through the discovery flow as a
discovered-source pool. Use a managed VPC network object when you need a durable
cloud object record without treating the CIDR as approved allocation intent. Do
not model it as an allocated block unless your IPAM policy treats VPC CIDRs as
approved allocation records.

Illustrative hierarchy response:

```json
{
  "items": [
    {
      "id": "pool:42",
      "kind": "pool",
      "name": "prod-aws-account-a",
      "cidr": "10.40.0.0/12",
      "children": [
        {
          "id": "discovered:11111111-1111-1111-1111-111111111111",
          "kind": "discovered",
          "object_type": "vpc",
          "name": "prod-vpc",
          "cidr": "10.40.0.0/16",
          "source": "discovered",
          "issues": [],
          "children": [
            {
              "id": "discovered:22222222-2222-2222-2222-222222222222",
              "kind": "discovered",
              "object_type": "subnet",
              "name": "app-a",
              "cidr": "10.40.1.0/24",
              "parent_provider_resource_id": "vpc-0a111111",
              "issues": []
            }
          ]
        }
      ]
    }
  ],
  "schema_policy": "account_level"
}
```

Previewing an import for the VPC should return an `importable` item when the
CIDR is valid, the selected pool contains it, and no conflicting duplicate or
managed overlap blocks it:

```json
{
  "items": [
    {
      "resource_id": "11111111-1111-1111-1111-111111111111",
      "resource_type": "vpc",
      "cidr": "10.40.0.0/16",
      "status": "importable",
      "proposed_action": "create_pool",
      "proposed_managed_type": "discovered_pool",
      "proposed_pool_id": 42,
      "issues": []
    }
  ],
  "importable": 1,
  "conflict_count": 0,
  "blocked": 0,
  "linked_only": 0
}
```

### Subnet With a Missing VPC Parent

Cloud providers can return a subnet whose `parent_provider_resource_id` points
to a VPC that CloudPAM did not discover in the same scope. This can happen when
credentials are incomplete, a region was omitted, or data was ingested from a
partial inventory.

Example:

```text
Discovered subnet: app-orphan      10.51.4.0/24
Parent provider resource ID:       vpc-missing
Discovered VPC record:             absent
```

Merged views should show a `missing_parent` issue instead of silently nesting
the subnet under any pool that happens to contain `10.51.4.0/24`.

```json
{
  "id": "missing-parent:33333333-3333-3333-3333-333333333333",
  "type": "missing_parent",
  "severity": "warning",
  "title": "Subnet references missing VPC parent",
  "discovered_ids": ["33333333-3333-3333-3333-333333333333"],
  "evidence": [
    "subnet subnet-0b333333 references parent vpc-missing",
    "no discovered VPC with provider_resource_id vpc-missing was found in account aws:111111111111 region us-east-1"
  ],
  "available_decisions": ["skip", "ignore", "defer"]
}
```

Import preview blocks the subnet until the missing parent is handled:

```json
{
  "items": [
    {
      "resource_id": "33333333-3333-3333-3333-333333333333",
      "resource_type": "subnet",
      "cidr": "10.51.4.0/24",
      "status": "blocked",
      "issues": ["missing_parent"]
    }
  ],
  "blocked": 1
}
```

Current resolution paths are to rediscover the missing VPC, skip or defer the
conflict, ignore it when the provider data is intentionally incomplete, or use
the placeholder-parent action from the conflict panel/API when a durable
placeholder VPC object is the right operational record.

### Duplicate VPC CIDR Across AWS Accounts

When two AWS accounts use the same VPC CIDR, the correct interpretation depends
on your schema policy. Under `account_level`, duplicate CIDRs are scoped per
account, so two accounts can use the same address range without being treated as
a duplicate by default. Under `global`, the same layout is flagged as a
cross-account duplicate.

Example:

```text
AWS account A / us-east-1 / vpc-prod-a   10.60.0.0/16
AWS account B / us-east-1 / vpc-prod-b   10.60.0.0/16
```

Use a global duplicate review when address space must be unique across the whole
organization:

```bash
curl 'http://localhost:8080/api/v1/network/conflicts?conflict_type=duplicate_cidr&schema_policy=global'
```

Illustrative conflict response:

```json
{
  "items": [
    {
      "id": "duplicate-cidr:10.60.0.0_16",
      "type": "duplicate_cidr",
      "severity": "error",
      "title": "Duplicate CIDR 10.60.0.0/16",
      "discovered_ids": [
        "44444444-4444-4444-4444-444444444444",
        "55555555-5555-5555-5555-555555555555"
      ],
      "evidence": [
        "aws account aws:111111111111 region us-east-1 vpc vpc-prod-a uses 10.60.0.0/16",
        "aws account aws:222222222222 region us-east-1 vpc vpc-prod-b uses 10.60.0.0/16",
        "schema_policy=global requires duplicate CIDRs to be reviewed across accounts"
      ],
      "available_decisions": ["skip", "ignore", "defer"]
    }
  ],
  "schema_policy": "global"
}
```

If the duplicate is expected because each account has isolated address space,
keep the default `account_level` policy or record an `ignore` decision with a
reason. If it is unintended drift, import or link only the authoritative object
and leave the conflicting VPC discovered-only until the cloud network is fixed.

### EIP as an Account and Region Object

An Elastic IP is an address-bearing cloud object, but it is not a pool and
should not become an allocated block. CloudPAM surfaces EIPs as network-object
candidates so they can be searched, related to pools or conflicts, and grouped
by account/region.

Example managed object request:

```bash
curl -X POST http://localhost:8080/api/v1/network/objects \
  -H 'Content-Type: application/json' \
  -d '{
    "object_type": "eip",
    "provider": "aws",
    "account_id": 7,
    "region": "us-west-2",
    "name": "nat-prod-a",
    "ip_address": "198.51.100.42",
    "provider_resource_id": "eipalloc-0e444444"
  }'
```

Discovery import preview reports EIPs as `linked_only` rather than pool imports:

```json
{
  "items": [
    {
      "resource_id": "66666666-6666-6666-6666-666666666666",
      "resource_type": "eip",
      "provider_resource_id": "eipalloc-0e444444",
      "ip_address": "198.51.100.42",
      "status": "linked_only",
      "proposed_action": "link_only",
      "proposed_managed_type": "network_object",
      "issues": ["network_object_only"],
      "evidence": [
        "EIPs are network-object candidates and are not converted into pools"
      ]
    }
  ],
  "linked_only": 1
}
```

Keep EIPs link-only when they are operational evidence attached to NAT gateways,
instances, load balancers, or account inventory. Create an allocated block only
for intentionally managed public address space, not for each discovered EIP.

### Pool Per Account

Use a pool-per-account layout when each AWS account owns an address boundary and
regions or VPCs sit below that account boundary.

```text
Global private space        10.0.0.0/8
  AWS account A pool        10.70.0.0/12
    us-east-1 VPC object    10.70.0.0/16
    us-west-2 VPC object    10.71.0.0/16
  AWS account B pool        10.80.0.0/12
    us-east-1 VPC object    10.80.0.0/16
```

Recommended behavior:

| Scenario | Recommended state |
|----------|-------------------|
| VPC CIDR is inside the account pool and expected | Link to the account pool, import as a discovered-source pool, or create a managed network object if it should not be allocation intent. |
| VPC CIDR exactly matches an approved account allocation | Link to the existing allocated block; do not create a duplicate block. |
| VPC CIDR belongs to a different account pool | Treat as outside-pool drift and review before import. |
| Same VPC CIDR appears in two accounts | Usually acceptable under `account_level`; use `global` when the organization requires uniqueness. |

Set the default schema policy to account-level when this is the standard
operating model:

```bash
curl -X PATCH http://localhost:8080/api/v1/settings/network-schema-policy \
  -H 'Content-Type: application/json' \
  -d '{"name":"account_level"}'
```

### Pool Per Region

Use a pool-per-region layout when each account has regional address boundaries
and VPCs are expected to sit under the matching account/region pool.

```text
AWS account A pool          10.90.0.0/12
  us-east-1 region pool     10.90.0.0/16
    prod VPC object         10.90.8.0/21
  us-west-2 region pool     10.91.0.0/16
    prod VPC object         10.91.8.0/21
```

Recommended behavior:

| Scenario | Recommended state |
|----------|-------------------|
| VPC is inside the matching region pool | Link, import as a discovered-source pool, or create a managed network object if it should not be allocation intent. |
| VPC is inside the account pool but outside its region pool | Treat as outside-pool or placement drift; fix the pool selection before import. |
| Same CIDR appears in two regions of the same account | Review with `region_level` or stricter policy depending on whether regional overlap is allowed. |
| Subnet parent VPC is in another region | Treat as invalid provider hierarchy and do not import into the selected region pool. |

Set or override the schema policy to region-level for review:

```bash
curl -X PATCH http://localhost:8080/api/v1/settings/network-schema-policy \
  -H 'Content-Type: application/json' \
  -d '{"name":"region_level"}'

curl 'http://localhost:8080/api/v1/network/conflicts?schema_policy=region_level'
```

### Current Limitations

- Import preview creates discovered-source pools for importable VPCs and subnets;
  it does not let the operator choose every future conversion shape from the
  preview response alone.
- EIPs and public IPs are represented as link-only or managed network objects,
  not as allocated blocks.
- Missing-parent handling can create a placeholder parent from the conflict
  workflow, but CloudPAM does not infer all missing provider hierarchy for you.
- Conflict resolution records durable review decisions and relationship
  evidence where durable drift/network storage is configured. In-memory
  development mode loses those records when the process exits.
- Schema policy is global or request-scoped today; per-pool or per-account
  schema policy is a follow-up area.

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
CLOUDPAM_AGENT_ID_FILE=/var/lib/cloudpam-agent/agent-id \
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
CLOUDPAM_AGENT_ID_FILE=/var/lib/cloudpam-agent/agent-id \
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
agent_id_file: /var/lib/cloudpam-agent/agent-id
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
| `agent_id` | `CLOUDPAM_AGENT_ID` | | Explicit discovery agent UUID |
| `agent_id_file` | `CLOUDPAM_AGENT_ID_FILE` | deterministic fallback | Host-side file used to persist a generated discovery agent UUID |
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
           -v /var/lib/cloudpam-agent:/var/lib/cloudpam-agent \
           -e CLOUDPAM_AGENT_ID_FILE=/var/lib/cloudpam-agent/agent-id \
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

## AWS Organizations Discovery

For environments using AWS Organizations, a single CloudPAM agent in the management account can discover resources across all member accounts.

### Architecture

```
Management Account (agent runs here)
├── organizations:ListAccounts → enumerate all active member accounts
├── For each member account:
│   ├── sts:AssumeRole → CloudPAMDiscoveryRole
│   ├── ec2:DescribeVpcs
│   ├── ec2:DescribeSubnets
│   └── ec2:DescribeAddresses
└── POST /api/v1/discovery/ingest/org → bulk push to server
    └── Server auto-creates Account records for new AWS accounts
```

### Setup

1. **Deploy member role** to all accounts via CloudFormation StackSet or Terraform:

   ```bash
   # CloudFormation StackSet (recommended for org-wide)
   aws cloudformation create-stack-set \
     --stack-set-name CloudPAMDiscoveryRole \
     --template-body file://deploy/cloudformation/discovery-role-stackset.yaml \
     --parameters ParameterKey=ManagementAccountId,ParameterValue=123456789012

   # Or Terraform (per-account)
   cd deploy/terraform/aws-org-discovery/member-role
   terraform apply -var="management_account_id=123456789012"
   ```

2. **Deploy management policy** in the management account:

   ```bash
   cd deploy/terraform/aws-org-discovery/management-policy
   terraform apply
   ```

   This creates the agent IAM role, instance profile, and three policies:
   - Org discovery: `organizations:ListAccounts`, `sts:AssumeRole`
   - EC2 read-only: `ec2:DescribeVpcs/Subnets/Addresses/NetworkInterfaces/Regions`
   - STS identity: `sts:GetCallerIdentity`

3. **Configure the agent** in org mode:

   ```bash
   CLOUDPAM_BOOTSTRAP_TOKEN=eyJhZ2VudF9uYW1lIjoi... \
   CLOUDPAM_AWS_ORG_ENABLED=true \
   CLOUDPAM_AWS_ORG_ROLE_NAME=CloudPAMDiscoveryRole \
   CLOUDPAM_AWS_ORG_REGIONS=us-east-1,us-west-2 \
   ./cloudpam-agent
   ```

   Or via YAML:

   ```yaml
   server_url: https://cloudpam.example.com
   bootstrap_token: eyJhZ2VudF9uYW1lIjoi...
   agent_name: org-discovery-agent
   aws_org:
     enabled: true
     role_name: CloudPAMDiscoveryRole
     external_id: cloudpam-discovery  # optional
     regions: [us-east-1, us-west-2]
     exclude_accounts: [999999999999]  # optional
   ```

### Org Agent Configuration Reference

| Field | Env Var | Default | Description |
|-------|---------|---------|-------------|
| `aws_org.enabled` | `CLOUDPAM_AWS_ORG_ENABLED` | `false` | Enable org-mode discovery |
| `aws_org.role_name` | `CLOUDPAM_AWS_ORG_ROLE_NAME` | `CloudPAMDiscoveryRole` | IAM role to assume in each member account |
| `aws_org.external_id` | `CLOUDPAM_AWS_ORG_EXTERNAL_ID` | | External ID for STS AssumeRole |
| `aws_org.regions` | `CLOUDPAM_AWS_ORG_REGIONS` | SDK default | Comma-separated regions to discover |
| `aws_org.exclude_accounts` | `CLOUDPAM_AWS_ORG_EXCLUDE_ACCOUNTS` | | Comma-separated account IDs to skip |

### Bulk Org Ingest API

```bash
curl -X POST http://localhost:8080/api/v1/discovery/ingest/org \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer cpk_abc123...' \
  -d '{
    "accounts": [
      {
        "aws_account_id": "111111111111",
        "account_name": "Production",
        "account_email": "prod@example.com",
        "provider": "aws",
        "regions": ["us-east-1"],
        "resources": [...]
      }
    ],
    "agent_id": "uuid"
  }'
```

Response:

```json
{
  "accounts_processed": 5,
  "accounts_created": 2,
  "total_resources": 47,
  "errors": []
}
```

The server auto-creates CloudPAM Account records (key `aws:<account_id>`) for any AWS accounts not yet registered.

### Terraform Modules

| Module | Path | Purpose |
|--------|------|---------|
| Management policy | `deploy/terraform/aws-org-discovery/management-policy/` | Agent IAM role, instance profile, org + EC2 + STS policies |
| Member role | `deploy/terraform/aws-org-discovery/member-role/` | Cross-account discovery role with trust to management account |
| CF StackSet | `deploy/cloudformation/discovery-role-stackset.yaml` | Deploy member role across org via StackSet |

### Frontend Wizard

The Discovery wizard (Plan Discovery button) supports org mode:
1. Step 1: Choose "AWS Organization" mode, configure role name, regions, exclude list
2. Step 2: Provision the agent (generates bootstrap token)
3. Step 3: Deploy using generated configs; wizard polls for agent connection and shows live status

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
