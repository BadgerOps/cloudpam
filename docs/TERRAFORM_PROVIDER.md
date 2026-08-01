# CloudPAM Terraform Provider

The CloudPAM Terraform provider manages IP address pools and cloud accounts through
the CloudPAM REST API, so your IPAM allocations can live alongside the infrastructure
that consumes them.

Source: [`terraform-provider-cloudpam/`](../terraform-provider-cloudpam). It is a
**separate Go module** on purpose — the `terraform-plugin-framework` dependency tree is
large, and the `cloudpam` server binary must stay lean. `go build ./...` from the repo
root does not reach it; CI builds and tests it in the dedicated
`test-terraform-provider` job in `.github/workflows/test.yml`.

## Scope (v1)

| Type | Name | Purpose |
|------|------|---------|
| Resource | `cloudpam_pool` | Create / read / update / delete pools (`/api/v1/pools`) |
| Resource | `cloudpam_account` | Create / read / update / delete accounts (`/api/v1/accounts`) |
| Data source | `cloudpam_pool` | Look up one pool by `id` or `cidr` |
| Data source | `cloudpam_pools` | List pools, optionally filtered |
| Data source | `cloudpam_account` | Look up one account by `id` or `key` |
| Data source | `cloudpam_accounts` | List accounts, optionally filtered |

Deliberately **not** covered in v1: discovery, drift, analysis, recommendations,
AI planning, OIDC provider administration, security settings, users and API keys.
Those APIs are either read-only, operational rather than declarative, or manage
credentials that do not belong in Terraform state.

## Provider configuration

```hcl
terraform {
  required_providers {
    cloudpam = {
      source  = "BadgerOps/cloudpam"
      version = "~> 0.1"
    }
  }
}

provider "cloudpam" {
  endpoint = "https://cloudpam.example.com" # or CLOUDPAM_ENDPOINT
  api_key  = var.cloudpam_api_key           # or CLOUDPAM_API_KEY
}
```

Both attributes fall back to environment variables, which is the recommended way to
supply the key so it never lands in a `.tf` file:

| Attribute | Environment variable |
|-----------|----------------------|
| `endpoint` | `CLOUDPAM_ENDPOINT` |
| `api_key`  | `CLOUDPAM_API_KEY` |

```bash
export CLOUDPAM_ENDPOINT=https://cloudpam.example.com
export CLOUDPAM_API_KEY=cpam_...
terraform plan
```

### Authentication

The provider sends `Authorization: Bearer <api_key>` on every request, which is exactly
what the server's `DualAuthMiddleware` expects. Keys must carry the `cpam_` prefix; the
provider emits a warning at configure time if yours does not, because the server rejects
any other bearer token format. API-key requests are exempt from CSRF (no cookies are
involved), so no extra headers are needed.

Create a key in the UI under **Settings → API Keys**, or via the API. The provider needs
scopes covering the resources it manages:

```bash
curl -X POST "$CLOUDPAM_ENDPOINT/api/v1/auth/keys" \
  -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $CSRF" -b cookies.txt \
  -d '{"name":"terraform","scopes":["pools:write","accounts:write"]}'
```

The plaintext key is returned **once**, at creation.

`endpoint` may include a trailing `/api/v1`; the provider normalises it away.

## Examples

### An account and a pool hierarchy

```hcl
resource "cloudpam_account" "prod" {
  key            = "aws:123456789012"
  name           = "Production"
  cloud_provider = "aws"
  external_id    = "123456789012"
  environment    = "production"
  tier           = "prod"
  regions        = ["us-east-1", "us-west-2"]
}

resource "cloudpam_pool" "supernet" {
  name        = "Global Supernet"
  cidr        = "10.0.0.0/8"
  type        = "supernet"
  description = "Top-level RFC1918 space"
}

resource "cloudpam_pool" "us_east_prod" {
  name       = "us-east-1 prod"
  cidr       = "10.10.0.0/16"
  parent_id  = cloudpam_pool.supernet.id
  account_id = cloudpam_account.prod.id
  type       = "region"
  status     = "active"

  tags = {
    managed_by = "terraform"
    team       = "network"
  }
}
```

### Reading existing data

```hcl
data "cloudpam_pool" "by_cidr" {
  cidr = "10.10.0.0/16"
}

data "cloudpam_pools" "prod_pools" {
  account_id = cloudpam_account.prod.id
  status     = "active"
}

data "cloudpam_account" "prod" {
  key = "aws:123456789012"
}

data "cloudpam_accounts" "aws" {
  cloud_provider = "aws"
}

output "prod_pool_cidrs" {
  value = [for p in data.cloudpam_pools.prod_pools.pools : p.cidr]
}
```

## Behaviour worth knowing

### `cloud_provider`, not `provider`

`provider` is a reserved meta-argument inside Terraform resource blocks, so the
account attribute is named `cloud_provider`. It maps to the API's `provider` field.

### Immutable attributes force replacement

The CloudPAM API cannot change these in place, so editing them replaces the resource:

- `cloudpam_pool`: `cidr`, `parent_id`, `source`
- `cloudpam_account`: `key`

### `account_id` clears in place

`PATCH /api/v1/pools/{id}` distinguishes three states for `account_id`: an absent key
keeps the current assignment, an explicit `null` clears it, and a number assigns it.
The provider always sends the key on update, emitting `null` when the attribute is
removed from configuration — so deleting `account_id` from your `.tf` actually
unassigns the pool instead of silently leaving the old account in place.

### Optional fields clear rather than linger

`description`, `tags`, and the optional account string fields default to empty. The
account API replaces its optional fields wholesale on every `PATCH`, so the provider
sends all of them on each update; removing one from configuration clears it server-side.

### Deleting parents and in-use accounts

CloudPAM answers `409 Conflict` when deleting a pool that still has children or an
account still referenced by pools. Set `force_destroy = true` to cascade
(`?force=true` on the API call):

```hcl
resource "cloudpam_pool" "supernet" {
  name          = "Global Supernet"
  cidr          = "10.0.0.0/8"
  force_destroy = true
}
```

### Drift

If a pool or account is deleted outside Terraform, the next refresh removes it from
state (404 on read) and the plan recreates it, rather than erroring.

## Import

Both resources import by their numeric CloudPAM ID:

```bash
terraform import cloudpam_pool.supernet 12
terraform import cloudpam_account.prod 3
```

## Building and installing locally

All commands assume the Nix dev shell (`nix develop`).

```bash
# Build and test the module
just tf-provider-build
just tf-provider-test

# Or directly
cd terraform-provider-cloudpam
go build ./...
go test ./...
```

To use the locally built provider from a Terraform configuration, install it into the
local plugin mirror:

```bash
just tf-provider-install            # defaults to version 0.1.0
just tf-provider-install version=0.2.0
```

That builds the binary into
`~/.terraform.d/plugins/registry.terraform.io/BadgerOps/cloudpam/<version>/<os>_<arch>/`,
where `terraform init` picks it up for `source = "BadgerOps/cloudpam"`.

Alternatively, use a dev override in `~/.terraformrc` to skip `terraform init`
entirely (note that dev overrides make Terraform ignore `required_providers` versions):

```hcl
provider_installation {
  dev_overrides {
    "BadgerOps/cloudpam" = "/absolute/path/to/cloudpam/terraform-provider-cloudpam"
  }
  direct {}
}
```

## Tests

Unit tests run everywhere and need nothing external — the API client is exercised
against `httptest` servers that mimic CloudPAM's request and response shapes.

```bash
just tf-provider-test
```

Acceptance tests drive a real `terraform` binary against a live CloudPAM server. They
are gated behind `TF_ACC`, so they skip during normal `go test ./...` runs and in CI:

```bash
# Terminal 1
just dev   # or any CloudPAM server

# Terminal 2 — create an API key first, then:
export TF_ACC=1
export CLOUDPAM_ENDPOINT=http://localhost:8080
export CLOUDPAM_API_KEY=cpam_...
just tf-provider-testacc
```

Acceptance tests create real pools and accounts (`tfacc-*` names, `10.240.0.0/16` and
`10.241.0.0/16` space) and destroy them at the end. Point them at a scratch server, not
production.
