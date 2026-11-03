# Per-account stored cloud credentials (#275)

Date: 2026-11-02. Validated against `master` at `8196b89` (0.23.10 plus PRs #367-#371).

## 1. Why this exists

### How discovery gets credentials today

CloudPAM has three ways to reach a cloud account, and all of them use a host identity rather than anything stored in CloudPAM.

| Model | Identity used | Reach | Where it lives |
|-------|---------------|-------|----------------|
| Server in-process sync | The server host: EC2 instance profile or `AWS_*` environment for AWS, Application Default Credentials for GCP | The host's own AWS account; the GCP projects the ADC identity can read | `cmd/cloudpam/main.go` registers `aws.New()` and `gcp.New()` on the `SyncService`; `POST /api/v1/discovery/sync` runs them when no agent is connected for the account |
| Agent, single-account mode | The agent host's identity, deployed next to the account | That one account | `cmd/cloudpam-agent` with `CLOUDPAM_ACCOUNT_ID`; pushes through the ingest API |
| Agent, organization mode | The agent host's identity in the management account, then STS AssumeRole into `CloudPAMDiscoveryRole` in every member account with the configured external ID | Every account in one AWS organization | `cmd/cloudpam-agent` with `CLOUDPAM_AWS_ORG_*`; the Terraform `member-role` module and the CloudFormation StackSet create the role |

The server prefers a connected agent for an account (`selectConnectedAgent`) and only falls back to its own collector when none is connected. `AssumeRole` is called in exactly one place: the agent's organization mode. The server's in-process AWS collector never assumes a role.

For one AWS organization with the member role deployed, this is complete. Operators do not need to put credentials into the application; they give the instance or the agent an IAM identity and CloudPAM uses it.

### What #275 adds

Stored per-account credentials cover the accounts the host identity cannot reach:

- a second AWS organization, or a partner account that only grants CloudPAM a role to assume;
- a GCP project whose service account is not the server's ADC identity;
- an environment where no agent can be placed next to the account.

Today those accounts cannot be synced at all. With this change an operator attaches a credential to the account record and the server's in-process sync uses it. Accounts without a stored credential keep today's behaviour exactly. Agents never use stored credentials; an agent-owned account keeps the agent's identity.

## 2. Credential model

One credential per account. Two types in this slice:

| Type | Provider | Secret payload | Non-secret summary |
|------|----------|----------------|--------------------|
| `aws_assume_role` | `aws` | `role_arn`, `external_id` (optional) | `role_arn`, `has_external_id` |
| `gcp_service_account` | `gcp` | the service-account key JSON | `client_email`, `project_id` |

The payload is a JSON object encrypted as one blob with the application key (`CLOUDPAM_OIDC_ENCRYPTION_KEY`) through `internal/secrets`, the same mechanism that protects OIDC client secrets and the AI provider API key. The summary is stored in clear for display. The row also carries the last test time, result (`ok` or `failed`) and a sanitized error message.

Saving is refused with `409 Conflict` when the application key was auto-generated at startup, because the credential would be unreadable after a restart. This is the rule `PATCH /api/v1/settings/ai` already applies, with the same error wording pattern.

Out of scope: static AWS access keys, Azure, more than one credential per account, rotation, expiry.

## 3. Storage

Domain types in `internal/domain/credentials.go`:

- `AccountCredential`: `AccountID`, `Type`, `Summary` (typed struct with the summary fields above, all `omitempty`), `LastTestedAt *time.Time`, `LastTestStatus string`, `LastTestError string`, `CreatedAt`, `UpdatedAt`. This is the API shape and carries no secret.
- `AccountCredentialRecord`: `AccountCredential` plus `PayloadEncrypted string`. Only the store and the resolver see it.
- `AWSAssumeRolePayload{RoleARN, ExternalID}` and `GCPServiceAccountPayload{KeyJSON}` for the decrypted payload.

Store interface `storage.AccountCredentialStore` in `internal/storage/credentials.go`:

```go
GetAccountCredential(ctx, accountID int64) (*domain.AccountCredentialRecord, error) // ErrNotFound when none
PutAccountCredential(ctx, rec domain.AccountCredentialRecord) (domain.AccountCredentialRecord, error) // insert or replace; resets test state
DeleteAccountCredential(ctx, accountID int64) (bool, error)
RecordAccountCredentialTest(ctx, accountID int64, at time.Time, status, errMsg string) error
```

Implementations:

- Memory: `internal/storage/credentials_memory.go`, `NewMemoryAccountCredentialStore(store *MemoryStore)`, following `drift_memory.go`. `MemoryStore.DeleteAccount` and `DeleteAccountCascade` remove the credential.
- SQLite: `internal/storage/sqlite/credentials.go`, migration `migrations/0022_account_credentials.sql`:

```sql
CREATE TABLE IF NOT EXISTS account_credentials (
    account_id        INTEGER PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    type              TEXT NOT NULL,
    payload_encrypted TEXT NOT NULL,
    summary           TEXT NOT NULL DEFAULT '{}',
    last_tested_at    TIMESTAMP NULL,
    last_test_status  TEXT NULL,
    last_test_error   TEXT NULL,
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL
);
```

- PostgreSQL: `internal/storage/postgres/credentials.go`, migration `migrations/postgres/0024_account_credentials.up.sql` with the same columns, `account_id BIGINT PRIMARY KEY REFERENCES accounts(seq_id) ON DELETE CASCADE`, `summary JSONB`, `TIMESTAMPTZ` timestamps.

Deleting an account removes its credential on every backend. Where the account delete is a hard delete the foreign key does it; where it is a soft delete the store deletes the credential row explicitly in the same transaction. The plan verifies which applies per backend.

`cmd/cloudpam/store_*.go` gain `selectAccountCredentialStore`, following `selectSettingsStore`.

## 4. API

All routes under `/api/v1/accounts/{id}/credentials`, registered in `account_handlers.go` next to the existing account subroutes, added to the OpenAPI route metadata and dispatch.

| Method | Path | Permission | Behaviour |
|--------|------|------------|-----------|
| GET | `/credentials` | `accounts:read` | `200` with `AccountCredential`; `404` when the account or the credential does not exist (the error text says which). Never returns the payload. |
| PUT | `/credentials` | `accounts:update` | Body `{"type": "...", "aws_assume_role": {...}}` or `{"type": "...", "gcp_service_account": {"key_json": "..."}}`. Validates: account exists (`404`); `type` matches the account `provider` (`400`); for AWS, `role_arn` matches `arn:aws[a-z-]*:iam::\d{12}:role/.+` and `external_id` is at most 1224 characters; for GCP, the JSON parses, has `"type": "service_account"`, `client_email` and `project_id` and `private_key`. Unknown fields are rejected (`DisallowUnknownFields`). Encrypts and stores. `409` when the application key is ephemeral. Returns `200` with `AccountCredential`. |
| DELETE | `/credentials` | `accounts:update` | `204`; `404` when none. |
| POST | `/credentials/test` | `accounts:update` | Decrypts, builds the provider client with the stored credential (host identity is never used for the test), runs one read-only call, records the result on the row, returns `200 {"status": "ok" \| "failed", "tested_at": ..., "error": "...", "identity": "..."}`. AWS: STS `GetCallerIdentity`, identity is the assumed role ARN. GCP: obtain a token and `GET compute/v1/projects/{project_id}`, identity is `client_email`. Errors are sanitized the way the AI provider test sanitizes them: status code and a short message, no response bodies. Time limit 15 seconds. |

Audit events use a new resource type `account_credential` with the account id as resource id and the account name as the display name: `ActionUpdate` for PUT, `ActionDelete` for DELETE, and `ActionUpdate` with the test status in the detail field for the test. Values are never included.

Sentry request-body capture already excludes secrets after #358; the plan confirms the PUT body is covered.

## 5. Collector wiring

`internal/discovery/collector.go`:

```go
// CredentialAware is implemented by collectors that can run with a stored account credential.
type CredentialAware interface {
    WithCredential(ctx context.Context, payload domain.CredentialPayload) (Collector, error)
}

// CredentialSource returns the decrypted payload for an account, or nil when none is stored.
type CredentialSource func(ctx context.Context, account domain.Account) (domain.CredentialPayload, error)
```

`SyncService` gets `SetCredentialSource(CredentialSource)`. In `Sync`, after choosing the collector by provider: call the source; on error fail the job with `credential: <err>`; when a payload is returned and the collector is `CredentialAware`, replace the collector with `WithCredential(...)` for this run; when the collector is not `CredentialAware`, fail the job with `provider %q does not support stored credentials`. No payload means today's path.

`domain.CredentialPayload` is a small interface with `CredentialType() string`, implemented by the two payload structs.

AWS (`internal/discovery/aws`): `WithCredential` accepts `AWSAssumeRolePayload`, builds `stscreds.NewAssumeRoleProvider` against an STS client from the host config, and returns `NewWithCredentials(provider)`. The existing `AssumeRole(ctx, accountID, roleName, externalID)` becomes a wrapper around a new `AssumeRoleARN(ctx, roleARN, externalID)` so the agent keeps working. The test action's `GetCallerIdentity` uses the same provider; it is exposed as `(*Collector).Identity(ctx) (string, error)`.

GCP (`internal/discovery/gcp`): `WithCredential` accepts `GCPServiceAccountPayload`, calls `google.CredentialsFromJSON(ctx, key, computeScope)` and returns `NewWithTokenSource(creds.TokenSource)`. The test action's project read is `(*Collector).Identity(ctx, project) (string, error)`.

`cmd/cloudpam/main.go` wires a `CredentialSource` backed by the credential store and the application key. Decryption happens there, per sync, so a replaced credential takes effect on the next sync on every replica.

## 6. UI

Accounts page (`ui/src/pages/AccountsPage.tsx`): a Credentials action in each row for AWS and GCP accounts opens `AccountCredentialModal` (new component under `ui/src/components/`). The modal:

- shows the current state from GET: none, or the type, summary and last test result;
- the form is fixed by the provider: ARN plus optional external ID for AWS, a textarea for the service-account JSON for GCP;
- Save (PUT), Test (POST test, shows the identity or the error), Clear (DELETE with confirm);
- renders the `409` ephemeral-key error as a sentence telling the operator to set `CLOUDPAM_OIDC_ENCRYPTION_KEY`;
- is hidden for users without `accounts:update`, read-only with `accounts:read`.

Hook `useAccountCredential(accountId)` in `ui/src/hooks/`, types in `ui/src/api/types.ts`, client methods in `ui/src/api/client.ts`.

Discovery page help panel (`DiscoveryPage.tsx`, the collapsible sections near the end): new section "Where credentials come from" with the table from section 1 condensed to four rows (server identity, agent single, agent org, stored per-account) and one sentence on which applies: a connected agent wins, otherwise a stored credential, otherwise the server identity.

## 7. Documentation

- `docs/DISCOVERY.md`: new section "Credential Models" before "AWS Setup", containing section 1 of this spec in operator wording, the API, the IAM trust policy an operator needs for the assumed role (principal is the server's identity, condition on the external ID), and the GCP roles the service account needs (`roles/compute.networkViewer`).
- `docs/DEPLOYMENT.md`: one paragraph that `CLOUDPAM_OIDC_ENCRYPTION_KEY` is required to store account credentials.
- `CLAUDE.md`: endpoint list and migration count.
- Changelog entry is added by the release PR, not this PR (one version heading per release).
- A comment on issue #275 with section 1, posted when the spec is committed.

## 8. Testing

- Store: shared test suite run against memory and SQLite (and PostgreSQL under `-tags postgres`) for put, get, replace resets test state, delete, record test, and account delete removes the credential.
- Handlers: the four endpoints, the `409` ephemeral key, type/provider mismatch, ARN and JSON validation, unknown fields, permission matrix, and a leak test that saves a credential then asserts the payload, `private_key` and `external_id` values appear in no response from GET credentials, GET account, list accounts, search, or CSV export.
- Audit: the three events exist and their serialized form contains no payload value.
- Discovery: `SyncService` tests with a fake `CredentialAware` collector proving the credential is used when stored, the host collector when not, and a failed job when the source errors or the collector is not credential-aware. AWS and GCP collector tests with an injected STS stub and a fake token source.
- UI: Vitest for the modal states and the `409` rendering.
- Verification: a `verify-cloudpam` journey `account-credentials.cjs` that creates a GCP account, saves a fake service-account JSON, asserts GET returns only the summary, runs Test against a stub compute endpoint and sees the recorded failure or success.

## 9. Decisions

- Separate table and sub-resource rather than columns on `accounts`, so no existing account read path can leak the secret and export/import stay untouched.
- One credential per account. A replacement overwrites.
- Test uses only the stored credential, never the host identity, so a passing test proves the stored credential works on its own.
- Agents are unaffected. Shipping credentials to agents is a different feature.
- Scheduled sync (#276) builds on this: it will call the same `SyncService.Sync` path.
