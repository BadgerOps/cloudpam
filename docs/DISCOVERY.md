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

Apply creates pools with the same checks as `POST /api/v1/pools`, under the same placement lock. A subnet goes under the pool that the same request created or linked for its VPC. A pool that is not inside its parent, or that overlaps a pool with the same parent, is reported in `errors` and skipped. The rest of the selection is still imported. The network conflict `override` does not bypass the overlap check.

`POST /api/v1/discovery/import` remains available for compatibility, but new integrations should use preview/apply so conflicts and non-pool network objects are visible before conversion. It applies the same checks. It places each VPC at the top level, so a VPC inside an existing top-level pool is reported as an overlap. Use preview and apply to place it under that pool.

### Drift Reconciliation Suggestions

`GET /api/v1/drift`, `GET /api/v1/drift/{id}` and `POST /api/v1/drift/detect` return a suggestion on every open drift item. The server derives it from the current pools and resources on each read and does not store it, so a suggestion reflects pools created since detection ran. When the resource's link, CIDR or status changed since detection, or an active resource links to an orphaned pool again, the suggestion is `review` until detection runs again.

| `suggested_action` | When | `suggested_pool_id` | Applied by |
|--------------------|------|---------------------|------------|
| `link_pool` | An unmanaged resource has an exact-CIDR pool in its account (or an unassigned one), or a linked resource's CIDR changed to match another pool. | The pool to link to. | `POST /api/v1/discovery/resources/{id}/link` |
| `create_pool` | Import preview classifies the unmanaged resource as `importable`. | Its most specific containing pool, or absent for a top-level pool. | `POST /api/v1/discovery/import/apply` with that one resource |
| `deprecate_pool` | A discovered-source pool has no active linked resource and is not deprecated yet. | The orphaned pool. | `PATCH /api/v1/pools/{id}` with `{"status":"deprecated"}` |
| `review` | Anything else, such as an overlap, a missing parent VPC, an EIP or NIC, or a CIDR mismatch with no matching pool. | Absent. | By hand |

`suggestion_reason` says why in one line. Each unmanaged resource is classified by the import preview above with only that resource selected, so a `create_pool` or `link_pool` suggestion is exactly what apply will do. Detection does not report a deprecated discovered-source pool as orphaned, so an applied `deprecate_pool` stays reconciled. The Drift page shows the suggestion under each open item. Its apply button calls the listed endpoint and then resolves the item. If import apply skips the resource, the item stays open and the page reports why.

### Provider Rate Limits

Each collector can pace its provider API calls with a token bucket. The cap is requests per second; the burst is one second's worth of calls. Every page of a listing (`DescribeVpcs`, `DescribeSubnets`, `DescribeAddresses`, Compute `networks`, `aggregated/subnetworks`, `aggregated/addresses`, and Organizations `ListAccounts` in org mode) takes one token first, and concurrent syncs of the same provider share one bucket. The default is unlimited, which leaves throttling to the SDK's own retry.

| Where | Setting | Scope |
|-------|---------|-------|
| Server | `CLOUDPAM_DISCOVERY_AWS_RPS`, `CLOUDPAM_DISCOVERY_GCP_RPS` | All syncs of that provider, including stored per-account credentials |
| Agent | `discovery_aws_rps` / `CLOUDPAM_DISCOVERY_AWS_RPS`, `discovery_gcp_rps` / `CLOUDPAM_DISCOVERY_GCP_RPS` | Single-account syncs and every account of an org-mode pass. A GCP org pass also spends tokens on Resource Manager listing and IAM Credentials token requests. |

A sync whose context ends while it waits for a token fails with that context error, the same as a provider call that times out.

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

## Credential models

Every sync runs as some cloud identity. Until stored credentials existed, that identity was always the host's, never something saved in CloudPAM.

### How discovery reaches an account

| Path | Identity used | Reaches | Where it is set up |
|------|---------------|---------|--------------------|
| Server sync | The server host. An EC2 instance profile, ECS task role, or `AWS_*` variables for AWS. Application Default Credentials for GCP. | The host's own AWS account. The GCP projects the ADC identity can read. | The server registers the AWS and GCP collectors at startup. `POST /api/v1/discovery/sync` runs them when no agent is connected for the account. |
| Agent, AWS single account | The agent host's identity, deployed next to the account. | That one account. | `cloudpam-agent` with `CLOUDPAM_PROVIDER=aws` and `CLOUDPAM_ACCOUNT_ID`. It pushes through the ingest API. |
| Agent, AWS organization mode | The agent host's identity in the management account, then STS `AssumeRole` into `CloudPAMDiscoveryRole` in every member account, with the configured external ID. | Every account in one AWS organization. | `cloudpam-agent` with `CLOUDPAM_PROVIDER=aws` and the `CLOUDPAM_AWS_ORG_*` variables. The Terraform `member-role` module or the CloudFormation StackSet creates the role. See [AWS Organizations Discovery](#aws-organizations-discovery). |
| Agent, GCP single-project mode | The agent's Application Default Credentials: GKE Workload Identity, a service-account key, or the GCE metadata server. | That one project. | `cloudpam-agent` with `CLOUDPAM_PROVIDER=gcp` and `CLOUDPAM_GCP_PROJECT`. See [GCP Agent](#gcp-agent). |
| Agent, GCP organization mode | The agent's Application Default Credentials, optionally impersonating a per-project service account. | Every active project under one organization or folder. | `cloudpam-agent` with `CLOUDPAM_PROVIDER=gcp` and the `CLOUDPAM_GCP_ORG_*` variables. You grant the roles with `gcloud`. See [GCP Agent](#gcp-agent). |
| Stored per-account credential | A role ARN the server assumes, or a GCP service-account key, saved encrypted on the account record. | That account, from the server, with no agent and no change to the host identity. | The **Credentials** action on the Accounts page, or `PUT /api/v1/accounts/{id}/credentials`. |

For one AWS organization with the member role deployed, or one GCP organization with the roles granted, the server and agent paths are complete. You give the instance or the agent an IAM identity and CloudPAM uses it. No credential goes into the application.

### Why stored credentials exist

Some accounts are out of reach for every host identity:

- a second AWS organization, or a partner account that only grants CloudPAM a role to assume;
- a GCP project whose service account is not the server's ADC identity;
- an environment where no agent can run next to the account.

Before stored credentials, those accounts could not be synced. Now you attach a credential to the account record and the server's own sync uses it. An account with no stored credential behaves exactly as before. Agents never use stored credentials. An account that an agent owns keeps the agent's identity.

### Which identity a sync uses

For one account, a connected agent wins. Otherwise the server uses the stored credential. Otherwise the server uses its host identity.

A stored credential that cannot be decrypted, that does not match the account's provider, or that belongs to a provider with no credential support fails the sync job with an `ErrorMessage` that starts with `credential:`. The server never falls back to the host identity in those cases, so a sync never runs as an identity you did not choose.

### Credential types

One credential per account. Saving a second one replaces the first.

| Type | Account provider | Secret payload | Shown in the UI and the API |
|------|------------------|----------------|-----------------------------|
| `aws_assume_role` | `aws` | `role_arn`, optional `external_id` | `role_arn`, `has_external_id` |
| `gcp_service_account` | `gcp` | the service-account key file | `client_email`, `project_id` |

The payload is encrypted with the application key `CLOUDPAM_OIDC_ENCRYPTION_KEY` through the same mechanism that protects OIDC client secrets and the AI provider API key. The summary is stored in clear. Account reads, the account list, search, and CSV export never include the payload. The GCP key's `token_uri` is dropped at save time. The server always requests tokens from Google's own endpoint.

The application key must be set, or saving returns `409 Conflict`. When the key is missing the server generates one at startup and logs a warning. A credential encrypted with that key would be unreadable after a restart, so the server refuses to store one. Set `CLOUDPAM_OIDC_ENCRYPTION_KEY` to a 32-byte hex string (`openssl rand -hex 32`) and restart.

### Set up an AWS assumed role

The server's own identity must be allowed to assume the role, and the role must be able to read EC2 networking. Create the role in the target account with this trust policy. Replace the principal with the ARN of the server's identity (the instance profile role, the task role, or the user behind the `AWS_*` variables).

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": { "AWS": "arn:aws:iam::111111111111:role/cloudpam-server" },
      "Action": "sts:AssumeRole",
      "Condition": { "StringEquals": { "sts:ExternalId": "choose-a-long-random-string" } }
    }
  ]
}
```

Leave out the `Condition` block when you save the credential without an external ID. When you set one, the value you save must equal the value in the policy.

Attach the read-only permissions from [Required IAM Permissions](#required-iam-permissions) to the role. The server's own identity needs `sts:AssumeRole` on the role ARN.

The server calls STS in the first region listed on the account, then in the host's default region, then in `us-east-1`.

### Set up a GCP service account

Create a service account in the project, grant it `roles/compute.networkViewer` on the project, and create a JSON key. Paste the whole key file into the credential form. The server keeps only `client_email`, `project_id`, `private_key_id`, and `private_key`.

The test action reads the project named by the account's `external_id`, or by the `gcp:` key when `external_id` is empty. That is the project sync reads too, so a passing test proves the credential can discover that account.

### Credential API

All routes live under `/api/v1/accounts/{id}/credentials`. Reading needs `accounts:read`. Saving, deleting, and testing need `accounts:update`.

Store or replace a credential:

```bash
curl -X PUT http://localhost:8080/api/v1/accounts/1/credentials \
  -H 'Content-Type: application/json' \
  -d '{
    "type": "aws_assume_role",
    "aws_assume_role": {
      "role_arn": "arn:aws:iam::222222222222:role/CloudPAMDiscoveryRole",
      "external_id": "choose-a-long-random-string"
    }
  }'
```

```bash
curl -X PUT http://localhost:8080/api/v1/accounts/2/credentials \
  -H 'Content-Type: application/json' \
  -d '{
    "type": "gcp_service_account",
    "gcp_service_account": { "key_json": "{\"type\":\"service_account\",\"project_id\":\"my-project\",\"client_email\":\"cloudpam@my-project.iam.gserviceaccount.com\",\"private_key\":\"-----BEGIN PRIVATE KEY-----\\n...\"}" }
  }'
```

Exactly one of `aws_assume_role` and `gcp_service_account` is set, and it must be the one that `type` names. Unknown fields are rejected. The response is the credential view with the summary, never the payload:

```json
{
  "account_id": 1,
  "type": "aws_assume_role",
  "summary": { "role_arn": "arn:aws:iam::222222222222:role/CloudPAMDiscoveryRole", "has_external_id": true },
  "created_at": "2026-11-02T10:00:00Z",
  "updated_at": "2026-11-02T10:00:00Z"
}
```

Read the stored credential:

```bash
curl http://localhost:8080/api/v1/accounts/1/credentials
```

`last_test` appears after a test and is cleared by the next save:

```json
{
  "account_id": 1,
  "type": "aws_assume_role",
  "summary": { "role_arn": "arn:aws:iam::222222222222:role/CloudPAMDiscoveryRole", "has_external_id": true },
  "last_test": { "tested_at": "2026-11-02T10:05:00Z", "status": "failed", "error": "assume_role: AccessDenied (http 403)" },
  "created_at": "2026-11-02T10:00:00Z",
  "updated_at": "2026-11-02T10:00:00Z"
}
```

Test the stored credential. The test uses only the stored credential, never the host identity. AWS runs `sts:GetCallerIdentity` as the assumed role. GCP fetches a token and reads the project. The call takes at most 15 seconds:

```bash
curl -X POST http://localhost:8080/api/v1/accounts/1/credentials/test
```

```json
{ "tested_at": "2026-11-02T10:05:00Z", "status": "ok", "identity": "arn:aws:sts::222222222222:assumed-role/CloudPAMDiscoveryRole/cloudpam" }
```

A failing credential is `200` with `"status": "failed"` and a sanitized `error` such as `assume_role: AccessDenied (http 403)`, `token: invalid_grant (http 400)`, `endpoint unreachable`, or `timed out after 15s`. The error never includes a response body or a credential value.

Delete the stored credential:

```bash
curl -X DELETE http://localhost:8080/api/v1/accounts/1/credentials
```

The response is `204`.

Error responses:

| Status | When | Body |
|--------|------|------|
| `400` | A field fails validation, or `type` does not match the account's provider | `{"error": "invalid aws_assume_role.role_arn", "detail": "must look like arn:aws:iam::<12-digit account>:role/<name>"}` or `{"error": "credential type does not match the account provider"}` |
| `404` | The account does not exist, or no credential is stored (GET, DELETE, test) | `{"error": "account not found"}` or `{"error": "no credential stored for this account"}` |
| `409` | `CLOUDPAM_OIDC_ENCRYPTION_KEY` is not set, so the server runs on an auto-generated key | `{"error": "cannot store a credential: CLOUDPAM_OIDC_ENCRYPTION_KEY is not set, so the stored credential would be lost at restart; set the key"}` |

Every save, delete, and test writes an audit event with resource type `account_credential` and the account id as the resource id. The event records the credential type or the test status, never a value. `GET /api/v1/audit?resource_type=account_credential` lists them.

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
│      Your Cloud Account     │          │    CloudPAM Server      │
│                             │          │                         │
│  ┌───────────────────────┐  │  HTTPS   │  POST /discovery/ingest │
│  │   cloudpam-agent      │──┼─────────>│  POST /agents/heartbeat │
│  │  - Cloud credentials  │  │          │  POST /agents/register  │
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
CLOUDPAM_PROVIDER=aws \
./cloudpam-agent
```

`CLOUDPAM_PROVIDER` is required and has no default. Set it to `aws` or `gcp`. For GCP settings, see [GCP Agent](#gcp-agent).

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
CLOUDPAM_PROVIDER=aws \
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
provider: aws
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
| `account_id` | `CLOUDPAM_ACCOUNT_ID` | (required) | CloudPAM account ID to discover for. In org mode, the account the agent heartbeats as. |
| `provider` | `CLOUDPAM_PROVIDER` | (required) | `aws` or `gcp`. One agent serves one provider. |
| `bootstrap_token` | `CLOUDPAM_BOOTSTRAP_TOKEN` | | Base64 provisioning bundle (replaces server_url, api_key, agent_name) |
| `aws_regions` | `CLOUDPAM_AWS_REGIONS` | SDK default | Comma-separated AWS regions |
| `discovery_aws_rps` | `CLOUDPAM_DISCOVERY_AWS_RPS` | `0` (unlimited) | Cap on AWS API calls per second, shared across every account in an org-mode pass |
| `gcp_project` | `CLOUDPAM_GCP_PROJECT` | | GCP project ID. Required when `provider` is `gcp` and `gcp_org` is off. |
| `gcp_regions` | `CLOUDPAM_GCP_REGIONS` | all regions | Comma-separated GCP regions to keep |
| `discovery_gcp_rps` | `CLOUDPAM_DISCOVERY_GCP_RPS` | `0` (unlimited) | Cap on GCP API calls per second, shared across every project in an org-mode pass |
| `sync_interval` | `CLOUDPAM_SYNC_INTERVAL` | `15m` | How often to run discovery |
| `heartbeat_interval` | `CLOUDPAM_HEARTBEAT_INTERVAL` | `1m` | How often to send heartbeats |
| `max_retries` | | `3` | Push retry attempts on server error |
| `retry_backoff` | | `5s` | Initial retry delay (exponential) |
| `request_timeout` | | `30s` | Timeout for each request to the CloudPAM server, and for each GCP API request and token fetch |

When `bootstrap_token` is set and `api_key` is not, the token is decoded and its fields populate `server_url`, `api_key`, and `agent_name`. If `api_key` is set explicitly, the token is ignored.

### Deploying with Docker

The agent image runs as the nonroot user, UID 65532. Docker creates a missing bind-mount source owned by root, and the agent then cannot write its ID file. Create the state directory first. These commands assume a Linux host with rootful Docker. With rootless Docker or userns-remap, use the host UID that container UID 65532 maps to.

```bash
docker build -f deploy/docker/Dockerfile.agent -t cloudpam-agent .

sudo install -d -o 65532 -g 65532 /var/lib/cloudpam-agent

docker run -e CLOUDPAM_BOOTSTRAP_TOKEN=eyJhZ2VudF9uYW1lIjoi... \
           -v /var/lib/cloudpam-agent:/var/lib/cloudpam-agent \
           -e CLOUDPAM_AGENT_ID_FILE=/var/lib/cloudpam-agent/agent-id \
           -e CLOUDPAM_ACCOUNT_ID=1 \
           -e CLOUDPAM_PROVIDER=aws \
           -e AWS_REGION=us-east-1 \
           cloudpam-agent
```

### Deploying with Helm (Kubernetes)

A Helm chart is available in `deploy/helm/cloudpam-agent/`:

```bash
helm install cloudpam-agent deploy/helm/cloudpam-agent/ \
  --set config.serverUrl=https://cloudpam.example.com \
  --set config.accountId=1 \
  --set config.agentName=prod-us-east-eks \
  --set config.provider=aws \
  --set config.awsRegions='{us-east-1,us-west-2}' \
  --set apiKey=cpk_abc123... \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::123456789012:role/cloudpam-discovery
```

`config.agentName` is required. Keep it stable across upgrades and make it unique
across clusters that report to the same CloudPAM server and account.

`config.provider` is required (`aws` or `gcp`), and the chart fails to render without it. A release installed before 0.25.0 must add `config.provider: aws` before it upgrades. For GCP values, see [GCP Agent](#gcp-agent).

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
   CLOUDPAM_ACCOUNT_ID=1 \
   CLOUDPAM_PROVIDER=aws \
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
   account_id: 1
   provider: aws
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
        "external_id": "111111111111",
        "account_name": "Production",
        "account_email": "prod@example.com",
        "provider": "aws",
        "regions": ["us-east-1"],
        "resources": [...]
      }
    ],
    "failures": [
      {"provider": "aws", "external_id": "222222222222", "error": "assume role: access denied"}
    ],
    "agent_id": "uuid",
    "sync_job_id": "uuid"
  }'
```

Response:

```json
{
  "accounts_processed": 5,
  "accounts_created": 2,
  "accounts_failed": 1,
  "total_resources": 47,
  "errors": ["discovery failed for aws:222222222222: assume role: access denied"]
}
```

Every entry needs `provider` (`aws` or `gcp`) and `external_id`, the AWS account ID or GCP project ID. `aws_account_id` is a deprecated alias of `external_id`, still accepted from agents older than 0.25.0. The server matches each entry to an account in this order:

1. The account whose key is `<provider>:<external_id>`.
2. The single account with the same provider and external ID, such as one an operator created by hand. If more than one matches, the entry is skipped with an `ambiguous accounts` error.
3. A new account with key `<provider>:<external_id>`, counted in `accounts_created`.

`sync_job_id` is optional. The agent sends it when the pass answers a server-requested sync. A request with no accounts and no failures is accepted only with a `sync_job_id`, and completes that job with no resources.

`failures` lists the accounts the agent could not read this pass: an AssumeRole, credential or discovery error, or a project that ran past its deadline. When listing the organization fails, the agent sends one failure with no accounts. Failures are reported in `errors` and counted in `accounts_failed`, and the failed accounts' existing inventory is left as it was, so nothing goes stale because one pass could not read it. A supplied `sync_job_id` ends `failed` whenever `errors` is non-empty.

### Terraform Modules

| Module | Path | Purpose |
|--------|------|---------|
| Management policy | `deploy/terraform/aws-org-discovery/management-policy/` | Agent IAM role, instance profile, org + EC2 + STS policies |
| Member role | `deploy/terraform/aws-org-discovery/member-role/` | Cross-account discovery role with trust to management account |
| CF StackSet | `deploy/cloudformation/discovery-role-stackset.yaml` | Deploy member role across org via StackSet |

There is no Terraform module for GCP discovery IAM yet. Use the `gcloud` commands in [GCP Agent](#gcp-agent). Moving these modules to their own repository and adding a GCP module is tracked in #416.

### Frontend Wizard

The Discovery wizard (Plan Discovery button) supports org mode for AWS and GCP:
1. Step 1: Choose the provider, then the organization mode. For AWS, set the role name, regions, and exclude list. For GCP, set the parent, regions, excluded projects, and an optional impersonation template
2. Step 2: Provision the agent (generates bootstrap token)
3. Step 3: Deploy using generated configs; wizard polls for agent connection and shows live status

## GCP Agent

With `CLOUDPAM_PROVIDER=gcp`, the agent discovers GCP networks, subnetworks, and external addresses. It authenticates with Application Default Credentials (ADC): GKE Workload Identity, a service-account key at `GOOGLE_APPLICATION_CREDENTIALS`, or the metadata server on a GCE VM. It has no credential settings of its own.

Upgrade the CloudPAM server to 0.25.0 or later before you deploy or upgrade GCP agents. An older server skips GCP entries in a bulk ingest and ignores reported failures. The agent detects both cases and logs an error that says to upgrade the server.

The [base agent settings](#configuration-reference) still apply. `CLOUDPAM_ACCOUNT_ID` is required in every mode. In org mode it is the account the agent heartbeats as.

### Single project

| Setting | Meaning |
|---|---|
| `CLOUDPAM_PROVIDER=gcp` | Required. |
| `CLOUDPAM_GCP_PROJECT` | The project ID to discover. Required. |
| `CLOUDPAM_GCP_REGIONS` | Optional comma-separated list of regions to keep. Networks are global and always kept. |
| `CLOUDPAM_DISCOVERY_GCP_RPS` | Optional cap on GCP API calls per second. |

The project must have the Compute Engine API (`compute.googleapis.com`) enabled. Grant the agent's service account `roles/compute.networkViewer` on the project:

```bash
gcloud projects add-iam-policy-binding my-project \
  --member="serviceAccount:cloudpam-agent@ops-project.iam.gserviceaccount.com" \
  --role=roles/compute.networkViewer
```

### Organization or folder

| Setting | Meaning |
|---|---|
| `CLOUDPAM_PROVIDER=gcp` | Required. |
| `CLOUDPAM_GCP_ORG_ENABLED=true` | Turns on org mode. `CLOUDPAM_GCP_PROJECT` is not needed. |
| `CLOUDPAM_GCP_ORG_PARENT` | `organizations/<id>` or `folders/<id>`. Required. |
| `CLOUDPAM_GCP_ORG_REGIONS` | Optional. Falls back to `CLOUDPAM_GCP_REGIONS`. |
| `CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS` | Optional comma-separated list of project IDs to skip. |
| `CLOUDPAM_GCP_ORG_IMPERSONATE_SA` | Optional service-account email. The agent replaces `{project}` with each project ID. |

The YAML keys are `gcp_org.enabled`, `gcp_org.parent`, `gcp_org.regions`, `gcp_org.exclude_projects`, and `gcp_org.impersonate_service_account`.

Each pass lists every `ACTIVE` project under the parent, walking folders up to 10 levels deep. It then discovers the projects one at a time and sends one bulk ingest at the end. Each project has 10 minutes for its token and discovery calls. A project arrives in CloudPAM as the account keyed `gcp:<project>`, or attaches to the single existing account whose provider is `gcp` and whose external ID is the project ID.

A project that fails is reported to the server as a failure, and its existing inventory is left unchanged. The partial results of a failed project are dropped, so nothing goes stale because of one failed read. If the project listing fails, the agent reports one failure for the parent and ingests nothing. When the pass answers a server-requested sync, the sync job ends `failed` if any project failed.

Every project must have the Compute Engine API enabled. Exclude a project that does not, or it fails on every pass.

**Grant roles directly.** Without impersonation, the agent reads every project as itself. Grant its service account two roles on the organization or folder:

```bash
gcloud organizations add-iam-policy-binding 123456789 \
  --member="serviceAccount:cloudpam-agent@ops-project.iam.gserviceaccount.com" --role=roles/browser
gcloud organizations add-iam-policy-binding 123456789 \
  --member="serviceAccount:cloudpam-agent@ops-project.iam.gserviceaccount.com" --role=roles/compute.networkViewer
```

For a folder, use `gcloud resource-manager folders add-iam-policy-binding <folder-id>`.

**Impersonate a service account per project.** The agent always lists projects as itself, so it keeps `roles/browser` on the parent. The IAM Credentials API checks `generateAccessToken` against the caller's project, so enable `iamcredentials.googleapis.com` in the project that owns the agent's service account. Then, in each project, create the discovery service account, grant it `roles/compute.networkViewer`, and grant the agent `roles/iam.serviceAccountTokenCreator` on it:

```bash
gcloud services enable iamcredentials.googleapis.com --project=ops-project

PROJECT_ID=my-project
gcloud iam service-accounts create cloudpam-discovery --project="$PROJECT_ID"
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:cloudpam-discovery@${PROJECT_ID}.iam.gserviceaccount.com" --role=roles/compute.networkViewer
gcloud iam service-accounts add-iam-policy-binding "cloudpam-discovery@${PROJECT_ID}.iam.gserviceaccount.com" \
  --project="$PROJECT_ID" \
  --member="serviceAccount:cloudpam-agent@ops-project.iam.gserviceaccount.com" --role=roles/iam.serviceAccountTokenCreator
```

Then set `CLOUDPAM_GCP_ORG_IMPERSONATE_SA='cloudpam-discovery@{project}.iam.gserviceaccount.com'`. A value without `{project}` reads every project through one shared service account. In that case, grant token creator on that account once, and grant it `roles/compute.networkViewer` in each project.

The Discovery wizard generates these commands for the parent and template you enter. A Terraform module for GCP IAM is tracked in #416.

### Timeouts and retries

- `request_timeout` (default `30s`) bounds each GCP API request and each token fetch.
- Project listing and impersonated token requests retry `429`, `500`, `502`, `503`, and `504` responses and transport errors, up to 4 attempts. The backoff starts at 1 second and doubles, and a `Retry-After` header in seconds (capped at 30) replaces it.
- Compute calls are not retried within a pass. The next scheduled pass reads the project again.
- Errors carry the HTTP status but not the provider's response body. Token-endpoint response bodies never reach the agent's logs or the server's sync job messages.

### Run the GCP agent

Shell, for an organization with impersonation:

```bash
CLOUDPAM_BOOTSTRAP_TOKEN=eyJhZ2VudF9uYW1lIjoi... \
CLOUDPAM_AGENT_ID_FILE=/var/lib/cloudpam-agent/agent-id \
CLOUDPAM_ACCOUNT_ID=1 \
CLOUDPAM_PROVIDER=gcp \
CLOUDPAM_GCP_ORG_ENABLED=true \
CLOUDPAM_GCP_ORG_PARENT=organizations/123456789 \
CLOUDPAM_GCP_ORG_IMPERSONATE_SA='cloudpam-discovery@{project}.iam.gserviceaccount.com' \
./cloudpam-agent
```

Docker, with a service-account key. The image runs as UID 65532, so copy the key where only that user can read it. On a host with ADC (GCE metadata), leave out the key lines, the key mount, and `GOOGLE_APPLICATION_CREDENTIALS`. These commands assume a Linux host with rootful Docker.

```bash
sudo install -d -o 65532 -g 65532 /var/lib/cloudpam-agent
sudo install -d -o 65532 -g 65532 -m 0700 /etc/cloudpam-agent
sudo install -o 65532 -g 65532 -m 0400 key.json /etc/cloudpam-agent/gcp-key.json

docker run -d --name cloudpam-agent \
  -v /var/lib/cloudpam-agent:/var/lib/cloudpam-agent \
  -v /etc/cloudpam-agent/gcp-key.json:/var/run/secrets/cloudpam/gcp/key.json:ro \
  -e GOOGLE_APPLICATION_CREDENTIALS=/var/run/secrets/cloudpam/gcp/key.json \
  -e CLOUDPAM_BOOTSTRAP_TOKEN=eyJhZ2VudF9uYW1lIjoi... \
  -e CLOUDPAM_AGENT_ID_FILE=/var/lib/cloudpam-agent/agent-id \
  -e CLOUDPAM_ACCOUNT_ID=1 \
  -e CLOUDPAM_PROVIDER=gcp \
  -e CLOUDPAM_GCP_PROJECT=my-project \
  ghcr.io/badgerops/cloudpam/agent:latest
```

### Helm

Set `config.provider: gcp` and one of these:

- `config.gcpProject` for one project.
- `config.gcpOrg.enabled: true` with `config.gcpOrg.parent`, plus the optional `config.gcpOrg.regions`, `config.gcpOrg.excludeProjects`, and `config.gcpOrg.impersonateServiceAccount`.

`config.gcpRegions` and `config.discoveryGcpRps` apply in both modes. The chart fails to render when `config.provider` is `gcp` with neither `config.gcpProject` nor `config.gcpOrg.enabled`, when `config.gcpOrg.parent` is not `organizations/<id>` or `folders/<id>`, and when `config.awsOrg.enabled` is set with provider `gcp`.

On GKE, annotate the Kubernetes service account for Workload Identity:

```yaml
config:
  provider: gcp
  gcpOrg:
    enabled: true
    parent: organizations/123456789
serviceAccount:
  annotations:
    iam.gke.io/gcp-service-account: cloudpam-agent@ops-project.iam.gserviceaccount.com
```

Without Workload Identity, store the key in a secret under the key `key.json` and set `gcpCredentials.existingSecret` to the secret's name. The chart mounts it read-only and points `GOOGLE_APPLICATION_CREDENTIALS` at it.

### Troubleshooting

The agent logs JSON. The table quotes the `msg` field and the start of the `error` field.

| Log message and error | Cause | Fix |
|---|---|---|
| `failed to load config`, `provider is required (aws or gcp); set CLOUDPAM_PROVIDER or yaml` | `CLOUDPAM_PROVIDER` is not set. Agents before 0.25.0 assumed AWS. | Set `CLOUDPAM_PROVIDER=aws` or `gcp`. |
| `failed to load config`, `gcp_project is required when provider is gcp and gcp_org is off` | Single-project mode without a project. | Set `CLOUDPAM_GCP_PROJECT`, or turn on org mode. |
| `failed to load config`, `gcp_org.parent "..." must be organizations/<id> or folders/<id>` | The parent is missing or malformed. | Use the numeric ID, as in `organizations/123456789`. |
| `failed to list gcp projects`, `list projects: list projects under organizations/<id>: cloud request failed: http 403` | The agent lacks `resourcemanager.projects.list` on that node. The same error with `list folders under` means it lacks `resourcemanager.folders.list`. | Grant `roles/browser` on the organization or folder. |
| `discovery failed for project`, error containing `impersonate <email>: cloud request failed: http 403` | The agent lacks `roles/iam.serviceAccountTokenCreator` on that project's service account, the account does not exist, or `iamcredentials.googleapis.com` is disabled in the agent's own project. | Create the account, grant token creator, and enable the IAM Credentials API. |
| `discovery failed for project`, `discover networks for project <id>: cloud request failed: http 403` | The identity lacks `roles/compute.networkViewer`, or the Compute Engine API is disabled in that project. | Grant the role, enable `compute.googleapis.com`, or exclude the project. |
| `discovery failed for project`, error ending in `context deadline exceeded` | The project ran past its 10-minute limit or a request ran past `request_timeout`. | Check the network path to `compute.googleapis.com`, or exclude the project. |
| `gcp credentials failed for project`, `credentials: gcp default credentials: ...` | The agent found no ADC to impersonate from. | Configure Workload Identity, a key at `GOOGLE_APPLICATION_CREDENTIALS`, or a VM service account. |
| `push org resources failed`, `server accepted none of the <n> accounts: ...` ending in `(upgrade the CloudPAM server to 0.25.0 or later before GCP agents)` | The server is older than 0.25.0 and rejects GCP entries. | Upgrade the server. |
| `push org resources failed`, `server did not acknowledge <n> account failure(s); upgrade the CloudPAM server to 0.25.0 or later before upgrading agents` | The server is older than 0.25.0 and ignores `failures`. | Upgrade the server. |
| `org sync finished with failures` | Some projects failed and were reported to the server. A server-requested sync job ends `failed`. | Read the per-project `discovery failed for project` lines. For a server-requested sync, the job's error message in Sync History lists them too. |

### Known limitations

- A server-requested sync job stays `running` if the agent restarts or crashes during the pass. Later heartbeats claim only `pending` jobs. Tracked in #417.
- In single-project mode, a discovery failure is logged but not reported to the server, so a server-requested job for that account stays `running`. Org mode reports failures. Tracked in #418.
- An org pass reads projects one at a time and pushes once at the end. A large organization can run for a long time with nothing reaching the server until the pass ends, and there is no overall pass deadline. Tracked in #419.

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

### Test Account Connectivity

Before a sync, check that the server can reach an account. The test makes the
provider's cheapest authenticated read (AWS `sts:GetCallerIdentity`, GCP
`compute.projects.get`) as the identity a server-side sync of that account
would use: the account's stored credential when one exists, otherwise the
server's own identity (AWS default credential chain, GCP Application Default
Credentials). It needs `discovery:create`, takes at most 15 seconds, and writes
an audit event with action `test` on the account.

```bash
curl -X POST http://localhost:8080/api/v1/accounts/1/test
```

A failing account still returns 200, with `status: "failed"` and a sanitized
`error` that names the provider call:

```json
{
  "tested_at": "2025-01-15T10:29:58Z",
  "status": "ok",
  "latency_ms": 184,
  "identity": "arn:aws:sts::123456789012:assumed-role/CloudPAMDiscovery/i-0abc",
  "source": "host"
}
```

`source` is `host` or `stored_credential`. The endpoint returns 404 for an
unknown account and 422 for a provider no collector can test. The test checks
authentication and reachability, not the EC2 or Compute read permissions a sync
also needs. The test runs from the server, so it says nothing about a
standalone agent, which uses its own credentials.

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

### List Each Account's Latest Sync Job

Returns one job per account that has synced at least once, in a single response. The dashboard uses it to show sync health for every account without one request per account.

```bash
curl http://localhost:8080/api/v1/discovery/sync/latest
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
    "hostname": "ip-10-0-1-42",
    "claim_sync": true
  }'
```

The response carries `sync_job_id` and `account_id` when the server hands the agent a pending sync job. `claim_sync` is optional and defaults to `true`. With `false`, the server leaves the job pending for a later heartbeat. Agents from 0.25.0 send `false` while they already hold a requested job, so each agent holds at most one job it has not started.

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

Auth is always on. Discovery endpoints require the `discovery` resource permission:

| Action | Required Permission | Roles |
|--------|-------------------|-------|
| List resources, view sync jobs, list agents | `discovery:read` | admin, operator, viewer |
| Trigger sync, provision agents | `discovery:create` | admin, operator |
| Link/unlink, approve/reject agents | `discovery:update` | admin, operator |

## Frontend

The Discovery page is at `/discovery` in the CloudPAM UI. It has four tabs:

- **Resources** — table of discovered cloud resources with search + filters (type, status, linked). Click the link/unlink icons in the Actions column to manage pool associations.
- **Network** — merged view of managed objects and discovered resources, with conflict review and schema-policy scoping.
- **Sync History** — table of past sync jobs showing status, timing, and resource counts.
- **Agents** — table of registered discovery agents with health status, version, hostname, and last heartbeat time. Auto-refreshes every 30 seconds.

Select a cloud account from the dropdown, then click **Sync Now** to run discovery. The setup wizard sets up an AWS or GCP agent, for one account or for an organization. GCP accounts can also be synced in-process when the server has Application Default Credentials; Azure accounts can be created but have no collector. See [CURRENT_STATUS.md](CURRENT_STATUS.md).

## Architecture

### Collector Interface

```go
type Collector interface {
    Provider() string
    Discover(ctx context.Context, account domain.Account) ([]domain.DiscoveredResource, error)
}
```

Each cloud provider implements this interface. AWS and GCP collectors are registered at startup in `cmd/cloudpam/main.go`. The standalone agent (`cmd/cloudpam-agent`) picks the AWS or GCP collector from `CLOUDPAM_PROVIDER`.

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

The `DiscoveryStore` interface (`internal/storage/discovery.go`) is implemented for in-memory, SQLite, and PostgreSQL backends.

## Adding a New Cloud Provider

GCP is already implemented (`internal/discovery/gcp/`) and registered next to AWS. Azure is the remaining collector.

To add Azure (or another provider):

1. Create `internal/discovery/azure/collector.go`
2. Implement the `Collector` interface
3. Register the collector in `cmd/cloudpam/main.go` (and in `cmd/cloudpam-agent` if the agent should run it)
4. Create accounts with `"provider": "azure"` — the sync service selects the collector by `account.Provider`

The HTTP API and storage layer are provider-agnostic. The agent's provider setting, the bulk org ingest's `provider` check, the Helm chart's provider check, and the Discovery wizard accept only `aws` and `gcp`, so each needs matching work. Account keys for `azure:` already validate as subscription UUIDs; without a collector, sync fails.


## Scheduled server discovery

Schedules are opt-in per account. Read or configure them through
`GET/PUT /api/v1/discovery/schedules/{accountId}`. Reading requires
`discovery:read`; changing an interval requires `discovery:create`, the same
permission as triggering a manual sync. Each change writes an account audit event.

```bash
curl -X PUT http://localhost:8080/api/v1/discovery/schedules/1 \
  -H "Authorization: Bearer $CLOUDPAM_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"interval_seconds":900}'
```

The response contains `account_id`, `interval_seconds`, and `next_run_at` (Unix
seconds). Use 0 to disable, or an interval from 60 seconds through 7 days. The
first run is due one interval after saving. Only AWS and GCP can be enabled.
There is no schedule editor in the UI yet.

The server checks due schedules every 30 seconds and processes them sequentially.
A slow account can delay other accounts. It skips deleted accounts and accounts
with a healthy assigned approved agent (heartbeat within five minutes; legacy
agents with no approval state are also recognized). Legacy AWS account-number
assignments are recognized. Successful agent ingest also records account ownership,
including organization members served by one agent. A healthy recorded agent
suppresses that account’s schedule. An unrelated agent does not suppress the schedule.
Agent-list failures prevent scheduled execution. Agents retain their own timers.

A scheduled run uses the account's stored credential when present, otherwise the
server identity, exactly as a manual server sync does. A stored-credential failure
never falls back to the host. Schedule state survives restarts on SQLite and
PostgreSQL. Missed intervals are coalesced into one run; the next due time advances
when a run is claimed, including a manual server run on a scheduled account.
Disabling a schedule prevents future runs but does not cancel one already claimed.

Manual and scheduled server runs share an atomic database lease per account.
An overlapping manual request returns 409. Runs have a ten-minute context deadline;
leases expire after fifteen minutes for crash recovery. All replicas must have
synchronized clocks. Shutdown cancels and joins the scheduler before closing storage.
Leases do not coordinate agent ingest during an agent/server ownership transition.
A hard process crash can leave the old job displayed as running until an operator
reviews it; the expired lease permits a later run.

### Retry and incremental persistence

Server collector failures retry at most twice (three attempts total), waiting one
then two seconds. Network timeouts, connection resets/refusals, broken pipes, HTTP 429/5xx, and recognized
AWS throttling/server errors qualify. Permission/configuration errors, cancellation,
and persistence failures do not. Provider SDKs may also perform their own retries.
Attempts are saved before each collector call. Retries discard partial inventories;
resources are processed only after a successful full collection.

Sync jobs keep `source: local|agent` and add:

- `identity_source`: `host`, `stored_credential`, or `agent`.
- `trigger`: `manual`, `scheduled`, or `agent`.
- `attempts`: collector calls made by the server (agent runs report one ingest;
  this does not count retries internal to an agent).

Older job rows have empty new metadata fields and zero attempts.

Incremental means **persistence**, not provider delta queries. Each successful
collection compares cloud-owned fields to the stored inventory. Unchanged rows
only refresh `last_seen_at`; `updated_at` tracks observed field or stale-status
changes, and `discovered_at`, IDs, and managed-pool links are preserved.
`resources_updated` counts changed rows, including resources returning from stale
state. Missing resources become stale only after all writes succeed. A database
failure can leave some resource writes applied, but never triggers stale marking
for that incomplete snapshot; the next successful run reconciles it.
