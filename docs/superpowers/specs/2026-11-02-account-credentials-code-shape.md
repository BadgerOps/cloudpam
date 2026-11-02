# Code shape for per-account stored cloud credentials (#275)

Companion to `2026-11-02-account-credentials-design.md`. The design spec fixes product scope. This file fixes the code shape: types, signatures, ownership, and file layout. Where the two disagree, this file wins, and section 9 lists each disagreement.

Two independent design runners produced candidates on 2026-11-02 (one on Opus, one on the session model). They converged on the vault, the sealed payload interface, the optional collector interface, one bind path for sync and test, and `audit.ActionTest`. The synthesis below takes candidate 1 as the base and grafts two items from candidate 2.

## 1. Facts the shape rests on

- Every account delete is a soft delete on all three backends. SQLite `DeleteAccount` and `DeleteAccountCascade` run `UPDATE accounts SET deleted_at=?` (`internal/storage/sqlite/sqlite.go:636`, `:695`). PostgreSQL does the same (`internal/storage/postgres/postgres.go:882`, `:912`). The memory store sets `DeletedAt` (`internal/storage/store.go:346`, `:395`). A foreign key with `ON DELETE CASCADE` never fires on these paths.
- SQLite foreign keys depend on `PRAGMA foreign_keys=ON`, issued once at open (`sqlite.go:55`) while the pool recycles connections. Nothing in this feature relies on the FK.
- `PATCH /api/v1/accounts/{id}` overwrites `provider` unconditionally, even to the empty string (`store.go:320`, `sqlite.go:602`). The type-to-provider rule therefore runs at save time and at use time.
- `google.CredentialsFromJSON` honours the key file's `token_uri`. Raw key JSON from an operator could point the server's token POST at an internal URL. The GCP payload keeps typed fields only and the collector uses `google.JWTTokenURL`.
- Method-pattern routes with a wildcard already exist (`POST /api/v1/ai/sessions/{id}/apply-plan`, the OIDC admin routes), so the four credential routes register the same way and the OpenAPI recorder understands them.
- `audit.ActionTest` exists for connectivity checks and the AI provider test uses it.
- The sync trigger picks a connected agent before calling `SyncService.Sync` (`discovery_handlers.go`, the `selectConnectedAgent` branch). `SyncService` is the seam; agent paths never see a stored credential.

## 2. Ownership, one owner per invariant

| Invariant | Owner | File |
|-----------|-------|------|
| Credential types and their provider | `credentialTypes` table; `CredentialType.Provider()` and `Valid()` derive from it | `internal/domain/credentials.go` |
| Field validation (ARN pattern, external ID length, key JSON shape, RSA PEM parses) | `NewAWSAssumeRole`, `ParseGCPServiceAccountKey`, reached only through `parseCredentialRequest` | `internal/discovery/credentials.go` |
| At-rest format | the canonical PUT request, JSON-encoded, then `secrets.Encrypt`; unsealing re-runs `parseCredentialRequest` | same file |
| Encryption key, ephemeral-key refusal | `AccountCredentials` (holds `key`, `durable`) | same file |
| Summary derivation | `CredentialPayload.summary()`, computed once in `Put`, stored beside the ciphertext | same file |
| Type matches account provider | `checkProvider`, called by `Put` and by `SyncService.bind` | same file, `collector.go` |
| A stored credential becomes a client | `SyncService.bind`, the only caller of `WithCredential`; `Sync` and `TestCredential` both use it | `internal/discovery/collector.go` |
| Test errors are safe to show | `diagnoseProbe`, fail-closed: only `*ProbeError` fields, a timeout, or "endpoint unreachable" | same file |
| GCP token endpoint is Google's | `gcp.WithCredential` sets `TokenURL: google.JWTTokenURL`; the payload has no `token_uri` field | `internal/discovery/gcp/credential.go` |
| Credential attaches only to a live account | `PutAccountCredential`, one statement with `SELECT ... FROM accounts WHERE deleted_at IS NULL` | each backend |
| Account delete removes the credential | SQL: an `AFTER UPDATE OF deleted_at` trigger in migrations 0022 and 0024. Memory: `delete(m.credentials, id)` in both delete methods under the held lock | migrations, `store.go` |
| Replace resets the test result | `PutAccountCredential` (its input has no test fields) | each backend |
| A test result never lands on a newer save | `RecordAccountCredentialTest` compares the stored ciphertext (fresh nonce per save) | each backend |

## 3. Domain (`internal/domain/credentials.go`, new)

```go
type CredentialType string

const (
	CredentialTypeAWSAssumeRole     CredentialType = "aws_assume_role"
	CredentialTypeGCPServiceAccount CredentialType = "gcp_service_account"
)

// credentialTypes is the one table every type-keyed rule derives from.
var credentialTypes = map[CredentialType]string{
	CredentialTypeAWSAssumeRole:     "aws",
	CredentialTypeGCPServiceAccount: "gcp",
}

func (t CredentialType) Valid() bool      // _, ok := credentialTypes[t]
func (t CredentialType) Provider() string // "" for an unknown type
func ValidCredentialTypes() []CredentialType // stable order, for errors and OpenAPI

type CredentialSummary struct {
	RoleARN       string `json:"role_arn,omitempty"`
	HasExternalID bool   `json:"has_external_id,omitempty"`
	ClientEmail   string `json:"client_email,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
}

type CredentialTestStatus string

const (
	CredentialTestOK     CredentialTestStatus = "ok"
	CredentialTestFailed CredentialTestStatus = "failed"
)

// CredentialTest is one test outcome. Error is sanitized text, never a value or a body.
type CredentialTest struct {
	TestedAt time.Time            `json:"tested_at"`
	Status   CredentialTestStatus `json:"status"`
	Error    string               `json:"error,omitempty"`
}

// CredentialTestResult is the response of POST .../credentials/test.
type CredentialTestResult struct {
	CredentialTest
	Identity string `json:"identity,omitempty"` // assumed-role ARN or service-account email; empty on failure
}

// AccountCredential is the API view. It has no field that can hold a secret or ciphertext.
type AccountCredential struct {
	AccountID int64             `json:"account_id"`
	Type      CredentialType    `json:"type"`
	Summary   CredentialSummary `json:"summary"`
	LastTest  *CredentialTest   `json:"last_test,omitempty"` // nil until tested after the last save
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// AccountCredentialRecord is the stored row. Only storage and discovery.AccountCredentials handle it.
type AccountCredentialRecord struct {
	AccountCredential
	PayloadEncrypted string `json:"-"`
}

// SealedAccountCredential is the input to PutAccountCredential. No test or time fields:
// the store stamps times and clears any previous test result.
type SealedAccountCredential struct {
	AccountID        int64
	Type             CredentialType
	Summary          CredentialSummary
	PayloadEncrypted string
}

// PutAccountCredentialRequest is the PUT body and the canonical form sealed at rest.
// Exactly the object named by Type must be set.
type PutAccountCredentialRequest struct {
	Type              CredentialType          `json:"type"`
	AWSAssumeRole     *AWSAssumeRoleInput     `json:"aws_assume_role,omitempty"`
	GCPServiceAccount *GCPServiceAccountInput `json:"gcp_service_account,omitempty"`
}

type AWSAssumeRoleInput struct {
	RoleARN    string `json:"role_arn"`
	ExternalID string `json:"external_id,omitempty"`
}

type GCPServiceAccountInput struct {
	KeyJSON string `json:"key_json"`
}
```

A test in `discovery` round-trips every `PutAccountCredentialRequest` variant through `Put` and `unseal` and fails when the DTO branches, the `credentialTypes` table, and the `parseCredentialRequest` switch disagree.

## 4. Storage (`internal/storage/credentials.go`, new; `store.go` edit)

```go
// AccountCredentialStore persists at most one sealed credential per live account.
// It never sees plaintext.
type AccountCredentialStore interface {
	// GetAccountCredential returns the credential of a live account, or ErrNotFound.
	GetAccountCredential(ctx context.Context, accountID int64) (domain.AccountCredentialRecord, error)
	// PutAccountCredential inserts or replaces in one statement that also requires the
	// account to be live (ErrNotFound otherwise). A replace keeps CreatedAt, moves
	// UpdatedAt, and clears the test result.
	PutAccountCredential(ctx context.Context, in domain.SealedAccountCredential) (domain.AccountCredentialRecord, error)
	// DeleteAccountCredential removes the credential and reports whether one existed.
	DeleteAccountCredential(ctx context.Context, accountID int64) (bool, error)
	// RecordAccountCredentialTest stores test only while the stored ciphertext still
	// equals sealedPayload; ErrConflict otherwise.
	RecordAccountCredentialTest(ctx context.Context, accountID int64, sealedPayload string, test domain.CredentialTest) error
}

// Store (edit): embeds AccountCredentialStore. All three backends implement it, so a
// backend that lacks credentials is a compile error, not a runtime fallback.
```

`MemoryStore` gains `credentials map[int64]domain.AccountCredentialRecord`; the four methods live in `credentials_memory.go`; `DeleteAccount` and `DeleteAccountCascade` each gain `delete(m.credentials, id)` under the lock they hold. SQLite and PostgreSQL methods live in `sqlite/credentials.go` and `postgres/credentials.go`. No `selectAccountCredentialStore`.

Conformance suite `internal/storage/storagetest/credentials.go`, run against memory, SQLite, and PostgreSQL (`-tags postgres`): put, get, replace clears the test and keeps `CreatedAt`, delete, record test, CAS conflict after a replace, put on a deleted account is `ErrNotFound`, account delete and cascade delete remove the credential.

SQL (SQLite; PostgreSQL mirrors it with `$n` and `seq_id`):

```sql
INSERT INTO account_credentials (account_id, type, payload_encrypted, summary, created_at, updated_at)
SELECT id, ?, ?, ?, ?, ? FROM accounts WHERE id = ? AND deleted_at IS NULL
ON CONFLICT(account_id) DO UPDATE SET
    type = excluded.type, payload_encrypted = excluded.payload_encrypted, summary = excluded.summary,
    last_tested_at = NULL, last_test_status = NULL, last_test_error = NULL, updated_at = excluded.updated_at;

UPDATE account_credentials SET last_tested_at = ?, last_test_status = ?, last_test_error = ?
WHERE account_id = ? AND payload_encrypted = ?;   -- 0 rows: ErrConflict
```

PostgreSQL reads join `accounts` on `seq_id`, `organization_id`, and `deleted_at IS NULL`; there is no `organization_id` column on the credential table. If `INSERT ... SELECT ... FOR SHARE ... ON CONFLICT` does not parse, use one transaction with `SELECT 1 FROM accounts ... FOR SHARE` followed by the upsert.

## 5. Migrations

`migrations/0022_account_credentials.sql`:

```sql
CREATE TABLE IF NOT EXISTS account_credentials (
    account_id        INTEGER PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    type              TEXT NOT NULL,
    payload_encrypted TEXT NOT NULL,
    summary           TEXT NOT NULL DEFAULT '{}',
    last_tested_at    TEXT NULL,
    last_test_status  TEXT NULL,
    last_test_error   TEXT NULL,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    CHECK ((last_tested_at IS NULL) = (last_test_status IS NULL))
);

-- Accounts are soft-deleted, so the foreign key never fires for them.
CREATE TRIGGER IF NOT EXISTS account_credentials_drop_on_account_delete
AFTER UPDATE OF deleted_at ON accounts
FOR EACH ROW WHEN NEW.deleted_at IS NOT NULL
BEGIN
    DELETE FROM account_credentials WHERE account_id = NEW.id;
END;
```

`migrations/postgres/0024_account_credentials.up.sql`: the same table with `BIGINT ... REFERENCES accounts(seq_id)`, `JSONB` summary, `TIMESTAMPTZ` times, and a plpgsql trigger function `drop_account_credential_on_soft_delete()` keyed on `NEW.seq_id`, following the trigger style in `001_core_schema.up.sql`. Timestamps are `TEXT` on SQLite to match migration 0021 and the accounts table.

Each `DeleteAccount` and `DeleteAccountCascade` in the SQL backends gets a one-line comment naming the trigger.

## 6. Discovery (`internal/discovery/credentials.go`, new; `collector.go` edit)

```go
var (
	ErrCredentialKeyEphemeral     = errors.New("cannot store a credential: CLOUDPAM_OIDC_ENCRYPTION_KEY is not set, so the stored credential would be lost at restart; set the key")
	ErrCredentialUnreadable       = errors.New("stored credential cannot be decrypted; check CLOUDPAM_OIDC_ENCRYPTION_KEY or save the credential again")
	ErrCredentialProviderMismatch = errors.New("credential type does not match the account provider")
	ErrCredentialUnsupported      = errors.New("provider does not support stored credentials")
)

// CredentialInputError names an invalid request field. Detail states the rule, never the value.
type CredentialInputError struct{ Field, Detail string }

// CredentialPayload is a decrypted credential. Sealed: only this package's types implement it.
// Payload values have no exported fields and implement fmt.Formatter without the secret.
type CredentialPayload interface {
	Type() domain.CredentialType
	Provider() string
	summary() domain.CredentialSummary
	canonical() domain.PutAccountCredentialRequest
}

type AWSAssumeRole struct{ roleARN, externalID string }
func NewAWSAssumeRole(roleARN, externalID string) (AWSAssumeRole, error) // pattern ^arn:aws[a-z-]*:iam::\d{12}:role/.+$, external ID <= 1224 chars
func (p AWSAssumeRole) RoleARN() string
func (p AWSAssumeRole) ExternalID() string

// GCPServiceAccount keeps only the fields discovery needs. token_uri is dropped at parse.
type GCPServiceAccount struct{ clientEmail, projectID, privateKeyID string; privateKey []byte }
func ParseGCPServiceAccountKey(keyJSON []byte) (GCPServiceAccount, error) // type == service_account; client_email, project_id, private_key present; PEM parses as RSA
func (p GCPServiceAccount) ClientEmail() string
func (p GCPServiceAccount) ProjectID() string
func (p GCPServiceAccount) PrivateKeyID() string
func (p GCPServiceAccount) PrivateKey() []byte // clone

func parseCredentialRequest(req domain.PutAccountCredentialRequest) (CredentialPayload, error)
func checkProvider(p CredentialPayload, account domain.Account) error

// AccountCredentials owns parsing, the provider rule, sealing, and the ephemeral-key refusal.
type AccountCredentials struct {
	store   storage.AccountCredentialStore
	key     []byte
	durable bool
}

func NewAccountCredentials(store storage.AccountCredentialStore, key []byte, durable bool) *AccountCredentials
func (c *AccountCredentials) Get(ctx context.Context, accountID int64) (domain.AccountCredential, error)
func (c *AccountCredentials) Put(ctx context.Context, account domain.Account, req domain.PutAccountCredentialRequest) (domain.AccountCredential, error)
func (c *AccountCredentials) Delete(ctx context.Context, accountID int64) (bool, error)
func (c *AccountCredentials) load(ctx context.Context, accountID int64) (domain.AccountCredentialRecord, error)
func (c *AccountCredentials) unseal(rec domain.AccountCredentialRecord) (CredentialPayload, error)
func (c *AccountCredentials) recordTest(ctx context.Context, rec domain.AccountCredentialRecord, test domain.CredentialTest) error
```

`Put` order: parse, `checkProvider`, `durable` check, `json.Marshal(p.canonical())`, `secrets.Encrypt`, `store.PutAccountCredential`. Errors: `*CredentialInputError`, `ErrCredentialProviderMismatch`, `ErrCredentialKeyEphemeral`, `storage.ErrNotFound`.

```go
// collector.go additions

// CredentialAware is implemented by host-identity collectors that can also run as a stored credential.
type CredentialAware interface {
	WithCredential(ctx context.Context, account domain.Account, cred CredentialPayload) (CredentialedCollector, error)
}

// CredentialedCollector is a collector bound to one stored credential. Only WithCredential
// produces one, so Identity can never run as the host identity.
type CredentialedCollector interface {
	Collector
	Identity(ctx context.Context, account domain.Account) (string, error)
}

// ProbeError is a provider failure reduced to fields safe to store and show. No free-text field.
type ProbeError struct {
	Step   string // "assume_role", "get_caller_identity", "token", "get_project"
	Status int
	Code   string // clipped to [A-Za-z0-9_.-]{0,64}
}

const credentialTestTimeout = 15 * time.Second

type SyncService struct {
	store       storage.DiscoveryStore
	collectors  map[string]Collector
	credentials *AccountCredentials // nil: every account runs as the host identity
}

func (s *SyncService) SetCredentials(c *AccountCredentials)
func (s *SyncService) TestCredential(ctx context.Context, account domain.Account) (domain.CredentialTestResult, error)
func (s *SyncService) collectorFor(ctx context.Context, account domain.Account, host Collector) (Collector, error)
func (s *SyncService) bind(ctx context.Context, account domain.Account, host Collector, p CredentialPayload) (CredentialedCollector, error)
func diagnoseProbe(err error) string
```

`Sync` keeps the provider lookup before `CreateSyncJob` and calls `collectorFor` after the job exists, so a credential failure is a failed job with `ErrorMessage` `credential: ...`. `collectorFor` returns the host collector only when `load` reports `ErrNotFound`. An unreadable, mismatched, or unsupported stored credential fails the run. There is no silent host fallback.

`TestCredential` loads, unseals, binds under one 15 second context, calls `Identity`, builds the result (`diagnoseProbe` on failure), and records it against the loaded record unless the client context is already done. `ErrConflict` from the record is logged and dropped. A failing credential is a result with `Status` failed, not an error. `storage.ErrNotFound` when nothing is stored.

## 7. Collectors

AWS (`internal/discovery/aws/credential.go`, new; `collector.go`, `assume_role.go` edit):

- `Collector` gains a `newSTSClient func(aws.Config) stsAPI` seam beside `newEC2Client`.
- `WithCredential` asserts `discovery.AWSAssumeRole`, loads the host config for the STS region (first `account.Regions` entry, else the host default, else `us-east-1`), builds `aws.NewCredentialsCache(newAssumeRoleProvider(stsClient, roleARN, externalID, sessionName))`, and returns an unexported `assumedRoleCollector` wrapping a `*Collector` that carries the host collector's seams.
- `assumedRoleCollector.Identity` retrieves the role credentials (step `assume_role`) then calls `sts:GetCallerIdentity` with them (step `get_caller_identity`) and returns the assumed-role ARN.
- `newAssumeRoleProvider` is the one assume-role builder. `AssumeRole(ctx, accountID, roleName, externalID)` keeps its signature for the agent and calls it.
- `probeError(step, err)` maps `smithy.APIError` and `awshttp.ResponseError` to `*discovery.ProbeError` (code and status, never the message).

GCP (`internal/discovery/gcp/credential.go`, new; `collector.go` edit):

- `WithCredential` asserts `discovery.GCPServiceAccount`, builds `jwt.Config{Email, PrivateKey, PrivateKeyID, Scopes: computeScope, TokenURL: google.JWTTokenURL}`, and returns an unexported `serviceAccountCollector` whose token source uses the receiver's injected `httpClient` as the base client (through `oauth2.HTTPClient` in the context) so tests route the token POST to a stub.
- `Identity` fetches a token (step `token`; `*oauth2.RetrieveError` gives status and error code) then `GET compute/v1/projects/{ProjectID(account)}` with the same client `Discover` uses (step `get_project`; body discarded unread) and returns the service-account email.
- `getHTTPClient` precedence: token source over the injected base client, then the injected client, then ADC.

## 8. API (`internal/api/account_credential_handlers.go`, new)

```go
type AccountCredentialServer struct {
	srv         *Server
	credentials *discovery.AccountCredentials
	sync        *discovery.SyncService
}

func NewAccountCredentialServer(srv *Server, credentials *discovery.AccountCredentials, sync *discovery.SyncService) *AccountCredentialServer
func (cs *AccountCredentialServer) RegisterProtectedAccountCredentialRoutes(dualMW Middleware, logger *slog.Logger)
// GET    /api/v1/accounts/{accountId}/credentials       accounts:read
// PUT    /api/v1/accounts/{accountId}/credentials       accounts:update
// DELETE /api/v1/accounts/{accountId}/credentials       accounts:update
// POST   /api/v1/accounts/{accountId}/credentials/test  accounts:update
func (cs *AccountCredentialServer) RegisterAccountCredentialRoutes() // no RBAC, for tests
```

Handlers decode with `decodeStrictJSON` (extracted from `ai_settings_handlers.go` into `server.go` and used by both), load the live account (400 bad id, 404 not found), delegate, map errors, audit. Error map for PUT: `*CredentialInputError` and `ErrCredentialProviderMismatch` to 400, `ErrCredentialKeyEphemeral` to 409, `storage.ErrNotFound` to 404, anything else to 500 with no detail. GET and DELETE: `storage.ErrNotFound` to 404 "no credential stored for this account". Test: `storage.ErrNotFound` to 404; a failing credential is a 200 with `status: failed`.

Audit: `audit.ResourceAccountCredential = "account_credential"`. PUT logs `ActionUpdate` with `Changes{After: {"type": ...}}`, DELETE logs `ActionDelete`, test logs `ActionTest` with `Changes{After: {"status": ...}}`. Never a value.

OpenAPI: route metadata for the four routes; component types `AccountCredential`, `PutAccountCredentialRequest`, `CredentialTestResult`.

`cmd/cloudpam/main.go`: move `parseOIDCEncryptionKey` above the discovery block; `discovery.NewAccountCredentials(store, secretKey, !secretKeyEphemeral)`; `syncService.SetCredentials(...)`; `api.NewAccountCredentialServer(...).RegisterProtectedAccountCredentialRoutes(...)`.

## 9. Deviations from the design spec

| Spec says | This file says | Why |
|-----------|----------------|-----|
| Flat `last_tested_at`, `last_test_status`, `last_test_error` | Nested `last_test` object, nil until tested | "tested_at set, status empty" becomes unrepresentable |
| Payload types in `domain` | Payload types in `discovery` with unexported fields | sealing and unsealing stay unexported; no package outside `discovery` can turn a payload into bytes |
| GCP stores the key JSON | GCP stores typed fields; `token_uri` dropped | closes the token-endpoint path through `CredentialsFromJSON` |
| `ON DELETE CASCADE` removes the row | an `AFTER UPDATE OF deleted_at` trigger removes it; FK kept for a future hard purge | all account deletes are soft |
| `NewMemoryAccountCredentialStore(store)` and `selectAccountCredentialStore` | methods on `MemoryStore`; `storage.Store` embeds the interface | one lock for account and credential; a missing backend is a compile error |
| `CredentialSource` func and `CredentialAware` returning `Collector` | `AccountCredentials` vault; `CredentialAware` returns `CredentialedCollector` with `Identity` | one decrypt owner; the test handler never type-switches per provider |
| Test audit event is `ActionUpdate` | `ActionTest` | it exists for this and the AI provider test uses it |
| Routes registered inside `account_handlers.go` | `account_credential_handlers.go` with method patterns | precedent in `ai_handlers.go` and `oidc_handlers.go`; the OpenAPI recorder understands them |
| `TIMESTAMP` columns on SQLite | `TEXT` | matches migration 0021 and the accounts table |

## 10. Decisions on the open questions

- STS region: first `account.Regions` entry, else the host default, else `us-east-1`.
- The GCP test reads the account's project, not the key's `project_id`, because that is what sync reads.
- No check that the role ARN's account matches the account record in this slice. `Identity` returns the assumed-role ARN, so the operator sees it. Filed as a follow-up.
- `accounts:update` is the permission for PUT, DELETE, and test, per the spec.
- The verify journey proves the failure path against an unreachable or refusing stub and the API contract (summary only, 404s, 409). A success path needs a token stub, which a later journey can add with an operator-only endpoint override that is not API-settable.

## 11. Delivery as a stack

1. `feat/275-account-credentials` (this branch): domain, storage with the conformance suite, migrations, the discovery vault, the collector bindings, and `SyncService` changes. Target `master`.
2. `feat/275-credential-api`: handlers, audit, OpenAPI, `main.go` wiring. Base is branch 1.
3. `feat/275-credential-ui`: Accounts page modal, Discovery help section, docs, verify journey. Base is branch 2.

Each branch builds, lints, and tests green on its own before the next starts.
