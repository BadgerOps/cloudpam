# SSO/OIDC Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add OIDC client support so enterprises can authenticate via their existing IdP (Okta, Azure AD, Authentik, etc.), with JIT user provisioning, silent session re-auth, and a toggle to disable local login.

**Architecture:** `coreos/go-oidc/v3` handles OIDC discovery and ID token verification. `golang.org/x/oauth2` handles the authorization code flow. On successful OIDC login, we create a standard CloudPAM session (no IdP tokens stored). A 30-second cached active-status check in `DualAuthMiddleware` enables immediate account deactivation.

**Tech Stack:** Go (`coreos/go-oidc/v3`, `golang.org/x/oauth2`), React/TypeScript, SQLite migrations

**Design doc:** `docs/plans/2026-02-18-sso-oidc-design.md`

---

## Task 1: Add Go Dependencies

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Add dependencies**

```bash
nix develop --command go get github.com/coreos/go-oidc/v3/oidc golang.org/x/oauth2
nix develop --command go mod tidy
```

**Step 2: Verify build**

Run: `nix develop --command go build ./cmd/cloudpam`
Expected: exit 0

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add coreos/go-oidc and golang.org/x/oauth2 for OIDC support"
```

---

## Task 2: Domain Types — OIDCProvider and User Fields

**Files:**
- Create: `internal/domain/oidc.go`
- Modify: `internal/auth/user.go`

**Step 1: Create `internal/domain/oidc.go`**

```go
package domain

import "time"

// OIDCProvider represents a configured OIDC identity provider.
type OIDCProvider struct {
    ID                    string            `json:"id"`
    Name                  string            `json:"name"`
    IssuerURL             string            `json:"issuer_url"`
    ClientID              string            `json:"client_id"`
    ClientSecretEncrypted string            `json:"-"`                         // never serialized
    ClientSecretMasked    string            `json:"client_secret,omitempty"`   // "****" for GET responses
    Scopes                string            `json:"scopes"`                    // space-separated
    RoleMapping           map[string]string `json:"role_mapping"`             // IdP group -> CloudPAM role
    DefaultRole           string            `json:"default_role"`
    AutoProvision         bool              `json:"auto_provision"`
    Enabled               bool              `json:"enabled"`
    CreatedAt             time.Time         `json:"created_at"`
    UpdatedAt             time.Time         `json:"updated_at"`
}

// RoleMappingEntry represents a single IdP group to CloudPAM role mapping.
type RoleMappingEntry struct {
    Claim string `json:"claim"` // e.g., "groups"
    Value string `json:"value"` // e.g., "cloudpam-admins"
    Role  string `json:"role"`  // e.g., "admin"
}
```

**Step 2: Add OIDC fields to `internal/auth/user.go` User struct**

Add these fields after `LastLoginAt`:

```go
AuthProvider string `json:"auth_provider,omitempty"` // "local" or "oidc"
OIDCSubject  string `json:"oidc_subject,omitempty"`  // IdP "sub" claim
OIDCIssuer   string `json:"oidc_issuer,omitempty"`   // IdP issuer URL
```

Also update `copyUser()` to copy the new fields.

**Step 3: Add `LocalAuthEnabled` to `internal/domain/settings.go` SecuritySettings**

Add field:

```go
LocalAuthEnabled bool `json:"local_auth_enabled"`
```

Update `DefaultSecuritySettings()` to set `LocalAuthEnabled: true`.

**Step 4: Run tests**

Run: `nix develop --command go test ./internal/... -count=1`
Expected: all pass (new fields have zero values, backwards compatible)

**Step 5: Commit**

```bash
git add internal/domain/oidc.go internal/auth/user.go internal/domain/settings.go
git commit -m "feat: add OIDCProvider domain type and OIDC fields on User"
```

---

## Task 3: Migration 0017 — OIDC Tables and User Columns

**Files:**
- Create: `migrations/0017_oidc_providers.sql`
- Modify: `internal/auth/user_sqlite.go` (add new columns to queries)
- Modify: `internal/auth/user_postgres.go` (add new columns to queries)

**Step 1: Create migration file**

```sql
-- 0017_oidc_providers.sql
-- OIDC provider configuration table
CREATE TABLE IF NOT EXISTS oidc_providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    issuer_url TEXT NOT NULL UNIQUE,
    client_id TEXT NOT NULL,
    client_secret_encrypted TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT 'openid profile email',
    role_mapping TEXT NOT NULL DEFAULT '{}',
    default_role TEXT NOT NULL DEFAULT 'viewer',
    auto_provision INTEGER NOT NULL DEFAULT 1,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Add OIDC fields to users table
ALTER TABLE users ADD COLUMN auth_provider TEXT NOT NULL DEFAULT 'local';
ALTER TABLE users ADD COLUMN oidc_subject TEXT;
ALTER TABLE users ADD COLUMN oidc_issuer TEXT;

-- Add local_auth_enabled to settings if not exists
INSERT OR IGNORE INTO settings (key, value, updated_at) VALUES ('local_auth_enabled', 'true', datetime('now'));
```

**Step 2: Update SQLite user queries**

In `internal/auth/user_sqlite.go`, update INSERT and SELECT queries to include `auth_provider`, `oidc_subject`, `oidc_issuer` columns. Follow existing column patterns.

**Step 3: Update PostgreSQL user queries**

In `internal/auth/user_postgres.go`, same updates. Also create a PostgreSQL-specific migration in `migrations/postgres/0017_oidc_providers.sql` using `BOOLEAN` instead of `INTEGER` and `TIMESTAMP` instead of `TEXT`.

**Step 4: Build with SQLite and verify migration applies**

Run: `nix develop --command go build -tags sqlite ./cmd/cloudpam`
Expected: exit 0

**Step 5: Commit**

```bash
git add migrations/0017_oidc_providers.sql migrations/postgres/0017_oidc_providers.sql internal/auth/user_sqlite.go internal/auth/user_postgres.go
git commit -m "feat: migration 0017 — oidc_providers table and user OIDC columns"
```

---

## Task 4: OIDCProviderStore Interface and Memory Implementation

**Files:**
- Create: `internal/storage/oidc.go`
- Create: `internal/storage/oidc_memory.go`
- Create: `internal/storage/oidc_memory_test.go`

**Step 1: Write the store interface**

Create `internal/storage/oidc.go`:

```go
package storage

import (
    "context"
    "cloudpam/internal/domain"
)

// OIDCProviderStore manages OIDC provider configurations.
type OIDCProviderStore interface {
    CreateProvider(ctx context.Context, p *domain.OIDCProvider) error
    GetProvider(ctx context.Context, id string) (*domain.OIDCProvider, error)
    GetProviderByIssuer(ctx context.Context, issuerURL string) (*domain.OIDCProvider, error)
    ListProviders(ctx context.Context) ([]*domain.OIDCProvider, error)
    ListEnabledProviders(ctx context.Context) ([]*domain.OIDCProvider, error)
    UpdateProvider(ctx context.Context, p *domain.OIDCProvider) error
    DeleteProvider(ctx context.Context, id string) error
}
```

**Step 2: Write failing tests**

Create `internal/storage/oidc_memory_test.go` with test cases:
- `TestOIDCMemoryStore_CreateAndGet` — create provider, get by ID, verify fields
- `TestOIDCMemoryStore_ListProviders` — create 2, list returns both
- `TestOIDCMemoryStore_ListEnabledProviders` — create 2 (one disabled), list returns only enabled
- `TestOIDCMemoryStore_Update` — create, update name, get confirms change
- `TestOIDCMemoryStore_Delete` — create, delete, get returns ErrNotFound
- `TestOIDCMemoryStore_DuplicateIssuer` — create 2 with same issuer URL, second fails

Run: `nix develop --command go test ./internal/storage/ -run TestOIDCMemory -v`
Expected: FAIL (MemoryOIDCProviderStore doesn't exist yet)

**Step 3: Implement in-memory store**

Create `internal/storage/oidc_memory.go` following the `MemorySettingsStore` pattern — `sync.RWMutex`, map by ID, secondary index by issuer URL.

**Step 4: Run tests**

Run: `nix develop --command go test ./internal/storage/ -run TestOIDCMemory -v`
Expected: all pass

**Step 5: Commit**

```bash
git add internal/storage/oidc.go internal/storage/oidc_memory.go internal/storage/oidc_memory_test.go
git commit -m "feat: OIDCProviderStore interface and in-memory implementation"
```

---

## Task 5: SQLite OIDCProviderStore Implementation

**Files:**
- Create: `internal/storage/sqlite/oidc.go`

**Step 1: Implement SQLite store**

Follow `internal/storage/sqlite/settings.go` pattern. Use the `oidc_providers` table from migration 0017. `role_mapping` is stored as JSON text, marshal/unmarshal with `encoding/json`.

**Step 2: Verify SQLite build**

Run: `nix develop --command go build -tags sqlite ./cmd/cloudpam`
Expected: exit 0

**Step 3: Run all tests**

Run: `nix develop --command go test ./internal/... -count=1`
Expected: all pass

**Step 4: Commit**

```bash
git add internal/storage/sqlite/oidc.go
git commit -m "feat: SQLite OIDCProviderStore implementation"
```

---

## Task 6: OIDC Provider Package — Discovery, Exchange, Claims

**Files:**
- Create: `internal/auth/oidc/provider.go`
- Create: `internal/auth/oidc/claims.go`
- Create: `internal/auth/oidc/provider_test.go`
- Create: `internal/auth/oidc/claims_test.go`

**Step 1: Write failing claims tests**

Create `internal/auth/oidc/claims_test.go`:
- `TestMapRole_GroupMatch` — groups contain "admins", mapping maps "admins" → admin role
- `TestMapRole_NoMatch` — no groups match, returns default role (viewer)
- `TestMapRole_HighestWins` — multiple groups match (admin + viewer), highest privilege wins
- `TestMapRole_EmptyGroups` — empty groups, returns default
- `TestMapRole_EmptyMapping` — no mapping configured, returns default

Run: `nix develop --command go test ./internal/auth/oidc/ -run TestMapRole -v`
Expected: FAIL

**Step 2: Implement claims.go**

```go
package oidc

import "cloudpam/internal/auth"

// Claims represents the extracted OIDC ID token claims.
type Claims struct {
    Subject string   `json:"sub"`
    Email   string   `json:"email"`
    Name    string   `json:"name"`
    Groups  []string `json:"groups"`
    Issuer  string   `json:"iss"`
}

// MapRole evaluates group-to-role mapping rules against claims.
// Returns the highest-privilege matching role, or defaultRole if no match.
func MapRole(claims Claims, mapping map[string]string, defaultRole auth.Role) auth.Role {
    // mapping: IdP group name -> CloudPAM role string
    // Iterate claims.Groups, find matches in mapping, return highest privilege
    bestRole := auth.RoleNone
    for _, group := range claims.Groups {
        if roleStr, ok := mapping[group]; ok {
            role := auth.ParseRole(roleStr)
            if auth.RoleLevel(role) > auth.RoleLevel(bestRole) {
                bestRole = role
            }
        }
    }
    if bestRole == auth.RoleNone {
        return defaultRole
    }
    return bestRole
}
```

**Step 3: Run claims tests**

Run: `nix develop --command go test ./internal/auth/oidc/ -run TestMapRole -v`
Expected: all pass

**Step 4: Write failing provider tests**

Create `internal/auth/oidc/provider_test.go`:
- `TestNewProvider_Discovery` — start mock OIDC server with `httptest.Server`, verify discovery succeeds
- `TestExchange_ValidCode` — mock token endpoint returns valid JWT, verify claims extracted
- `TestExchange_InvalidCode` — mock token endpoint returns error, verify Exchange returns error
- `TestAuthCodeURL` — verify URL contains client_id, redirect_uri, state, nonce, scope params

The mock OIDC server must serve:
- `GET /.well-known/openid-configuration` — discovery doc
- `GET /keys` — JWKS endpoint (generate a test RSA key pair)
- `POST /token` — token endpoint (return signed ID token)

**Step 5: Implement provider.go**

```go
package oidc

import (
    "context"
    gooidc "github.com/coreos/go-oidc/v3/oidc"
    "golang.org/x/oauth2"
)

// ProviderConfig holds the configuration for creating an OIDC provider.
type ProviderConfig struct {
    IssuerURL    string
    ClientID     string
    ClientSecret string
    RedirectURL  string
    Scopes       []string
}

// Provider wraps the OIDC verifier and OAuth2 config.
type Provider struct {
    oidcProvider *gooidc.Provider
    verifier     *gooidc.IDTokenVerifier
    oauth2Config oauth2.Config
}

// NewProvider creates a Provider by performing OIDC discovery.
func NewProvider(ctx context.Context, cfg ProviderConfig) (*Provider, error) { ... }

// AuthCodeURL generates the IdP redirect URL.
func (p *Provider) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string { ... }

// Exchange exchanges an auth code for tokens and returns verified claims.
func (p *Provider) Exchange(ctx context.Context, code string) (*Claims, error) { ... }
```

**Step 6: Run provider tests**

Run: `nix develop --command go test ./internal/auth/oidc/ -v`
Expected: all pass

**Step 7: Commit**

```bash
git add internal/auth/oidc/
git commit -m "feat: OIDC provider package — discovery, code exchange, claim mapping"
```

---

## Task 7: Client Secret Encryption

**Files:**
- Create: `internal/auth/oidc/crypto.go`
- Create: `internal/auth/oidc/crypto_test.go`

**Step 1: Write failing tests**

- `TestEncryptDecrypt_RoundTrip` — encrypt "my-secret", decrypt, verify matches
- `TestEncrypt_DifferentCiphertexts` — encrypt same plaintext twice, verify ciphertexts differ (random nonce)
- `TestDecrypt_InvalidKey` — encrypt with key A, decrypt with key B, verify error
- `TestDecrypt_CorruptedData` — decrypt garbage bytes, verify error
- `TestGenerateEncryptionKey` — verify returns 32 bytes

Run: `nix develop --command go test ./internal/auth/oidc/ -run TestEncrypt -v`
Expected: FAIL

**Step 2: Implement crypto.go**

AES-256-GCM encryption/decryption. Key is 32 bytes from `CLOUDPAM_OIDC_ENCRYPTION_KEY` env var (hex-encoded) or auto-generated.

```go
package oidc

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/hex"
    "io"
)

func Encrypt(plaintext string, key []byte) (string, error) { ... }
func Decrypt(ciphertext string, key []byte) (string, error) { ... }
func GenerateEncryptionKey() ([]byte, error) { ... }
```

**Step 3: Run tests**

Run: `nix develop --command go test ./internal/auth/oidc/ -run TestEncrypt -v`
Expected: all pass

**Step 4: Commit**

```bash
git add internal/auth/oidc/crypto.go internal/auth/oidc/crypto_test.go
git commit -m "feat: AES-256-GCM encryption for OIDC client secrets"
```

---

## Task 8: User Active-Status Cache in DualAuthMiddleware

**Files:**
- Modify: `internal/api/middleware.go`
- Modify: `internal/api/middleware_test.go`

**Step 1: Write failing tests**

Add to `internal/api/middleware_test.go`:
- `TestDualAuth_DeactivatedUserRejected` — create user, create session, deactivate user, request with session cookie returns 401
- `TestDualAuth_ActiveUserCached` — active user, two requests within 30s, verify only one `GetByID` call (mock user store with call counter)
- `TestDualAuth_CacheExpires` — active user, request, wait 30s (or manipulate time), second request triggers fresh lookup

**Step 2: Run tests**

Run: `nix develop --command go test ./internal/api/ -run TestDualAuth_Deactivated -v`
Expected: FAIL (no active-status check exists)

**Step 3: Implement cached active-status check**

In `DualAuthMiddleware`, after `userStore.GetByID` succeeds in the session path:

```go
// SECURITY TRADE-OFF: This cache balances database load against deactivation
// responsiveness. A 30-second TTL means a deactivated user could retain access
// for up to 30 seconds. Shorter TTL = more DB hits per request; longer TTL =
// wider security window. For instant revocation, set activeStatusCacheTTL to 0,
// which checks the DB on every session-authenticated request.
//
// At 100 req/s with 30s cache: ~1 DB lookup per 30s per user
// At 100 req/s with 0s cache:  ~100 DB lookups per second per user
```

Use a `sync.Map` keyed by user ID, storing `{isActive bool, checkedAt time.Time}`. Check if `time.Since(checkedAt) < 30s`; if so, use cached value. Otherwise, call `userStore.GetByID` and update cache.

Add the `!user.IsActive` check:

```go
if !user.IsActive {
    http.Error(w, `{"error":"account disabled"}`, http.StatusUnauthorized)
    return
}
```

**Step 4: Run tests**

Run: `nix develop --command go test ./internal/api/ -run TestDualAuth -v`
Expected: all pass

**Step 5: Run full test suite**

Run: `nix develop --command go test ./... -count=1`
Expected: all pass

**Step 6: Commit**

```bash
git add internal/api/middleware.go internal/api/middleware_test.go
git commit -m "security: cached user active-status check in DualAuthMiddleware

Adds a 30-second TTL cache for user active status to enable
immediate account deactivation without a DB hit per request.
See code comments for the security/performance trade-off."
```

---

## Task 9: OIDC Handlers — Login Redirect and Callback

**Files:**
- Create: `internal/api/oidc_handlers.go`
- Create: `internal/api/oidc_handlers_test.go`

**Step 1: Write failing tests**

Create `internal/api/oidc_handlers_test.go`:
- `TestOIDCLogin_RedirectsToIdP` — GET `/api/v1/auth/oidc/login?provider_id=test-provider`, verify 302 redirect to mock IdP with correct query params (client_id, redirect_uri, state, nonce, scope)
- `TestOIDCLogin_UnknownProvider` — GET with unknown provider_id, verify 404
- `TestOIDCLogin_DisabledProvider` — GET with disabled provider, verify 404
- `TestOIDCCallback_NewUser_JITProvision` — callback with valid code, user doesn't exist, verify: user created with auth_provider=oidc, session created, cookie set, redirects to /
- `TestOIDCCallback_ExistingUser_SessionCreated` — callback with valid code, user exists, verify: no new user created, session created
- `TestOIDCCallback_DeactivatedUser_Rejected` — callback for deactivated user, verify 403
- `TestOIDCCallback_InvalidState` — callback with mismatched state, verify 403
- `TestOIDCCallback_LocalAuthDisabled_StillWorks` — with local_auth_enabled=false, OIDC login still works
- `TestOIDCRefresh_Success` — POST `/api/v1/auth/oidc/refresh` with active session, verify success JSON
- `TestOIDCRefresh_ExpiredIdPSession` — POST refresh when IdP returns login_required, verify error JSON

All tests use a mock OIDC server (reuse test helpers from Task 6).

**Step 2: Create OIDCServer struct and handler stubs**

Create `internal/api/oidc_handlers.go`:

```go
package api

type OIDCServer struct {
    *Server
    oidcStore     storage.OIDCProviderStore
    sessionStore  auth.SessionStore
    userStore     auth.UserStore
    settingsStore storage.SettingsStore
    encryptionKey []byte
    callbackURL   string // from CLOUDPAM_OIDC_CALLBACK_URL or auto-detected
    // providerCache caches initialized oidc.Provider instances
    providerCache sync.Map // id -> *oidc.Provider
}

func NewOIDCServer(srv *Server, oidcStore storage.OIDCProviderStore,
    sessionStore auth.SessionStore, userStore auth.UserStore,
    settingsStore storage.SettingsStore, encryptionKey []byte, callbackURL string) *OIDCServer { ... }

// RegisterOIDCRoutes registers both public and protected OIDC routes.
func (os *OIDCServer) RegisterOIDCRoutes(dualMW Middleware, slogger *slog.Logger) {
    // Public routes (no auth required — these ARE the auth flow):
    os.mux.HandleFunc("GET /api/v1/auth/oidc/login", os.handleOIDCLogin)
    os.mux.HandleFunc("GET /api/v1/auth/oidc/callback", os.handleOIDCCallback)
    os.mux.HandleFunc("POST /api/v1/auth/oidc/refresh", os.handleOIDCRefresh)

    // Public read-only (for login page to list providers):
    os.mux.HandleFunc("GET /api/v1/auth/oidc/providers", os.handleListPublicProviders)

    // Admin-only management routes:
    adminRead := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionRead, slogger)
    adminWrite := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionWrite, slogger)

    os.mux.Handle("GET /api/v1/settings/oidc/providers",
        dualMW(adminRead(http.HandlerFunc(os.handleListProviders))))
    os.mux.Handle("POST /api/v1/settings/oidc/providers",
        dualMW(adminWrite(http.HandlerFunc(os.handleCreateProvider))))
    os.mux.Handle("GET /api/v1/settings/oidc/providers/{id}",
        dualMW(adminRead(http.HandlerFunc(os.handleGetProvider))))
    os.mux.Handle("PATCH /api/v1/settings/oidc/providers/{id}",
        dualMW(adminWrite(http.HandlerFunc(os.handleUpdateProvider))))
    os.mux.Handle("DELETE /api/v1/settings/oidc/providers/{id}",
        dualMW(adminWrite(http.HandlerFunc(os.handleDeleteProvider))))
    os.mux.Handle("POST /api/v1/settings/oidc/providers/{id}/test",
        dualMW(adminWrite(http.HandlerFunc(os.handleTestProvider))))
}
```

**Step 3: Implement `handleOIDCLogin`**

Flow:
1. Read `provider_id` query param
2. Look up provider config from store
3. Decrypt client secret
4. Call `oidc.NewProvider()` (cached)
5. Generate state + nonce, store in a short-lived cookie (`oidc_state`, 10 min, httponly)
6. Generate auth code URL with `prompt` param if present
7. Redirect (302)

**Step 4: Implement `handleOIDCCallback`**

Flow:
1. Read `code`, `state` from query params
2. Validate state matches `oidc_state` cookie, clear cookie
3. Call `provider.Exchange(ctx, code)` to get claims
4. Look up user by `(oidc_issuer, oidc_subject)` — if exists, use it
5. If not found, look up by email — if exists and auth_provider is "local", reject (user must link manually, or admin can change auth_provider)
6. If not found at all and `auto_provision` is true: create user with auth_provider=oidc, default role from provider config (or role mapping)
7. Check `user.IsActive` — reject if disabled
8. Create session (mirror login handler: `auth.NewSession`, `sessionStore.Create`, session limit enforcement, set cookie)
9. Audit log: `auth.oidc_login`
10. Redirect to `/` (or to a `redirect_uri` stored in the state cookie)

**Step 5: Implement `handleOIDCRefresh`**

Flow:
1. Get session from cookie (must be valid OIDC session)
2. Look up user's `oidc_issuer` to find provider
3. Generate auth URL with `prompt=none`
4. Return JSON `{"redirect_url": "..."}` for frontend iframe

**Step 6: Implement `handleListPublicProviders`**

Return only `id`, `name`, `enabled` for enabled providers. No secrets, no config details.

**Step 7: Run tests**

Run: `nix develop --command go test ./internal/api/ -run TestOIDC -v`
Expected: all pass

**Step 8: Commit**

```bash
git add internal/api/oidc_handlers.go internal/api/oidc_handlers_test.go
git commit -m "feat: OIDC login, callback, and refresh handlers with JIT provisioning"
```

---

## Task 10: OIDC Provider Admin CRUD Handlers

**Files:**
- Modify: `internal/api/oidc_handlers.go` (add admin handlers)
- Create: `internal/api/oidc_admin_test.go`

**Step 1: Write failing tests**

- `TestOIDCAdmin_CreateProvider` — POST valid provider config, verify 201 and stored
- `TestOIDCAdmin_CreateProvider_DuplicateIssuer` — POST with existing issuer URL, verify 409
- `TestOIDCAdmin_GetProvider_SecretMasked` — GET provider, verify client_secret is "****"
- `TestOIDCAdmin_UpdateProvider` — PATCH name and scopes, verify updated
- `TestOIDCAdmin_DeleteProvider` — DELETE, verify gone
- `TestOIDCAdmin_TestConnection` — POST test with valid issuer, verify discovery result
- `TestOIDCAdmin_TestConnection_InvalidIssuer` — POST test with bad URL, verify error

**Step 2: Implement admin handlers**

Each handler follows the settings handler pattern: validate input, call store, return JSON. `handleCreateProvider` encrypts the client secret before storing. `handleGetProvider` masks the secret in the response.

`handleTestProvider`: calls `oidc.NewProvider()` against the issuer URL and returns the discovered endpoints (authorization, token, userinfo, jwks). Does not perform a full auth flow.

**Step 3: Run tests**

Run: `nix develop --command go test ./internal/api/ -run TestOIDCAdmin -v`
Expected: all pass

**Step 4: Commit**

```bash
git add internal/api/oidc_handlers.go internal/api/oidc_admin_test.go
git commit -m "feat: OIDC provider admin CRUD with secret encryption and test endpoint"
```

---

## Task 11: Local Auth Toggle Enforcement

**Files:**
- Modify: `internal/api/user_handlers.go` (login handler)
- Modify: `internal/api/settings_handlers.go` (validation)
- Modify: `internal/api/user_handlers_test.go`
- Modify: `internal/api/settings_handlers_test.go`

**Step 1: Write failing tests**

In `internal/api/user_handlers_test.go`:
- `TestLogin_LocalAuthDisabled_Rejected` — set `local_auth_enabled=false` in settings, POST login with valid credentials, verify 403 "local authentication is disabled"

In `internal/api/settings_handlers_test.go`:
- `TestSettings_DisableLocalAuth_RequiresOIDCProvider` — PATCH local_auth_enabled=false with no OIDC providers configured, verify 400 "at least one OIDC provider must be configured"
- `TestSettings_DisableLocalAuth_RequiresLocalAdmin` — PATCH local_auth_enabled=false, verify at least one active local admin exists (break-glass)

**Step 2: Implement login handler check**

In the login handler (user_handlers.go), before password verification, check settings:

```go
settings, _ := us.settingsStore.GetSecuritySettings(ctx)
if settings != nil && !settings.LocalAuthEnabled {
    writeJSON(w, http.StatusForbidden, apiError{Error: "local authentication is disabled"})
    return
}
```

The login handler will need access to `settingsStore`. Add it as a field to the user handler struct or pass via functional option.

**Step 3: Implement settings validation**

In the PATCH handler for security settings, when `local_auth_enabled` changes to `false`:
1. Check at least one enabled OIDC provider exists
2. Check at least one active local admin user exists (break-glass)
3. Reject with 400 if either check fails

**Step 4: Run tests**

Run: `nix develop --command go test ./internal/api/ -run "TestLogin_LocalAuth|TestSettings_DisableLocalAuth" -v`
Expected: all pass

**Step 5: Commit**

```bash
git add internal/api/user_handlers.go internal/api/settings_handlers.go internal/api/user_handlers_test.go internal/api/settings_handlers_test.go
git commit -m "feat: local auth toggle — disable password login when OIDC is configured"
```

---

## Task 12: Wire OIDC into main.go

**Files:**
- Modify: `cmd/cloudpam/main.go`

**Step 1: Wire up OIDCProviderStore and OIDCServer**

After the settings subsystem block (around line 189), add:

```go
// OIDC subsystem
oidcStore := storage.NewMemoryOIDCProviderStore()
oidcEncKey := parseOIDCEncryptionKey(logger) // reads CLOUDPAM_OIDC_ENCRYPTION_KEY
oidcCallbackURL := os.Getenv("CLOUDPAM_OIDC_CALLBACK_URL")
oidcSrv := api.NewOIDCServer(srv, oidcStore, sessionStore, userStore, settingsStore, oidcEncKey, oidcCallbackURL)
logger.Info("oidc subsystem initialized")
```

After the settings routes registration:

```go
oidcSrv.RegisterOIDCRoutes(dualMW, logger.Slog())
```

Add `parseOIDCEncryptionKey` helper that reads hex-encoded key from env or auto-generates one (logging a warning that auto-generated keys don't survive restarts).

Also add OIDC login/callback to the CSRF exemption list in `csrf.go`.

**Step 2: Run full test suite**

Run: `nix develop --command go test ./... -count=1`
Expected: all pass

**Step 3: Commit**

```bash
git add cmd/cloudpam/main.go internal/api/csrf.go
git commit -m "feat: wire OIDC subsystem into main.go"
```

---

## Task 13: Frontend — Login Page SSO Buttons

**Files:**
- Create: `ui/src/hooks/useOIDCProviders.ts`
- Modify: `ui/src/pages/LoginPage.tsx`

**Step 1: Create the hook**

```typescript
// ui/src/hooks/useOIDCProviders.ts
import { useState, useEffect, useCallback } from 'react'
import { get } from '../api/client'

interface OIDCProvider {
  id: string
  name: string
}

export function useOIDCProviders() {
  const [providers, setProviders] = useState<OIDCProvider[]>([])
  const [loading, setLoading] = useState(true)

  const fetchProviders = useCallback(async () => {
    try {
      const data = await get<OIDCProvider[]>('/api/v1/auth/oidc/providers')
      setProviders(data)
    } catch {
      // Silently fail — no providers means no SSO buttons
      setProviders([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchProviders() }, [fetchProviders])
  return { providers, loading }
}
```

**Step 2: Add SSO buttons to LoginPage**

After the existing login form submit button, add:
- Horizontal divider with "or"
- One button per enabled provider: `<a href="/api/v1/auth/oidc/login?provider_id={id}">Sign in with {name}</a>`
- If `local_auth_enabled` is false (get from /healthz response), hide the username/password form entirely

**Step 3: TypeScript check**

Run: `nix develop --command bash -c 'cd ui && npx tsc --noEmit'`
Expected: no errors

**Step 4: Commit**

```bash
git add ui/src/hooks/useOIDCProviders.ts ui/src/pages/LoginPage.tsx
git commit -m "feat: SSO login buttons on login page"
```

---

## Task 14: Frontend — Security Settings OIDC Section

**Files:**
- Create: `ui/src/hooks/useOIDCAdmin.ts`
- Modify: `ui/src/pages/SecuritySettingsPage.tsx`

**Step 1: Create admin hook**

`useOIDCAdmin()` — full CRUD for OIDC providers via `/api/v1/settings/oidc/providers`. Same pattern as `useSecuritySettings` but with list/create/update/delete operations.

**Step 2: Replace "SSO / OIDC — coming soon" placeholder**

Replace the placeholder section with:
- Provider list table (name, issuer URL, enabled status, actions)
- "Add Provider" button → modal form: name, issuer URL, client ID, client secret, scopes, default role, auto-provision toggle, role mapping (JSON editor or key-value pairs)
- Edit/Delete actions per provider
- "Test Connection" button per provider
- "Disable local login" toggle (from SecuritySettings) with confirmation modal

**Step 3: TypeScript check**

Run: `nix develop --command bash -c 'cd ui && npx tsc --noEmit'`
Expected: no errors

**Step 4: UI build**

Run: `nix develop --command bash -c 'cd ui && npm run build'`
Expected: builds successfully

**Step 5: Commit**

```bash
git add ui/src/hooks/useOIDCAdmin.ts ui/src/pages/SecuritySettingsPage.tsx
git commit -m "feat: OIDC provider management UI in Security settings"
```

---

## Task 15: Frontend — Silent Session Re-auth Hook

**Files:**
- Create: `ui/src/hooks/useSessionRefresh.ts`
- Modify: `ui/src/App.tsx` (mount the hook)

**Step 1: Create the hook**

```typescript
// ui/src/hooks/useSessionRefresh.ts
// Only activates for OIDC-authenticated users.
// When session enters last 20% of lifetime, creates hidden iframe
// to attempt prompt=none re-auth with the IdP.
```

Logic:
1. Poll `/api/v1/auth/me` every 60 seconds to get session expiry
2. If user's `auth_provider` is not "oidc", do nothing
3. Calculate 20% threshold: if `remaining < totalDuration * 0.2`, trigger re-auth
4. Create hidden iframe: `src="/api/v1/auth/oidc/login?provider_id={id}&prompt=none"`
5. Listen for postMessage from callback page (callback should postMessage on success/failure)
6. On failure: show toast "Session expiring — please log in again"

**Step 2: Mount in App.tsx**

Add `useSessionRefresh()` call inside the authenticated layout component.

**Step 3: Update OIDC callback to postMessage when in iframe**

In `handleOIDCCallback`, detect if the request came from an iframe (check `Sec-Fetch-Dest: iframe` header or add a `&iframe=1` query param). If so, return an HTML page that calls `window.parent.postMessage({type: 'oidc-refresh', success: true}, '*')` instead of redirecting.

**Step 4: TypeScript check**

Run: `nix develop --command bash -c 'cd ui && npx tsc --noEmit'`
Expected: no errors

**Step 5: Commit**

```bash
git add ui/src/hooks/useSessionRefresh.ts ui/src/App.tsx internal/api/oidc_handlers.go
git commit -m "feat: silent session re-auth for OIDC users via hidden iframe"
```

---

## Task 16: Documentation and Version Bump

**Files:**
- Modify: `docs/CHANGELOG.md`
- Modify: `CLAUDE.md`

**Step 1: Update CHANGELOG**

Add v0.8.0 entry under `[Unreleased]`:
- SSO/OIDC provider integration with generic OIDC discovery
- JIT user provisioning from IdP claims with configurable role mapping
- Silent session re-authentication via `prompt=none`
- OIDC provider management UI in Config > Security
- Client secret encryption at rest (AES-256-GCM)
- Local auth toggle — disable password login when OIDC configured
- User active-status cache in auth middleware (30s TTL)
- New env vars: `CLOUDPAM_OIDC_ENCRYPTION_KEY`, `CLOUDPAM_OIDC_CALLBACK_URL`

**Step 2: Update CLAUDE.md**

- Add new API endpoints to the endpoints list
- Add new env vars to the Environment Variables section
- Add `internal/auth/oidc/` to the package table
- Update project structure tree
- Update implementation status to "Sprint 20 complete"

**Step 3: Commit**

```bash
git add docs/CHANGELOG.md CLAUDE.md
git commit -m "docs: update CHANGELOG and CLAUDE.md for v0.8.0 OIDC integration"
```

---

## Task 17: Final Verification

**Step 1: Run full test suite**

```bash
nix develop --command go test ./... -count=1
```
Expected: all pass, 0 failures

**Step 2: Run linter**

```bash
nix develop --command golangci-lint run ./...
```
Expected: 0 issues

**Step 3: Build all variants**

```bash
nix develop --command go build ./cmd/cloudpam
nix develop --command go build -tags sqlite ./cmd/cloudpam
```
Expected: both exit 0

**Step 4: TypeScript check**

```bash
nix develop --command bash -c 'cd ui && npx tsc --noEmit'
```
Expected: no errors

**Step 5: UI build**

```bash
nix develop --command bash -c 'cd ui && npm run build'
```
Expected: builds successfully

**Step 6: Create PR**

Push branch, create PR linking issues #130 and #131.
