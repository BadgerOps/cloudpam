# CloudPAM Current Status and Known Gaps

Validated against `master` at commit `e0818fd` (**0.23.4** plus #315) and the changes queued in #316–#319 and #322 (2026-10-30). #315 stores PostgreSQL recommendations in a `recommendations` table. It is not in a release yet, so a 0.23.4 binary on PostgreSQL still keeps recommendations in memory and loses them on restart.

> **Merge dependency for #320:** merge #316–#319 and #322 first. This snapshot already treats those changes as applied; if any dependency is dropped, restore its corresponding gap before merging this documentation.

This is the source of truth for what is implemented versus what is still missing. Week-by-week checkboxes in [IMPLEMENTATION_ROADMAP.md](IMPLEMENTATION_ROADMAP.md), architecture sketches in [SMART_PLANNING.md](SMART_PLANNING.md) / [DATABASE_SCHEMA.md](DATABASE_SCHEMA.md) / [OBSERVABILITY.md](OBSERVABILITY.md), and the October 2025 [REVIEW.md](REVIEW.md) are **not** current status unless they agree with this file.

Product version and user-facing changes live in [CHANGELOG.md](CHANGELOG.md). [CLEANUP.md](CLEANUP.md) sorts the other docs in `docs/` into current, design, and historical.

---

## What is implemented

| Area | Status | Notes |
|------|--------|-------|
| Core IPAM | Complete | Hierarchical pools, accounts, assigned blocks, CIDR search, CSV import/export, validation, audit. Every endpoint that creates pools (pool create, schema apply, CSV import, AI plan apply, recommendation apply, discovery import) checks parent containment and sibling overlap under a lock that spans replicas on PostgreSQL and one process on SQLite or in memory. Schema apply and AI plan apply skip the overlap check when `skip_conflicts` is true |
| Discovery: AWS | Complete | Single-account and Organizations, AssumeRole, standalone agent, ingest/heartbeat, bootstrap register/approve, IAM Terraform/CloudFormation helpers |
| Discovery: GCP | Partial | Server collector for networks, subnetworks, and external IPs. No org mode, no agent, no IaC helpers. Uses Application Default Credentials on the CloudPAM host |
| Discovery: Azure | Missing | Account keys accept `azure:<subscription-uuid>` and the UI offers Azure; there is no collector. Sync fails with `no collector registered for provider` |
| Drift | Complete | Detect, list, resolve, ignore. No automatic reconciliation suggestions |
| Network objects | Complete | Merged network view, conflicts, persisted objects and relationships. `DELETE` returns `409` while child objects or relationships still reference the object |
| Analysis engine | Complete as API | `POST /api/v1/analysis`, `/gaps`, `/fragmentation`, `/compliance`. `/fragmentation` and the full report score fragmentation for only the first pool. **No UI** |
| Recommendations | Partial | Allocation and compliance only. Consolidation / resize / reclaim are commented stubs. Stored in a table on SQLite and PostgreSQL (#315) |
| Schema Planner | Complete, one gap | Templates, conflict check, apply. Terraform export writes `cloudpam_pool` resources (0.23.1). Apply does not check that a child CIDR sits inside its declared parent (#284) |
| Terraform provider | Complete for v1 | `cloudpam_pool` and `cloudpam_account` resources plus matching data sources. Not discovery, drift, users, or AI. See [TERRAFORM_PROVIDER.md](TERRAFORM_PROVIDER.md) |
| AI planning | Complete, optional | OpenAI-compatible backend via `CLOUDPAM_LLM_*`. SSE chat, sessions, plan extract/apply. Env-only config; no admin provider registry |
| Auth | Complete | Local users, sessions, API keys (`Authorization: Bearer cpam_…`), custom-role RBAC, OIDC/SSO with JIT and group mapping. Auth is always on. **No MFA** |
| Observability | Partial | slog, custom Prometheus `/metrics`, Sentry, opt-in OTLP/HTTP tracing (`CLOUDPAM_TRACING_ENABLED`). Vector sidecar configs exist but are not integrated. `deploy/docker-compose.observability.yml` starts Prometheus, Grafana (provisioned request dashboard), and Jaeger from a clean checkout, with no log pipeline; `deploy/vector/vector.toml` needs updating for Vector 0.34.1 |
| SIEM | Partial | CEF-over-syslog via `CLOUDPAM_AUDIT_SYSLOG_*`. Log Destinations UI is documentation, not destination CRUD. No Splunk HEC / CloudWatch / Cloud Logging connectors |
| Updates | Complete, host-managed | Release check and file-triggered upgrade when `CLOUDPAM_CONTROL_DIR` is set; otherwise trigger returns 501 |
| Multi-tenancy | Not enforced | Postgres stores pin `defaultOrgID`. SQLite `organizations` table is unused by pools/users. Single-tenant appliance |
| Addressing | IPv4 only | `ErrIPv6NotSupported`. Dual-stack is a new workstream, not a flag. GCP discovery can ingest IPv6 addresses that cannot become managed pools |

---

## Storage backend parity

Core pool/account/search `Store` is implemented on memory, SQLite, and PostgreSQL. Of the additional interfaces, only `UtilizationStore` has no PostgreSQL implementation:

| Interface | Memory | SQLite | PostgreSQL |
|-----------|--------|--------|------------|
| `Store` (pools, accounts, search) | yes | yes | yes |
| `DiscoveryStore` | yes | yes | yes |
| `NetworkStore` | yes | yes | yes |
| `DriftStore` | yes | yes | yes |
| `ConversationStore` | yes | yes | yes |
| `SettingsStore` / OIDC | yes | yes | yes |
| `RecommendationStore` | yes | yes (`0011_recommendations.sql`) | yes (`0023_recommendations.up.sql`, #315) |
| **`UtilizationStore`** | yes | yes (`0015_utilization_snapshots.sql`) | **missing**. No API calls `RecordSnapshot` on any backend |

SQLite migrations run through `0021_network_objects_relationships.sql`. Postgres uses a separate sequence (`001` through `0023_recommendations.up.sql`) and has no utilization-snapshot table.

`pool_utilization_cache` exists in SQL and is never written from Go.

`internal/storage/interfaces.go` (`ExtendedStore`, transactions, `FindOverlapping`, `FindGaps`, `MigrateDown`) and `internal/audit/store.go` (`AuditStore` retention APIs) have **no implementations**.

---

## Production and deploy gaps

1. **Azure is selectable but cannot sync.** No collector is planned (#273 was closed as not planned). The API keeps accepting `azure` as an account provider, so operators can track Azure accounts and their pools by hand.
2. **Cloud credentials are the host's, not the account's.** No encrypted per-account secrets, no cloud-connect OAuth. AWS uses the default credential chain / AssumeRole. GCP uses ADC. The standalone agent (`cmd/cloudpam-agent`) hardcodes `aws.New()`, so **GCP cannot run in the agent**.
3. **Soft delete is one-way.** `deleted_at` is set; there is no restore API or UI.
4. **Network objects cannot be deleted.**
5. **The local observability stack ships no logs.** `deploy/docker-compose.observability.yml` starts Prometheus, Grafana, and Jaeger from a clean checkout. Vector runs only in the Kubernetes DaemonSet (`deploy/k8s/vector-daemonset.yaml`), and the sample `deploy/vector/vector.toml` needs updating for Vector 0.34.1 (its `encoding.codec` form is rejected).
6. **Sync is full rescan, on-demand.** No incremental sync, no server-side retry or schedule. The agent has a timer and HTTP push retry for AWS only.
7. **Rate limit is per client IP** (`RATE_LIMIT_RPS` / `RATE_LIMIT_BURST`). No per-user, per-key, or per-org quotas.
8. **`skip_conflicts: true` skips the overlap check.** Every endpoint that creates pools lists, checks, and inserts under one lock. On PostgreSQL that lock is a per-organization advisory lock held in one transaction, so replicas on one database cannot both store overlapping siblings. SQLite and the in-memory store use the in-process `Server.placeGate`. Schema apply and AI plan apply with `skip_conflicts: true` still take the lock and check parent containment, but store overlapping pools.

---

## API vs UI gaps

These backend capabilities exist and the SPA does not expose them (or exposes them incorrectly):

| Capability | API | UI |
|------------|-----|----|
| Gap / fragmentation / compliance analysis | `POST /api/v1/analysis*` | No page, no client |
| Candidate subnet enumeration | `GET /api/v1/pools/{id}/blocks?new_prefix_len=` | Unused. Blocks page lists assigned pools via `GET /api/v1/blocks` |
| Account edit | `PATCH /api/v1/accounts/{id}` | `useAccounts` is list/create/delete only. Create form omits regions, environment, description, `external_id` |
| Cascade delete | `DELETE …?force=true` | UI never sends `force`; parents with children return 409 |
| Persisted network schema policy | `GET/PATCH /api/v1/settings/network-schema-policy` | Discovery has a request-scoped dropdown only |
| Session revoke | `POST /api/v1/auth/users/{id}/revoke-sessions` | No UI |
| Recs / drift / blocks pagination | Hooks accept `page` | Count readout only (audit pager works) |
| LLM configuration | `CLOUDPAM_LLM_*` env | No settings UI |
| Log Destinations | Env-based CEF syslog | Page is static copy |
| Dashboard ops | Drift, recs, sync APIs | Dashboard is pool stats + 5 accounts + 5 audit rows |
| Sidebar vs route RBAC | None | `/config/api-keys` is always in the sidebar but the route requires `settings:read`. `/audit` is gated in the sidebar but not on the route |

Phase 6 (Graphēon-style top nav and shared primitives) is not started. The shell is still a left sidebar; pages restyle cards and tables locally. Identity / Configuration grouping exists.

---

## AI planning: shipped vs remaining

**Shipped:** `POST /api/v1/ai/chat` (SSE), `GET/POST /api/v1/ai/sessions`, `GET/DELETE /api/v1/ai/sessions/{id}`, `POST /api/v1/ai/sessions/{id}/apply-plan`. OpenAI-compatible endpoint override covers Ollama, vLLM, and Azure OpenAI.

**Not shipped:** `/api/v1/planning/ai/conversations/...` (that path is a design leftover; do not use it). Native Anthropic provider, provider fallback, admin-managed `/settings/ai`, what-if simulation, risk scoring, plan comparison, cost/growth analytics, conversation title PATCH, plan validate endpoint, plan Terraform export endpoint.

Enable with `CLOUDPAM_LLM_API_KEY` and/or `CLOUDPAM_LLM_ENDPOINT`. There is no in-app configuration.

---

## Discovery: shipped vs remaining

**Shipped:** AWS + GCP collectors on the server; AWS agent; bootstrap tokens; register / approve / reject; ingest and org ingest; drift detect/resolve/ignore; merged network objects and relationships; schema policy evaluation (request-scoped and persistable via API).

**Not shipped:** Azure collector; GCP in the agent; per-account stored credentials; incremental sync; in-process scheduled sync; collector rate limiter; reconciliation suggestions.

The Discovery UI at `/discovery` has four tabs: Resources, Network, Sync History, Agents. GCP and Azure in the account picker have no matching wizard path; the wizard is AWS-shaped (IAM role, org mode, Terraform/CloudFormation snippets).

---

## Recommended implementation order

These are the gaps that most contradict the product's stated Postgres, multi-cloud, and operator-console story. Each item has an open GitHub issue in the range #266 to #314.

1. Analysis UI, or fold gaps/fragmentation/compliance into pool detail and the dashboard.
2. Operator UI holes: account edit, cascade delete, schema policy in Configuration, and Log Destinations vs env docs.
3. Admin-managed AI provider settings instead of more chat features.
4. GCP in the discovery agent (or document that GCP sync is in-process only).
5. Larger programs: true multi-tenancy, IPv6, MFA, per-org quotas, incremental sync, vendor SIEM, Graphēon shell.

---

## How to keep this file honest

When a gap above is closed, update this file in the same change as the code, and add a concrete version entry in [CHANGELOG.md](CHANGELOG.md). Do not add an `Unreleased` section.
