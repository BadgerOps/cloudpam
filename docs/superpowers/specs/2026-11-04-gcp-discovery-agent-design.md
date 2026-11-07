# GCP support in the discovery agent: design

Issue: #274. Follow-up: #416 (Terraform modules move to their own repository, including a GCP module).

## Problem

`cmd/cloudpam-agent` can only discover AWS. It hardcodes `aws.New()` for single-account mode and
`aws.ListOrgAccounts` + `aws.AssumeRole` for organization mode. GCP discovery exists only as the
server's in-process collector (`internal/discovery/gcp`), which runs as the server host's
Application Default Credentials or a stored per-account credential (#275). There is no way to run
discovery next to a GCP environment, and no way to cover every project in a GCP organization
without creating each account by hand.

## Goals

1. The agent discovers a single GCP project, using the same ingest, heartbeat, server-requested
   sync and retry path as AWS.
2. The agent discovers every active project under a GCP organization or folder and bulk-ingests
   them, auto-creating `gcp:<project>` accounts, the GCP counterpart of AWS org mode.
3. Org mode supports two identity models:
   - **Direct**: the agent's own identity is granted roles once on the organization or folder and
     IAM inheritance covers every project.
   - **Impersonation**: the agent mints a short-lived token for a per-project (or shared) service
     account, the counterpart of AWS `AssumeRole`.
4. Nothing defaults the cloud provider. The agent, the Helm chart and the org-ingest API all
   require it to be stated, so a third provider can be added later without guessing.
5. The Discovery wizard generates working GCP configs and the IAM commands to go with them.

## Non-goals

- Terraform for GCP IAM (tracked in #416, which moves all discovery Terraform to its own repo).
- Agents using stored per-account credentials (#275 scoped those to the server's in-process sync).
- Azure.
- Discovering more GCP resource types than the collector already covers (networks, subnetworks,
  external addresses).
- Shared VPC host/service-project relationships beyond what the collector already reports.

## Breaking change

The agent no longer assumes AWS. `CLOUDPAM_PROVIDER` (`provider:` in YAML) is required and must
be `aws` or `gcp`. Existing AWS agents fail at startup after upgrade until they set
`CLOUDPAM_PROVIDER=aws`. The Helm chart refuses to render without `config.provider`, so chart
users see the error at `helm upgrade` time. The changelog entry is marked **Breaking** with the
one-line fix.

The server side is not breaking: existing agents already send `provider: "aws"` on every
org-ingest entry, and `aws_account_id` keeps working as an alias.

## Part 1: agent

### Configuration

New `Config` fields, each with a YAML key and an environment variable (env overrides YAML, as
today):

| YAML | Env | Meaning |
|---|---|---|
| `provider` | `CLOUDPAM_PROVIDER` | Required. `aws` or `gcp`. No default. |
| `gcp_project` | `CLOUDPAM_GCP_PROJECT` | GCP project ID. Required when `provider=gcp` and GCP org mode is off. |
| `gcp_regions` | `CLOUDPAM_GCP_REGIONS` | Optional comma list; filters subnetworks and addresses. Empty means all regions. |
| `discovery_gcp_rps` | `CLOUDPAM_DISCOVERY_GCP_RPS` | Cap on GCP API calls per second (Compute, Resource Manager, IAM Credentials). Same parsing and bounds as `discovery_aws_rps`. |
| `gcp_org.enabled` | `CLOUDPAM_GCP_ORG_ENABLED` | `true`/`1` enables GCP org mode. |
| `gcp_org.parent` | `CLOUDPAM_GCP_ORG_PARENT` | Required in GCP org mode. `organizations/<digits>` or `folders/<digits>`. |
| `gcp_org.regions` | `CLOUDPAM_GCP_ORG_REGIONS` | Falls back to `gcp_regions`. |
| `gcp_org.exclude_projects` | `CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS` | Project IDs to skip. |
| `gcp_org.impersonate_service_account` | `CLOUDPAM_GCP_ORG_IMPERSONATE_SA` | Optional. Empty means direct mode. Otherwise a service-account email, where the literal `{project}` is replaced by each project ID (for example `cloudpam-discovery@{project}.iam.gserviceaccount.com`). Without `{project}`, every project is read through the same service account. |

Validation in `Config.Validate()` (each is a startup error with an actionable message):

- `provider` empty: `provider is required (aws or gcp); set CLOUDPAM_PROVIDER or yaml`.
- `provider` not `aws` or `gcp`: `unsupported provider "<v>" (aws or gcp)`.
- `aws_org.enabled` with `provider` other than `aws`, or `gcp_org.enabled` with `provider` other
  than `gcp`: error naming both settings.
- `provider=gcp`, org mode off, `gcp_project` empty: error.
- `gcp_org.enabled`, `parent` not matching `^(organizations|folders)/[0-9]+$`: error.
- `impersonate_service_account` set but not shaped like an email after substitution: error.
- `discovery_gcp_rps` checked with `discovery.CheckRPS`, as the AWS one is.
- `account_id` stays required in every mode (it is the account the agent heartbeats as).

GCP credentials come from Application Default Credentials only: GKE Workload Identity, a key file
at `GOOGLE_APPLICATION_CREDENTIALS`, or the metadata server on a GCE VM. The agent adds no
credential settings of its own.

### Single-project mode

`provider=gcp`, org mode off. The agent builds
`domain.Account{ID: account_id, Provider: "gcp", ExternalID: gcp_project, Regions: gcp_regions}`,
creates `gcp.New()` with a limiter from `discovery_gcp_rps`, and runs the existing `runSync` path
unchanged: `Discover`, then `PushResources` with retries. Heartbeats and server-requested syncs
behave as they do for AWS.

`runSync` stops hardcoding `Provider: "aws"`. The AWS single-account path builds
`Account{ID, Provider: "aws", Regions: aws_regions}` as today.

### Org mode

`provider=gcp`, `gcp_org.enabled`. One sync pass:

1. **List projects.** As the agent's own ADC identity (scope
   `https://www.googleapis.com/auth/cloud-platform.read-only`), walk the hierarchy under `parent`:
   - `GET https://cloudresourcemanager.googleapis.com/v3/projects?parent=<node>` for projects
     directly under the node, following `nextPageToken`.
   - `GET https://cloudresourcemanager.googleapis.com/v3/folders?parent=<node>` for child
     folders, following `nextPageToken`, and recurse into each.
   - Keep projects with `state == "ACTIVE"`. Deduplicate by `projectId`. Guard the walk with a
     visited set and a depth limit of 10 (GCP's own folder nesting limit) so a malformed response
     cannot loop.
   - A failure to list any node fails the whole pass with an error log and no push. A partial
     project list would leave projects silently undiscovered, and an org pass is retried on the
     next tick anyway.
   - The agent's identity needs `resourcemanager.projects.list` and
     `resourcemanager.folders.list` on the parent (`roles/browser`) in **both** identity modes.
2. **Drop excluded projects** (`exclude_projects`), logging each one.
3. **Discover each project.**
   - **Direct mode** (no template): `gcp.New()` with the agent's ADC. The identity needs
     `roles/compute.networkViewer`, normally inherited from a grant on the parent.
   - **Impersonation mode**: build a token source that calls
     `POST https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/<email>:generateAccessToken`
     with `{"scope":["https://www.googleapis.com/auth/compute.readonly"],"lifetime":"3600s"}`,
     authenticated as the agent's ADC with scope
     `https://www.googleapis.com/auth/cloud-platform`. Wrap it in `oauth2.ReuseTokenSource` and
     pass it to `gcp.NewWithTokenSource`. The agent's identity needs
     `roles/iam.serviceAccountTokenCreator` on each target service account; each target needs
     `roles/compute.networkViewer` on its project.
   - All collectors in the pass share one limiter, so Compute, Resource Manager and IAM
     Credentials calls draw from one `discovery_gcp_rps` budget.
   - If minting the token or discovery fails for a project, log it with the project ID and skip
     that project. `Discover` can return partial results with an error; those are dropped, not
     pushed, because pushing an incomplete inventory would mark the missing resources stale. This
     matches AWS org mode's handling of a failed `AssumeRole`.
4. **Push.** One `PushOrgResources` call with an entry per discovered project:
   `{provider: "gcp", external_id: <projectId>, account_name: <displayName>, regions, resources}`.
   No push when no project succeeded.

`runOrgSync` becomes `runAWSOrgSync`, and the AWS entries switch from `aws_account_id` to
`external_id`. The scheduler picks `runAWSOrgSync`, `runGCPOrgSync` or `runSync` from
`provider` and the org flags.

### Code layout

| File | Contents |
|---|---|
| `internal/discovery/gcp/resourcemanager.go` | `ListProjects(ctx, client *http.Client, parent string, limiter *discovery.Limiter) ([]Project, error)` with the folder/project walk; `Project{ID, Name, Number}`. Base URL is a package variable so tests can point it at `httptest`. |
| `internal/discovery/gcp/impersonate.go` | `ImpersonatedTokenSource(ctx, base oauth2.TokenSource, email string, limiter) oauth2.TokenSource` making the `generateAccessToken` call over plain REST (no `google.golang.org/api` dependency; the repo already avoids it). Also `ServiceAccountFor(template, project string) string`. |
| `cmd/cloudpam-agent/config.go` | New fields, env loading, validation above. |
| `cmd/cloudpam-agent/main.go` | Collector selection by provider; `runAWSOrgSync`; new `runGCPOrgSync`. |
| `cmd/cloudpam-agent/gcp_org.go` | `runGCPOrgSync`, kept out of `main.go`, which is already the AWS org path's home. |

## Part 2: server org ingest

Only `POST /api/v1/discovery/ingest/org` changes. Single-account ingest, heartbeats and
server-requested syncs already work for any provider.

### Payload

`domain.OrgAccountIngest` gains:

```go
ExternalID string `json:"external_id"`
```

Per-entry handling in `handleOrgIngest`:

1. `provider` is required. Empty or not `aws`/`gcp` produces a per-entry error
   (`skipped account: provider is required` / `skipped account: unsupported provider "<v>"`) and
   the entry is skipped. The current `provider = "aws"` default is removed.
2. The external ID is `external_id`, falling back to the deprecated `aws_account_id`. Both set and
   different is a per-entry error. Neither set is a per-entry error.
3. The account key is `<provider>:<external_id>`. AWS keys are unchanged (`aws:<id>`); GCP keys
   are `gcp:<project>`, which `gcp.ProjectID()` already strips.

### Account matching

1. `GetAccountByKey(<provider>:<external_id>)`.
2. On not-found, scan `ListAccounts` for accounts with the same `Provider` and `ExternalID`:
   - exactly one: use it;
   - more than one: per-entry error `ambiguous accounts for <provider> <external_id>: ids …`, entry
     skipped;
   - none: auto-create as today, with `Provider` and `ExternalID` from the entry.

The fallback applies to AWS as well. A hand-created AWS account whose external ID matches an org
member account now receives that member's resources instead of a duplicate `aws:<id>` account
being created. This is intended and goes in the changelog.

No storage interface change and no migration.

Unchanged: sync-job completion, agent last-seen refresh, `recordSyncAgent`, the response shape.

### OpenAPI

Update the org-ingest route metadata in `internal/api/openapi.go` (summary "Ingest AWS Organizations resources" becomes provider-neutral): document `external_id`, mark
`aws_account_id` deprecated, mark `provider` required, and describe the matching order.

## Part 3: wizard, Helm, docs

### Discovery wizard

`ui/src/components/DiscoveryWizard.tsx`:

- **Step 1** starts with a provider choice (AWS / GCP). Mode cards read "Single Account / AWS
  Organization" for AWS and "Single Project / GCP Organization or Folder" for GCP.
- **Single mode** lists only accounts of the chosen provider; a new account defaults to it. For
  GCP the project ID is the account's external ID, else its key with any `gcp:` prefix removed;
  if neither yields a value, the wizard asks for the project ID.
- **GCP org fields**: parent (validated against the same pattern as the agent), regions, excluded
  projects, optional impersonation template with a hint that empty means direct grants.
- **Tabs**:
  - AWS: unchanged (shell, YAML, Terraform, Docker, and IAM in org mode).
  - GCP: shell, YAML, Docker, IAM in both modes. No Terraform tab until #416.
- **GCP IAM tab** shows the `gcloud` commands for the case at hand:
  - single project: `roles/compute.networkViewer` on the project;
  - org direct: `roles/browser` and `roles/compute.networkViewer` on the org or folder;
  - org impersonation: `roles/browser` on the parent; per project, create the service account,
    grant it `roles/compute.networkViewer`, and grant the agent identity
    `roles/iam.serviceAccountTokenCreator` on it.
  - The agent's identity in the commands is a placeholder the operator replaces
    (`AGENT_SA_EMAIL`), since the wizard cannot know it.
- **Every generated config sets `CLOUDPAM_PROVIDER`**, AWS ones included.
- **Refactor**: config strings move out of the component into pure builders in
  `ui/src/components/discoveryWizardConfig.ts` (input: provider, mode, form values, provision
  result; output: content per tab). The component keeps state and layout. Only code this change
  touches moves.

### Helm agent chart

`deploy/helm/cloudpam-agent`:

- `config.provider` has no default; `templates/` fails rendering with
  `config.provider is required (aws or gcp)` when it is empty or unsupported.
- New values: `config.gcpProject`, `config.gcpRegions`, `config.discoveryGcpRps`,
  `config.gcpOrg.{enabled, parent, regions, excludeProjects, impersonateServiceAccount}`, mapped
  to the env vars above in the configmap.
- `gcpCredentials.existingSecret` (key `key.json`): mounts the secret read-only and sets
  `GOOGLE_APPLICATION_CREDENTIALS`. Omitted when using Workload Identity.
- Workload Identity annotation example
  (`iam.gke.io/gcp-service-account: cloudpam-agent@<project>.iam.gserviceaccount.com`) beside the
  IRSA example.
- `ci/full-values.yaml` sets `provider: aws`; new `ci/gcp-org-values.yaml` covers the GCP org
  render with a key secret.
- Bump chart version and `appVersion` for both charts with the changelog version (Helm CI
  requires it).

### Docs

- `docs/DISCOVERY.md`: new "GCP Agent" section covering both modes, both identity models with the
  exact role bindings, Workload Identity and key-file setup, and a troubleshooting table
  (permission denied on project listing, token creator missing, Compute API disabled in a
  project). Add the GCP agent rows to the "How discovery reaches an account" table.
- `docs/DISCOVERY_AGENT_PLAN.md`: mark the GCP agent as built.
- `docs/CHANGELOG.md`: feature entry plus the **Breaking** note for `CLOUDPAM_PROVIDER`, and the
  org-ingest matching change.
- `CLAUDE.md`: agent environment variables gain `CLOUDPAM_PROVIDER` and the GCP settings.

## Testing

Go:

- `config` tests: provider required, unsupported provider, cross-provider org flags, GCP project
  required, parent pattern, impersonation template substitution, GCP RPS bounds, env overrides.
- `gcp/resourcemanager` tests against `httptest`: nested folders, paging on both endpoints,
  non-`ACTIVE` projects dropped, duplicates removed, depth guard, a listing error failing the
  call.
- `gcp/impersonate` tests: request body and target URL, token parsing and expiry, error status
  surfaced, `{project}` substitution.
- Agent org-sync test: fake Resource Manager and Compute servers, one project failing discovery is
  skipped and the rest are pushed with `provider: "gcp"` and `external_id`.
- `handleOrgIngest` tests: GCP entry creates `gcp:<project>`; legacy `aws_account_id`; conflicting
  `external_id`/`aws_account_id`; missing and unknown provider; fallback match on provider +
  external ID; ambiguous fallback.
- OpenAPI validation (`just openapi-validate`).

Frontend (Vitest):

- Builders: each provider × mode × tab produces the expected settings, always including
  `CLOUDPAM_PROVIDER`; GCP IAM commands per identity mode.
- Wizard: switching provider filters accounts, changes mode labels and the tab set; a stale tab
  selection falls back (`resolveConfigTab`).

End to end:

- `verify-cloudpam`: walk the wizard's GCP single-project and GCP org flows and capture
  screenshots of the generated config and IAM tabs.
- `helm template` for the AWS and GCP CI values, and the failure without `config.provider`.

Known gap: no CI run against a real GCP organization. Resource Manager, IAM Credentials and
Compute are exercised through `httptest` fakes that follow the documented REST shapes.
