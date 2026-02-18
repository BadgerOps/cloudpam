# SSO/OIDC Integration Design — Sprint 20

**Date:** 2026-02-18
**Issues:** #130 (SSO/OIDC Provider Integration), #131 (OIDC User Provisioning)
**Depends on:** Sprint 19 (auth hardening) — merged as PR #135

## Overview

Add OIDC client support to CloudPAM so enterprises can authenticate users via their existing identity provider (Okta, Azure AD, Google Workspace, Authentik, Keycloak, etc.). Local auth and OIDC coexist, with an admin toggle to disable local login once OIDC is configured.

## Approach

Use `github.com/coreos/go-oidc/v3` for OIDC discovery and ID token verification, combined with `golang.org/x/oauth2` for the OAuth2 authorization code flow. This is the standard Go pairing used by Kubernetes, Vault, and Dex.

CloudPAM does **not** store IdP access/refresh tokens. On OIDC login, we verify the ID token, extract claims, and create a standard CloudPAM session (identical to local login). This keeps the implementation simple and avoids storing sensitive IdP tokens.

## Data Model

### User struct additions

```go
type User struct {
    // ... existing fields ...
    AuthProvider  string  `json:"auth_provider"`            // "local" or "oidc"
    OIDCSubject   string  `json:"oidc_subject,omitempty"`   // IdP "sub" claim
    OIDCIssuer    string  `json:"oidc_issuer,omitempty"`    // IdP issuer URL
}
```

### New `oidc_providers` table (migration 0017)

```sql
CREATE TABLE oidc_providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    issuer_url TEXT NOT NULL UNIQUE,
    client_id TEXT NOT NULL,
    client_secret_encrypted TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT 'openid profile email',
    role_mapping TEXT NOT NULL DEFAULT '{}',
    default_role TEXT NOT NULL DEFAULT 'viewer',
    auto_provision BOOLEAN NOT NULL DEFAULT true,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

ALTER TABLE users ADD COLUMN auth_provider TEXT NOT NULL DEFAULT 'local';
ALTER TABLE users ADD COLUMN oidc_subject TEXT;
ALTER TABLE users ADD COLUMN oidc_issuer TEXT;
CREATE UNIQUE INDEX idx_users_oidc ON users(oidc_issuer, oidc_subject)
    WHERE oidc_subject IS NOT NULL;
```

### SecuritySettings addition

Add `local_auth_enabled` (bool, default `true`) to the existing `SecuritySettings` struct. When `false`, the login handler rejects password auth. At least one active local admin must exist as a break-glass account.

## Backend OIDC Package (`internal/auth/oidc/`)

### `provider.go` — Provider management

- `Provider` struct wrapping `coreos/go-oidc` verifier + `golang.org/x/oauth2` config
- `NewProvider(cfg ProviderConfig)` — performs OIDC discovery, caches the verifier
- `AuthCodeURL(state, nonce string)` — generates IdP redirect URL
- `Exchange(ctx, code string)` — exchanges auth code, verifies ID token, returns claims

### `claims.go` — Claim extraction and role mapping

- `Claims` struct: `Subject`, `Email`, `Name`, `Groups []string`, `Issuer`
- `MapRole(claims Claims, mapping RoleMapping) Role` — evaluates group-to-role rules, falls back to default role

### `store.go` — OIDCProviderStore interface

- `Create/Get/Update/Delete/List` for provider configs
- Memory, SQLite, and PostgreSQL implementations
- Client secret encrypted at rest using AES-256-GCM
- Encryption key from `CLOUDPAM_OIDC_ENCRYPTION_KEY` env var, or auto-generated and persisted in settings table on first use

## Session Lifecycle

### Silent re-authentication

When a user's CloudPAM session approaches expiry (last 20% of lifetime), the frontend auto-triggers silent re-auth:

1. Frontend creates a hidden iframe pointing to `/api/v1/auth/oidc/login?prompt=none&provider_id=...`
2. Server passes `prompt=none` to the IdP
3. If IdP session is alive: returns new auth code silently, server extends CloudPAM session
4. If IdP session expired: returns `login_required` error, frontend shows "Session expiring, please log in again"
5. Only activates for OIDC-authenticated users (local auth sessions are unaffected)

### Account deactivation — immediate revocation

When an admin deactivates a user (`is_active=false`):

1. Server immediately calls `DeleteByUserID` to revoke all sessions
2. `DualAuthMiddleware` checks user active status on every session-authenticated request
3. Uses a **30-second TTL cache** to avoid a DB hit on every request

> **IMPORTANT TRADE-OFF (document in code):** The 30-second cache means a deactivated user could have up to 30 seconds of access before being cut off. A shorter TTL increases DB load; a longer TTL increases the security window. For most deployments 30 seconds is acceptable. If instant revocation is required, set the cache TTL to 0 (every request checks DB).

## API Endpoints

### OIDC auth flow

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/v1/auth/oidc/login` | Generate state/nonce, redirect to IdP. Query params: `provider_id`, optional `prompt=none` |
| GET | `/api/v1/auth/oidc/callback` | Exchange code, verify ID token, JIT provision, create session, redirect to UI |
| POST | `/api/v1/auth/oidc/refresh` | Silent re-auth endpoint for frontend iframe (returns JSON) |

### OIDC provider management (admin-only)

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/v1/settings/oidc/providers` | List providers |
| POST | `/api/v1/settings/oidc/providers` | Create provider |
| GET | `/api/v1/settings/oidc/providers/{id}` | Get provider (secret masked) |
| PATCH | `/api/v1/settings/oidc/providers/{id}` | Update provider |
| DELETE | `/api/v1/settings/oidc/providers/{id}` | Delete provider |
| POST | `/api/v1/settings/oidc/providers/{id}/test` | Test OIDC discovery |

### Local auth toggle

Covered by existing `PATCH /api/v1/settings/security` — adds `local_auth_enabled` field. Login handler checks this before accepting password auth.

## Frontend Changes

### Login page

- Fetch enabled providers from `/api/v1/settings/oidc/providers` (public endpoint, returns `id` and `name` only)
- Render "Sign in with {name}" button(s) below local login form
- If `local_auth_enabled` is `false`, hide username/password form, show only SSO buttons

### Silent re-auth hook (`useSessionRefresh`)

- Polls `/api/v1/auth/me` for session expiry
- At 20% remaining lifetime, creates hidden iframe for `prompt=none` re-auth
- Listens for postMessage from callback page
- On failure: toast "Session expiring — please log in again"
- Only for OIDC users

### Security settings page

Replace "SSO / OIDC — coming soon" placeholder with:

- Provider list with add/edit/delete
- Per-provider config form: name, issuer URL, client ID, client secret, scopes, role mapping rules, default role, auto-provision toggle
- "Test Connection" button
- "Disable local login" toggle with confirmation modal

## Testing Strategy

### Unit tests (CI/CD — mocked OIDC)

- `internal/auth/oidc/provider_test.go` — mock OIDC server via `httptest.Server` serving `.well-known/openid-configuration`, JWKS, and token endpoint. Tests: discovery, code exchange, ID token verification, claim extraction
- `internal/auth/oidc/claims_test.go` — role mapping: group matches, no match defaults, multiple matches take highest privilege, empty groups
- `internal/auth/oidc/store_test.go` — CRUD, client secret encryption/decryption round-trip
- `internal/api/oidc_handlers_test.go` — login redirect URL correctness, callback with valid code creates session, invalid state returns 403, JIT provisioning creates user with correct role, deactivated user blocked
- `internal/api/middleware_test.go` — active-status cache: deactivated user rejected after cache TTL, active user passes
- Frontend: `useSessionRefresh` hook with mocked timers

### Integration test (local dev, not CI)

- `docker-compose.oidc-test.yml` with Authentik + CloudPAM
- Manual smoke test script: create Authentik application, configure provider, test login, JIT provisioning, session re-auth, account deactivation

## Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `CLOUDPAM_OIDC_ENCRYPTION_KEY` | AES-256 key for encrypting client secrets at rest | Auto-generated on first use |
| `CLOUDPAM_OIDC_CALLBACK_URL` | Public callback URL override (for reverse proxy setups) | Auto-detected from request |

## Dependencies

New Go dependencies:
- `github.com/coreos/go-oidc/v3` — OIDC discovery and ID token verification
- `golang.org/x/oauth2` — OAuth2 authorization code flow (likely already indirect)
