# CloudPAM SIEM Integration

## Overview

This document is the design answer to issue #93 ("Design: SIEM integration — log format and shipping"). It decides the wire format, the transports CloudPAM ships in-process, the event taxonomy, configuration, delivery semantics, and redaction rules for exporting CloudPAM audit events to a security information and event management system.

It is a companion to `docs/OBSERVABILITY.md`, which owns structured logging, metrics, tracing, and the Vector sidecar pattern. Where the two overlap, `OBSERVABILITY.md` remains authoritative on collector-side concerns; this document is authoritative on what the CloudPAM process itself emits.

**The most important finding is that issue #93 is not a greenfield design.** A working CEF-over-syslog forwarder already exists, is wired into `main.go`, is covered by tests, and is documented. The issue's framing ("what format? CEF, JSON, OCSF?") and its recommendation ("JSON format; webhook + S3/GCS shipping for v1; CEF/OCSF as optional formatters for enterprise") were written without that context and are revised below.

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                            CloudPAM Process                                   │
│                                                                              │
│   HTTP handlers ──► logAudit() ──┐                                           │
│   auth handlers ──► logAuditEventAs() ──┤                                    │
│   OIDC handlers ──► logOIDCAudit() ─────┤                                    │
│   network/drift ──► logAuditWithChanges()┤                                   │
│                                          ▼                                   │
│                             ┌────────────────────────┐                       │
│                             │ ForwardingAuditLogger  │                       │
│                             │  (forwarding.go)       │                       │
│                             └───────┬────────┬───────┘                       │
│                    persist first ───┘        └─── fan out (best effort)      │
│                             ▼                          ▼                     │
│                  ┌────────────────────┐    ┌───────────────────────────┐    │
│                  │ Primary AuditLogger │    │ Sinks                     │    │
│                  │ memory / sqlite /   │    │  • syslog+CEF  (EXISTS)   │    │
│                  │ postgres            │    │  • webhook+JSON (v1)      │    │
│                  │ = system of record  │    │  • file+JSONL   (v1)      │    │
│                  └────────────────────┘    │  • kafka        (deferred)│    │
│                                             └───────────┬───────────────┘    │
└─────────────────────────────────────────────────────────┼────────────────────┘
                                                          ▼
                            ┌──────────────────────────────────────────────┐
                            │ Collector (Vector / Fluent Bit) — optional   │
                            │  tails file sink, adds vendor auth+buffering │
                            └───────┬──────────────┬───────────┬───────────┘
                                    ▼              ▼           ▼
                              Splunk HEC     S3 / GCS     Datadog / Elastic
```

---

## 1. Current State

### 1.1 The event record

`internal/audit/audit.go:11-24` defines `AuditEvent`, and every field already carries a JSON tag:

| Field | Type | Notes |
|-------|------|-------|
| `ID` | string | UUID assigned at persist time |
| `Timestamp` | time.Time | UTC, assigned if zero |
| `Actor` | string | username, API key name, or key prefix — see §1.4 |
| `ActorType` | string | `user`, `api_key`, `anonymous` (`audit.go:74-78`) |
| `Action` | string | `create`/`update`/`delete`/`read` plus auth verbs (`audit.go:56-61`, `audit.go:81-88`) |
| `ResourceType` | string | `pool`, `account`, `api_key`, `user`, `session`, `network_conflict` (`audit.go:64-71`) |
| `ResourceID` | string | |
| `ResourceName` | string | omitempty |
| `Changes` | `*Changes` | `Before`/`After` as `map[string]any` (`audit.go:27-30`) |
| `RequestID` | string | correlates with request logs |
| `IPAddress` | string | **empty in production — see §1.4** |
| `StatusCode` | int | HTTP status |

`ListOptions` (`internal/audit/audit.go:33-41`) supports `Limit`, `Offset`, `Actor`, `Action`, `ResourceType`, `Since`, `Until`. The `Since`/`Until` pair is what makes gap-replay feasible (§6.4).

`AuditLogger` (`internal/audit/audit.go:44-53`) is a three-method interface: `Log`, `List`, `GetByResource`. Note that the much richer `AuditStore` interface in `internal/audit/store.go:13-46` (with `Query`, `Count`, `GetByID`, `GetByActor`, `DeleteOlderThan`, `Close`) has **no implementations** — it is aspirational. Any shipper must be written against `AuditLogger`, not `AuditStore`.

### 1.2 Persistence

Three loggers implement `AuditLogger`, selected by build tag:

- `MemoryAuditLogger` — `internal/audit/memory.go`, ring-trimmed to `DefaultMaxEvents = 10000` (`memory.go:12`). Selected by `cmd/cloudpam/store_default.go:34-37`.
- `SQLiteAuditLogger` — `internal/audit/sqlite.go`, table from `migrations/0004_audit_logs.sql`. Selected by `cmd/cloudpam/store_sqlite.go:40-52`.
- `PostgresAuditLogger` — `internal/audit/postgres.go`, table `audit_events` from `migrations/postgres/001_core_schema.up.sql:226-253`. Selected by `cmd/cloudpam/store_postgres.go:40-52`.

The two SQL schemas differ. Postgres carries `organization_id`, `actor_email`, and a `metadata JSONB DEFAULT '{}'` column (`001_core_schema.up.sql:228,232,239`) that the SQLite table lacks and that no Go code writes. SQLite stores `changes` as a `TEXT` JSON blob (`0004_audit_logs.sql`); Postgres as `JSONB`. A shipper must not depend on `metadata` or `actor_email` existing.

### 1.3 What already ships — CEF over syslog

This is the part issue #93 did not account for.

- `internal/audit/cef.go:19-87` — `CEFFormatter.Format` produces a CEF:0 record. Header is `CEF:0|BadgerOps|CloudPAM|{version}|{action}|{resource_type}.{action}|{severity}|{extensions}` (`cef.go:78-86`).
- `internal/audit/cef.go:96-116` — severity mapping: 8 for 5xx, 6 for 4xx, 7 for `login_failed`/`account_locked`, 5 for delete, 1 for read, 3 otherwise.
- `internal/audit/cef.go:143-221` — `SyslogSink` wraps the formatter and emits RFC 5424 framed messages over UDP or TCP (`cef.go:223-237`), facility `local4` (`cef.go:239-242`).
- `internal/audit/forwarding.go:18-58` — `ForwardingAuditLogger` persists via the primary logger first, then fans out to sinks best-effort; sink errors go to an error handler and never fail the write (`forwarding.go:47-56`).
- `cmd/cloudpam/main.go:367-394` — wiring. Enabled by `CLOUDPAM_AUDIT_SYSLOG_ADDR`, with `CLOUDPAM_AUDIT_SYSLOG_NETWORK` (default `udp`) and `CLOUDPAM_AUDIT_SYSLOG_APP_NAME` (default `cloudpam`). Attached at `main.go:155`.
- Documented at `docs/OBSERVABILITY.md:110-131`, which explicitly states the architectural position that vendor APIs (Splunk HEC, Datadog intake) belong in a collector, "keeps credentials, buffering, and backpressure handling outside the CloudPAM process" (`OBSERVABILITY.md:129-131`).
- Tests: `internal/audit/syslog_test.go`, `internal/audit/forwarding_test.go`.

### 1.4 Two gaps in what is captured today

**`IPAddress` is empty on every production audit event.** The only code in the repository that sets `AuditEvent.IPAddress` is `internal/api/middleware.go:868`, inside `AuditMiddleware` (`middleware.go:805`). That middleware is **not** in the production chain — `cmd/cloudpam/main.go:294-300` applies `MetricsMiddleware`, `RequestIDMiddleware`, `LoggingMiddleware`, `CSRFMiddleware`, and `RateLimitMiddleware`, and no audit middleware. `AuditMiddleware`'s only caller is `internal/testutil/testutil.go:124`. Production events are emitted by the handler-level helpers — `Server.logAuditWithChanges` (`internal/api/server.go:135-167`), `UserServer.logAuditEventAs` (`internal/api/user_handlers.go:899-929`), and `OIDCServer.logOIDCAudit` (`internal/api/oidc_handlers.go:801-822`) — and **none of the three populate `IPAddress`**.

The consequence for SIEM is direct: the CEF formatter only emits `src=` when `net.ParseIP(event.IPAddress)` succeeds (`cef.go:59-61`), so today's forwarded CEF records carry no source address at all. Source IP is the single most-used correlation key in SIEM detection rules — brute-force clustering, impossible-travel, threat-intel joins all key on it. Shipping auth-failure events with no source IP delivers very little of the value the integration exists for. This is a prerequisite fix (§8).

**`Actor` is inconsistent between emitters.** `Server.logAuditWithChanges` records `key.Name` for API-key actors (`server.go:147`), while `UserServer.logAuditEventAs` records `key.Prefix` (`user_handlers.go:911`) and `AuditMiddleware` also records `key.Prefix` (`middleware.go:856`). `OIDCServer.logOIDCAudit` sets `Actor` to the resource name and hardcodes `ActorType` to `user` (`oidc_handlers.go:807-808`). A SIEM cannot build a reliable per-principal timeline across these. Normalising on the key prefix (a stable, non-secret identifier) is a prerequisite.

### 1.5 No redaction exists

A repository-wide search for redaction logic in `internal/` and `cmd/` returns nothing. `Changes.Before`/`Changes.After` are unconstrained `map[string]any` (`audit.go:27-30`), populated by whatever the calling handler puts in them. Today only two production call sites populate `Changes` — `internal/api/auth_handlers.go:489` (API-key rotation, carrying `expires_at`/`expires_in_days`) and `internal/api/network_handlers.go:1570-1576` (network conflict resolution) — and neither currently carries a secret. That is a coincidence of current call sites, not a guarantee. See §7.

---

## 2. Format Decision

### 2.1 Decision

| Format | Status | Rationale |
|--------|--------|-----------|
| **JSON** | **Default for all new transports** | 1:1 with `AuditEvent`'s existing JSON tags; only format that can carry `Changes` |
| **CEF** | **Retained; remains the syslog default** | Already shipped and documented; breaking it is a regression |
| **OCSF** | **Deferred to a later phase; collector-side mapping recommended in the interim** | Pure downstream transform of JSON; taxonomy work is disproportionate to demand |

This **partially contradicts issue #93**, which proposed JSON as *the* format with "CEF/OCSF as optional formatters for enterprise". CEF is not a future optional enterprise add-on — it is the currently shipping default and the only format any existing deployment can be consuming. The correct framing is that JSON is added *alongside* CEF as the default for new transports, and the formatter becomes a selectable axis independent of the transport.

### 2.2 Why JSON is required, not merely preferred

The decisive argument is `Changes`. `CEFFormatter.Format` builds a flat `key=value` extension list (`cef.go:43-76`) and **never reads `event.Changes`**. The before/after diff — the field that answers "what did this admin actually alter?", the thing a security analyst most needs — is silently dropped by the only formatter CloudPAM ships.

This is not a bug to patch in CEF; it is a structural limit. CEF extensions are flat scalars, while `Changes` is a pair of arbitrarily nested `map[string]any` values. Encoding nesting into CEF means either JSON-stringifying into a custom slot or flattening with synthesised key names, and the custom slots are nearly exhausted already: `cs1` = actor_type (`cef.go:57`), `cs2` = resource_type (`cef.go:66`), `cs3` = resource_id (`cef.go:69`), `cs4` = resource_name (`cef.go:72`), `cn1` = http_status (`cef.go:75`). Two string slots remain for an unbounded structure.

JSON has no such problem, and the marginal implementation cost is near zero: `AuditEvent` is already fully tagged (`audit.go:12-23`), so the formatter is `json.Marshal` over a redacted copy.

### 2.3 Why OCSF is deferred

OCSF is a JSON-schema profile, so it sits strictly downstream of a JSON formatter — nothing about deferring it forecloses it. Adopting it now would require mapping CloudPAM's action vocabulary onto OCSF `class_uid`/`activity_id` pairs, and that vocabulary does not map cleanly. `audit.go:56-88` and the ad-hoc action strings used at call sites (`"revoke_sessions"` at `user_handlers.go:812`, `"network_conflict."+action` at `network_handlers.go:1576`, `"update"` on resource type `"system"` at `update_handlers.go:206`) span at least four OCSF categories: Identity & Access Management, Application Activity, Configuration/Discovery, and Findings. Each needs a hand-audited mapping plus a `metadata.product` block, and it must be re-audited whenever a call site adds a new action string.

Until there is a concrete deployment asking for it, a Vector VRL `remap` transform reading the JSON sink produces OCSF with no CloudPAM code and no CloudPAM release coupling. Ship OCSF in-process only when a user asks and the action vocabulary has been frozen (§9, Phase 4).

### 2.4 Example payload — JSON format

A privileged pool deletion, as it would appear on the webhook/file sink after redaction:

```json
{
  "id": "0f1b2c3d-4e5f-4a6b-8c9d-0e1f2a3b4c5d",
  "timestamp": "2026-08-01T14:22:07.418293Z",
  "actor": "cp_a1b2c3d4",
  "actor_type": "api_key",
  "action": "delete",
  "resource_type": "pool",
  "resource_id": "412",
  "resource_name": "prod-us-east-1-shared",
  "changes": {
    "before": {
      "cidr": "10.42.0.0/16",
      "account_id": 7,
      "parent_id": 3
    }
  },
  "request_id": "01J9K3M7QW8ZP2X4V6R1TB5NCD",
  "ip_address": "203.0.113.44",
  "status_code": 204
}
```

Envelope fields added by the shipper (not part of `AuditEvent`, see §5):

```json
{
  "source": "cloudpam",
  "source_version": "0.9.3",
  "host": "cloudpam-7d9f8c-x2k4l",
  "environment": "production",
  "schema_version": 1,
  "events": [ /* ... batch of the above ... */ ]
}
```

Two things to note against §1.4: `ip_address` above is populated only once the prerequisite fix lands, and `actor` is the key prefix only once the emitters are normalised.

### 2.5 Example payload — CEF format (current behaviour)

The same event through today's `CEFFormatter` (`cef.go:78-86`), with `IPAddress` empty as it is in production:

```
CEF:0|BadgerOps|CloudPAM|0.9.3|delete|pool.delete|5|act=delete outcome=success rt=1785680527418 externalId=0f1b2c3d-4e5f-4a6b-8c9d-0e1f2a3b4c5d suser=cp_a1b2c3d4 cs1Label=actor_type cs1=api_key request=01J9K3M7QW8ZP2X4V6R1TB5NCD cs2Label=resource_type cs2=pool cs3Label=resource_id cs3=412 cs4Label=resource_name cs4=prod-us-east-1-shared cn1Label=http_status cn1=204
```

Severity `5` comes from the delete branch of `cefSeverity` (`cef.go:109-110`). Note the absence of both `src=` (§1.4) and any representation of `changes` (§2.2) — the two concrete losses this design fixes.

---

## 3. Transport Decision

### 3.1 Decision

| Transport | v1 | Rationale |
|-----------|-----|-----------|
| **Syslog (UDP/TCP)** | Already shipped — keep | `cef.go:143-221`; existing deployments depend on it |
| **HTTPS webhook** | **Yes** | Universal; every SIEM has an HTTP intake; no new dependencies |
| **File (JSON Lines)** | **Yes** | The correct answer to "S3/GCS" — see §3.3 |
| **S3 / GCS direct** | **No** | Contradicts an existing architectural decision — see §3.3 |
| **Kafka** | **No** | Heavy dependency; collector solves it — see §3.4 |

### 3.2 Webhook

A batched HTTPS POST of `Content-Type: application/json` carrying the §2.4 envelope. Chosen because it is the lowest-common-denominator intake across Splunk HEC, Elastic, Datadog, Sumo Logic, Panther, and every homegrown collector, and because it needs nothing beyond `net/http`, which is already in the binary. Authentication is a static bearer or custom header (§7.3), which covers HEC tokens and Datadog API keys without vendor-specific code.

### 3.3 File sink instead of direct S3/GCS

**This contradicts issue #93's recommendation of "webhook + S3/GCS shipping for v1", and deliberately so.**

Direct object-store shipping is not one feature; it is batching, buffering, multipart upload, retry, key/partition layout, compaction, and lifecycle policy — plus the AWS and GCP SDKs and their credential chains pulled into the server process. `docs/OBSERVABILITY.md:129-131` already made the opposite call in writing: use a collector "when a deployment needs vendor APIs... That keeps credentials, buffering, and backpressure handling outside the CloudPAM process." Object stores are the same class of problem. Adding S3/GCS in-process would silently reverse a decision the project already documented, and would duplicate machinery Vector already has and that `deploy/vector/` already exists to configure.

A file sink writing newline-delimited JSON to a configured path satisfies the same requirement at a fraction of the cost. Vector or Fluent Bit tails the file and performs the object upload with its own buffering, compression, and partitioning — the sidecar pattern `OBSERVABILITY.md` §2.2 already specifies. The file also gives operators a local durable spool for free, which is what makes gap-replay (§6.4) practical.

If a future deployment genuinely cannot run a sidecar (a constraint no current requirement states), revisit S3 direct as a distinct phase with its own design.

### 3.4 Kafka deferred

Kafka requires a substantial client library (`franz-go` or `sarama`), broker-specific TLS/SASL configuration, partitioning strategy, and its own delivery-semantics surface. No current requirement asks for it. Vector has a Kafka sink; the file sink plus a collector covers it. Reconsider only on concrete demand.

---

## 4. Event Taxonomy

Issue #93 asks "what events beyond CRUD? Auth failures, discovery anomalies, drift detection?" **The premise understates what already exists.** Auth failures and drift are both already captured. The real gaps are elsewhere.

### 4.1 Already captured

| Category | Actions | Emitted at |
|----------|---------|-----------|
| Pool CRUD | create/update/delete | `pool_handlers.go:258,396,588` |
| Account CRUD | create/update/delete | `account_handlers.go:196,220,286` |
| User CRUD | create/update/delete | `user_handlers.go:577,655,712,788` |
| Role CRUD | create/update/delete | `role_handlers.go:113,128,138` |
| **Auth success/failure** | `login`, `login_failed`, `logout` | `user_handlers.go:227,235,248,315,345,878` |
| **Account lockout** | `account_locked`, `account_unlocked` | `user_handlers.go:243,841,880` |
| Session revocation | `revoke_sessions` | `user_handlers.go:812` |
| OIDC operations | via `logOIDCAudit` | `oidc_handlers.go:801-822` |
| API key lifecycle | CRUD + `api_key_rotation_due` | `auth_handlers.go:481-492` |
| **Drift / network conflict** | `network_conflict.{action}` with `Changes.After` | `network_handlers.go:1570-1576`, resource type `audit.ResourceNetworkConflict` (`audit.go:70`) |
| Security settings change | `update` on `settings` | `settings_handlers.go:123,153` |
| Schema-plan bulk apply | `create` per pool | `schema_handlers.go:285` |
| AI plan apply | `create` per pool | `ai_handlers.go:317` |
| Self-upgrade trigger | `update` on `system` | `update_handlers.go:206` |
| First-boot setup | `create` on `user` | `system_handlers.go:294` |

So: auth failures — **yes, already there**. Drift detection — **yes, already there**. The answer to two of the issue's three sub-questions is "no new capture work needed".

### 4.2 Genuinely missing capture points

| Gap | Evidence | Priority | Why it matters to a SIEM |
|-----|----------|----------|--------------------------|
| **Source IP on all events** | Only `middleware.go:868` sets it; that middleware is unwired (§1.4) | **P0** | Blocks brute-force, impossible-travel, and threat-intel correlation |
| **Actor normalisation** | `key.Name` (`server.go:147`) vs `key.Prefix` (`user_handlers.go:911`) | **P0** | Breaks per-principal timelines |
| **Authorization denials (403)** | No audit call in `internal/auth/rbac.go` | **P1** | Privilege-probing is a primary detection signal; currently invisible |
| **Bulk export** | No `logAudit` in `internal/api/export_handlers.go` | **P1** | `GET /api/v1/export` dumps accounts, pools, and blocks to CSV. This is the clearest data-exfiltration signal in the product and it is entirely unaudited. `AuditMiddleware` would not have caught it either — it skips all GET requests (`middleware.go:826`) |
| **Bulk import** | No `logAudit` in `internal/api/export_handlers.go` | **P1** | Mass mutation with no record |
| **Discovery sync lifecycle** | No `audit.` reference anywhere in `internal/discovery/` | **P2** | Sync start/finish/failure and org bulk-ingest (`/api/v1/discovery/ingest/org`) are unaudited; "discovery anomalies" from the issue have no capture point at all |
| **`Changes` on pool/account mutations** | `logAudit` passes `nil` changes (`server.go:131`) | **P2** | Without before/after, "pool 412 updated" is not actionable |

Note the asymmetry: reads are almost entirely unaudited by design (`ActionRead` exists at `audit.go:60` and is commented "Used only for sensitive operations like key listing"). That is a defensible default for an IPAM, but export is the exception that must be carved out.

### 4.3 Recommended severity mapping

The existing `cefSeverity` (`cef.go:96-116`) is a reasonable baseline and should be lifted into a format-independent `Severity(event)` helper so JSON, CEF, and any future OCSF mapping agree. Extend it for the new events: authorization denial → 6, bulk export → 6, discovery sync failure → 4.

---

## 5. Configuration

Following the established conventions — `CLOUDPAM_*` prefix (`internal/observability/logger.go:74-86`, `internal/observability/metrics.go:37-43`), a presence-or-boolean enable flag, and the existing `CLOUDPAM_AUDIT_SYSLOG_*` namespace (`cmd/cloudpam/main.go:368-373`).

### 5.1 Existing (unchanged)

| Variable | Default | Description |
|----------|---------|-------------|
| `CLOUDPAM_AUDIT_SYSLOG_ADDR` | — | Syslog target `host:port`. Presence enables the sink. |
| `CLOUDPAM_AUDIT_SYSLOG_NETWORK` | `udp` | `udp` or `tcp` |
| `CLOUDPAM_AUDIT_SYSLOG_APP_NAME` | `cloudpam` | RFC 5424 APP-NAME |

### 5.2 New — shared

| Variable | Default | Description |
|----------|---------|-------------|
| `CLOUDPAM_AUDIT_SYSLOG_FORMAT` | `cef` | `cef` or `json`. Default preserves current behaviour. |
| `CLOUDPAM_AUDIT_SHIP_QUEUE_SIZE` | `1024` | Bounded async queue depth across all sinks |
| `CLOUDPAM_AUDIT_SHIP_BATCH_SIZE` | `50` | Max events per batch (webhook/file) |
| `CLOUDPAM_AUDIT_SHIP_BATCH_INTERVAL` | `5s` | Max flush latency |
| `CLOUDPAM_AUDIT_SHIP_INCLUDE_CHANGES` | `true` | Ship the redacted `Changes` diff |
| `CLOUDPAM_AUDIT_SHIP_REDACT_KEYS` | *(built-in list)* | Comma-separated additional keys to redact (§7.1) |
| `CLOUDPAM_AUDIT_SHIP_ENVIRONMENT` | `SENTRY_ENVIRONMENT` or `production` | Envelope `environment` field |

### 5.3 New — webhook sink

| Variable | Default | Description |
|----------|---------|-------------|
| `CLOUDPAM_AUDIT_WEBHOOK_URL` | — | HTTPS endpoint. Presence enables the sink. |
| `CLOUDPAM_AUDIT_WEBHOOK_AUTH_HEADER` | `Authorization` | Header name (e.g. `DD-API-KEY`, `Authorization`) |
| `CLOUDPAM_AUDIT_WEBHOOK_AUTH_VALUE` | — | Header value (e.g. `Splunk <hec-token>`, `Bearer <token>`) |
| `CLOUDPAM_AUDIT_WEBHOOK_TIMEOUT` | `10s` | Per-request timeout |
| `CLOUDPAM_AUDIT_WEBHOOK_MAX_RETRIES` | `4` | Exponential backoff attempts |
| `CLOUDPAM_AUDIT_WEBHOOK_CA_FILE` | — | Custom CA bundle for private SIEM endpoints |
| `CLOUDPAM_AUDIT_WEBHOOK_INSECURE_SKIP_VERIFY` | `false` | Escape hatch; logs a startup warning when true |

### 5.4 New — file sink

| Variable | Default | Description |
|----------|---------|-------------|
| `CLOUDPAM_AUDIT_FILE_PATH` | — | JSON Lines output path. Presence enables the sink. |
| `CLOUDPAM_AUDIT_FILE_MAX_SIZE_MB` | `128` | Rotate at this size |
| `CLOUDPAM_AUDIT_FILE_MAX_FILES` | `5` | Rotated files retained |
| `CLOUDPAM_AUDIT_FILE_MODE` | `0600` | Created-file permissions (§7.4) |

`CLOUDPAM_AUDIT_WEBHOOK_URL` must reject non-`https` schemes unless the host resolves to loopback, to prevent silently plaintexting audit records containing usernames and source IPs.

---

## 6. Delivery Semantics

### 6.1 What today's behaviour actually is

Worth stating plainly, because it constrains the design:

- **Synchronous and in-band.** `ForwardingAuditLogger.Log` calls `sink.Send` inline (`forwarding.go:52-56`), inside the request goroutine.
- **One connection per event.** `SyslogSink.Send` dials a fresh connection on every call and closes it in a defer (`cef.go:200-207`). Acceptable for UDP; for TCP this is a full handshake per audit event on the request path.
- **Request-scoped context.** The request `ctx` is passed to `Send` (`forwarding.go:53`), so a client disconnect cancels forwarding of an event that was already persisted.
- **Best-effort.** Errors go to the handler and are logged as warnings (`main.go:387-393`); nothing retries, nothing is queued.

With a 2-second default dial timeout (`cef.go:176-178`), a black-holing TCP syslog target adds up to 2s to every mutating request. That is a latency and availability coupling the webhook sink would make far worse, since HTTP timeouts are measured in seconds by default.

### 6.2 Target model

**Asynchronous, bounded, at-least-once to durable sinks; best-effort to syslog.**

1. `Log` persists to the primary logger first, exactly as now (`forwarding.go:47-49`). Persistence failure still fails the write. **The database remains the system of record**; shipping is a projection of it, never the authority.
2. After a successful persist, a deep copy (`forwarding.go:51`, `memory.go:165-177`) is pushed onto a bounded channel of `CLOUDPAM_AUDIT_SHIP_QUEUE_SIZE`. The enqueue is non-blocking.
3. A dedicated worker goroutine per sink drains the queue, batching by `BATCH_SIZE` / `BATCH_INTERVAL`.
4. `Send` receives a background context with the sink's own timeout — never the request context. Client disconnects must not drop already-persisted events.
5. Long-lived connections: the TCP syslog sink and the webhook client keep a reusable connection with reconnect-on-error, replacing dial-per-event.

### 6.3 Backpressure

When the queue is full, **drop the newest event, increment a counter, and log at warn with sampling**. Blocking the request path on a SIEM outage is unacceptable — it converts a logging degradation into an application outage, and the event is already durably persisted regardless.

Three metrics, registered alongside the existing Prometheus set in `internal/observability/metrics.go`:

- `cloudpam_audit_ship_queued_total{sink}` / `cloudpam_audit_ship_delivered_total{sink}`
- `cloudpam_audit_ship_dropped_total{sink,reason}` — `reason` ∈ `queue_full`, `retries_exhausted`
- `cloudpam_audit_ship_queue_depth{sink}` gauge, and `cloudpam_audit_ship_lag_seconds{sink}`

`cloudpam_audit_ship_dropped_total` increasing is the alert that matters; it is the only signal distinguishing "SIEM is quiet because nothing happened" from "SIEM is quiet because delivery is broken".

### 6.4 Sink down, and recovery

- **Transient failure** — retry with exponential backoff and jitter up to `MAX_RETRIES`, holding the batch in the queue. Queue fills, then §6.3 drops apply.
- **Sustained outage** — CloudPAM keeps serving; drops are counted; the audit table stays complete.
- **Recovery** — because the database is complete and `ListOptions` supports `Since`/`Until` (`audit.go:39-40`), a gap is repairable. Provide `cloudpam -audit-replay -since <ts> -until <ts> -sink <name>`, paginating `List` and re-emitting through the formatter. This is why the design does not attempt an in-process durable disk queue: the durable queue already exists, and it is the audit table.
- **Shutdown** — the shutdown sequence at `cmd/cloudpam/main.go:332-357` closes the store and flushes Sentry. Add a bounded drain (5s) for shipper queues before `store.Close()`, so an orderly restart does not silently drop the tail.

At-least-once means the SIEM may see duplicates after a retry. `AuditEvent.ID` is a UUID assigned at persist time (`sqlite.go:51-53`, `postgres.go:53-55`) and is stable across re-sends, so downstream dedup is straightforward. Document `id` as the dedup key.

---

## 7. Security Considerations

### 7.1 Redaction — the primary risk

There is no redaction anywhere in the codebase today (§1.5), and `Changes` is an untyped `map[string]any` (`audit.go:27-30`). Today's exposure is limited only because just two call sites populate it, and neither carries a secret. That is luck, not design — and the moment JSON shipping makes `Changes` visible off-box, it becomes an active liability. Specifically, the OIDC admin handlers manage client secrets, and the security settings handler (`settings_handlers.go:123`) manages auth policy; if anyone later adds a `Changes` diff to either — the natural next improvement, and one §4.2 explicitly recommends for pools and accounts — secrets would flow straight to the SIEM.

**Redaction must land before or with the JSON formatter, not after.**

Design: a deny-by-default key allowlist is too brittle for an evolving diff shape, so use a case-insensitive substring denylist applied recursively to `Changes.Before` and `Changes.After`, replacing matched values with `"[REDACTED]"`. Built-in terms:

```
password, passwd, secret, client_secret, token, api_key, apikey, key_hash,
token_hash, salt, credential, private, authorization, cookie, session_id,
encryption_key, refresh_token, id_token, access_token, bearer
```

Extensible via `CLOUDPAM_AUDIT_SHIP_REDACT_KEYS`. Redact on a **copy**, never in place — `forwarding.go:51` already deep-copies, and the copy is what the sink sees, so the persisted record and the API-visible audit log are unaffected.

Additional rules:

- Values longer than a configurable cap (default 4 KB) are truncated with a `"[TRUNCATED]"` marker — a blind `map[string]any` can otherwise carry an entire request body.
- Never ship a full API key. `auth_handlers.go:481-492` correctly records only `key.ID`, `key.Name`, and expiry; keep that discipline.
- `CLOUDPAM_AUDIT_SHIP_INCLUDE_CHANGES=false` is the operator kill switch for regulated environments.
- Redaction needs a table-driven test asserting that nested and array-embedded secrets are caught, and that non-secret fields survive.

### 7.2 PII

`Actor` is a username (`server.go:144`) and `IPAddress` (once populated) is personal data under GDPR. This is inherent to audit logging and appropriate, but it must be stated: the webhook target is a data processor. Document it in `docs/DEPLOYMENT.md` when the feature ships.

### 7.3 Transport authentication

- **Webhook** — TLS required; a single configurable auth header covers HEC (`Authorization: Splunk <token>`), Datadog (`DD-API-KEY`), and bearer schemes. `CLOUDPAM_AUDIT_WEBHOOK_CA_FILE` supports private CAs so operators are not pushed toward `INSECURE_SKIP_VERIFY`. Never log the auth value; redact it from startup logs and from any error message that echoes request headers.
- **Syslog** — plain UDP/TCP today (`cef.go:159-161` accepts only `udp`/`tcp`). This is unencrypted on the wire and carries usernames. Document that it belongs on a trusted network segment, and add TLS syslog (RFC 5425, `network=tcp+tls`) as a follow-on.
- **File** — see §7.4.

### 7.4 File sink permissions

Create with `0600` and a `0700` parent directory. The file contains usernames, source IPs, and diffs; a world-readable audit spool on a shared host is a straightforward local privilege-escalation aid. If the collector runs as a different user, use a shared group and `0640`, not `0644`.

### 7.5 Self-referential logging

The shipper must never emit an audit event for its own activity, and its error logs must not include payload bodies — a failing webhook that echoes the request body into `slog` output would defeat redaction by routing the same data through the general log pipeline.

---

## 8. Prerequisites

Two open defects in `internal/audit` are inherited by anything reading through these paths. **Both are prerequisites and neither is fixed by this document** (which is docs-only).

### 8.1 `SQLiteAuditLogger.Close()` closes caller-owned handles

`internal/audit/sqlite.go:34-42`:

```go
// NewSQLiteAuditLoggerFromDB creates a new SQLite-backed audit logger using an existing DB connection.
func NewSQLiteAuditLoggerFromDB(db *sql.DB) *SQLiteAuditLogger {
	return &SQLiteAuditLogger{db: db}
}

// Close closes the database connection.
func (s *SQLiteAuditLogger) Close() error {
	return s.db.Close()
}
```

`Close()` unconditionally closes the `*sql.DB` regardless of who owns it. A logger built via `NewSQLiteAuditLoggerFromDB` shares a handle the caller also holds; closing it invalidates every other user of that connection.

The correct pattern is already implemented one file over: `PostgresAuditLogger` carries an `ownPool bool` (`internal/audit/postgres.go:22`) and honours it (`postgres.go:40-45`). SQLite needs the same `ownDB` flag.

**Impact on the shipper.** The replay tool (§6.4) and any future sink that opens its own reader over the audit database would construct a logger from a shared handle; a defer-close in that code path would tear down the application's SQLite connection at runtime. Fix first.

*A separate branch may already be addressing this.*

### 8.2 `SQLiteAuditLogger.GetByResource` filters after a SQL limit

`internal/audit/sqlite.go:167-184`:

```go
func (s *SQLiteAuditLogger) GetByResource(ctx context.Context, resourceType, resourceID string) ([]*AuditEvent, error) {
	events, _, err := s.List(ctx, ListOptions{
		ResourceType: resourceType,
		Limit:        1000,
	})
	...
	for _, e := range events {
		if e.ResourceID == resourceID {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}
```

It fetches the newest 1000 events of the given *type* and then filters by `resource_id` in Go (`sqlite.go:178-181`). Any matching event older than the 1000th most recent event of that type is silently invisible — no error, no truncation flag, just a short result. On a busy deployment 1000 pool events is a matter of hours.

`ListOptions` has no `ResourceID` field (`audit.go:33-41`), which is the root cause. The Postgres implementation avoids it entirely by filtering in SQL (`postgres.go:151-158`), and the schema has the supporting index — `idx_audit_logs_resource ON audit_logs(resource_type, resource_id)` in `migrations/0004_audit_logs.sql`, and `idx_audit_events_resource` at `migrations/postgres/001_core_schema.up.sql:248-249`. The fix is to add `ResourceID` to `ListOptions` and push the predicate into the `WHERE` clause built at `sqlite.go:89-112`.

**Impact on the shipper.** This is not hypothetical. `auth_handlers.go:471-479` already calls `GetByResource` to decide whether an `api_key_rotation_due` event was already emitted; when the lookup misses, it re-emits a duplicate. Any shipper feature that reconciles per-resource history — dedup, replay-by-resource, backfill verification — inherits the same silent under-read and would report success on an incomplete comparison.

### 8.3 Capture-point prerequisites

From §4.2, required before the integration delivers its intended value:

1. Populate `IPAddress` in `Server.logAuditWithChanges`, `UserServer.logAuditEventAs`, and `OIDCServer.logOIDCAudit`, using the same trusted-proxy-aware client resolution as `middleware.go:868`. **P0.**
2. Normalise `Actor` for API-key principals to `key.Prefix` across all emitters. **P0.**
3. Decide `AuditMiddleware`'s fate: wire it in (`main.go:294-300`) as a backstop, or delete it. Leaving unwired production-shaped code that is only exercised by `testutil.go:124` is how the `IPAddress` gap went unnoticed.

---

## 9. Phased Implementation Plan

Sizing is rough, in engineer-days, excluding review.

### Phase 0 — Prerequisites (3–4 d)

- Fix `sqlite.go` `ownDB` (§8.1) — 0.5 d
- Add `ResourceID` to `ListOptions`, push predicate into SQL, fix `GetByResource` (§8.2) — 1 d
- Populate `IPAddress` and normalise `Actor` across the three emitters; resolve `AuditMiddleware` (§8.3) — 1.5 d
- Tests across memory/sqlite/postgres — 1 d

Independently valuable; unblocks everything below.

### Phase 1 — Format and redaction core (3–4 d)

- Extract a `Formatter` interface; make `CEFFormatter` implement it with no behaviour change — 0.5 d
- `JSONFormatter` over a redacted copy, plus the envelope (§2.4) — 1 d
- Redaction package: recursive denylist, truncation, config plumbing (§7.1) — 1.5 d
- `CLOUDPAM_AUDIT_SYSLOG_FORMAT` — 0.5 d

### Phase 2 — Async delivery pipeline (4–5 d)

- Bounded queue, per-sink workers, batching, background contexts (§6.2) — 2 d
- Retry with backoff, drop-on-full, metrics (§6.3) — 1.5 d
- Long-lived syslog TCP connection replacing dial-per-event (`cef.go:200-207`) — 0.5 d
- Shutdown drain in `main.go:332-357` — 0.5 d

Behaviour-preserving for existing syslog users, so it ships independently of any new sink.

### Phase 3 — Webhook and file sinks (4–5 d)

- Webhook sink: TLS config, auth header, batching, retry (§5.3) — 2 d
- File sink: JSON Lines, rotation, permissions (§5.4) — 1.5 d
- `docs/OBSERVABILITY.md` cross-reference, `docs/DEPLOYMENT.md` operator guidance, Vector example config in `deploy/vector/` for the file → S3/GCS path — 1 d

Delivers the issue's actual v1 requirement.

### Phase 4 — Coverage and enterprise (5–7 d, demand-gated)

- New capture points: authz denials, export/import, discovery sync lifecycle (§4.2) — 2 d
- `Changes` diffs on pool and account mutations — 1.5 d
- `cloudpam -audit-replay` (§6.4) — 1.5 d
- OCSF formatter, **only on concrete demand** (§2.3) — 2 d
- TLS syslog / RFC 5425 (§7.3) — 1 d

### Explicitly out of scope

Direct S3/GCS sinks (§3.3), Kafka (§3.4), and per-tenant SIEM routing (multi-tenancy is not enforced yet — the Postgres logger hardcodes `defaultOrgID`, `postgres.go:16,31`).

---

## 10. Summary of Decisions

| Question | Decision | Divergence from issue #93 |
|----------|----------|---------------------------|
| Format | JSON default for new transports; CEF retained as syslog default; OCSF deferred | **Yes** — CEF is already shipped, not a future "optional enterprise formatter" |
| Transport v1 | Webhook + file (JSON Lines); syslog already exists | **Yes** — file sink replaces direct S3/GCS, per `OBSERVABILITY.md:129-131` |
| Kafka | Deferred to collector | No |
| Events beyond CRUD | Auth failures and drift **already captured**; real gaps are source IP, authz denials, export, discovery sync | **Yes** — more exists than the issue assumes; the binding gap is missing `IPAddress` |
| Delivery | Async, bounded, at-least-once with DB as system of record; drop-on-full | — |
| Redaction | Recursive denylist on a copy of `Changes`; must land with the JSON formatter | — |
