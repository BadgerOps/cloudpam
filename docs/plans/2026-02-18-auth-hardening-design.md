# Auth Hardening Design

**Date:** 2026-02-18
**Status:** Approved
**Sprint:** 19 (implementation), 20 (OIDC issues pre-created)

## Context

CloudPAM's auth system has solid foundations (Argon2id API keys, bcrypt passwords, RBAC with 4 roles, dual session+API key auth, setup wizard) but ships with auth disabled by default. The `CLOUDPAM_AUTH_ENABLED` toggle means deployments can accidentally run wide open. Enterprise multi-team usage requires auth-always, hardened sessions, and a path to SSO/OIDC.

## Decision

**Approach A: Auth-Always + Fix Bugs First.** Flip the default so auth is always required, fix the known security vulnerabilities, harden session management, then pre-create issues for OIDC/SSO in the next sprint.

## Sprint 19: Auth Hardening (Implement)

### 1. Auth-Always Default

Remove the `CLOUDPAM_AUTH_ENABLED` env var. Auth is always on.

**Startup flow:**
1. Server starts, checks user store
2. Zero users → `needsSetup=true`, only setup wizard + healthz/readyz/metrics accessible
3. Users exist → full `DualAuthMiddleware(required=true)` enforcement

**Dev convenience preserved:** `CLOUDPAM_ADMIN_USERNAME` + `CLOUDPAM_ADMIN_PASSWORD` env vars auto-seed an admin at startup (idempotent). CI and docker-compose use these. Running `just dev` with no env vars shows the setup wizard on first visit.

**What gets removed:**
- The `if authEnabled || needsSetup` branch in `cmd/cloudpam/main.go`
- `RegisterRoutes()` (unprotected variant) — everything uses `RegisterProtectedRoutes()`
- `CLOUDPAM_AUTH_ENABLED` env var and all documentation references

**What gets fixed along the way:**
- Import routes (`/api/v1/import/accounts`, `/api/v1/import/pools`) registered in protected routes with appropriate permissions
- `s.logAudit()` extracts actor from request context instead of hardcoding "anonymous"

### 2. P0 Security Bug Fixes

#### 2a. API Key Scope Elevation Prevention

**Bug:** An `operator` with `keys:write` can create an API key with `"*"` (admin) scope.

**Fix:** In `createAPIKey` handler, compare requested scopes against the caller's effective role via `auth.GetRoleFromScopes()`. Reject with `403 Forbidden` if any requested scope maps to a higher role than the caller's.

#### 2b. Login Rate Limiting

**Current state:** Global rate limiter at configurable RPS. No per-endpoint limiting on `/api/v1/auth/login`.

**Fix:** Dedicated login rate limiter — configurable attempts per minute per IP (default: 5) on the login endpoint. Implemented as a middleware wrapper on the login handler. Returns `429 Too Many Requests` with `Retry-After` header.

#### 2c. Trusted Proxy Configuration

**Bug:** `X-Forwarded-For` trusted blindly in `clientKey()` — attackers can spoof to bypass IP rate limiting.

**Fix:** New configurable trusted proxy list (default: empty = trust direct connection only). `clientKey()` only reads `X-Forwarded-For` if the direct peer IP is in the trusted proxy list. Configurable via Security settings page and `CLOUDPAM_TRUSTED_PROXIES` env var (initial default).

#### 2d. Register Import Routes in Protected Mode

**Bug:** `POST /api/v1/import/accounts` and `POST /api/v1/import/pools` missing from `RegisterProtectedRoutes` — 404 when auth enabled.

**Fix:** Add both routes requiring `pools:create` / `accounts:create` permissions.

#### 2e. Fix Audit Actor Attribution

**Bug:** `s.logAudit()` hardcodes `"anonymous"` even when auth context is available.

**Fix:** Extract actor from `auth.UserFromContext(r.Context())` or `auth.APIKeyFromContext(r.Context())`, fall back to `"anonymous"` only when neither is present.

### 3. Session & Security Configuration

#### 3a. Security Settings API + UI

New API endpoint: `GET/PATCH /api/v1/settings/security` (admin only).

```json
{
  "session_duration_hours": 24,
  "max_sessions_per_user": 10,
  "password_min_length": 12,
  "password_max_length": 72,
  "login_rate_limit_per_minute": 5,
  "account_lockout_attempts": 0,
  "trusted_proxies": []
}
```

**Storage:** New `settings` table (key-value with JSON value column). Loaded at startup, cached in memory, reloaded on PATCH. Env vars are the initial defaults; DB values override.

**UI:** New "Security" page under Settings:

```
Settings > Security
├── Session Management
│   ├── Session duration (hours)
│   ├── Max concurrent sessions per user
│   └── [Revoke All Sessions] (per-user, in user management)
├── Password Policy
│   ├── Minimum length
│   └── Maximum length (72 max, bcrypt limit)
├── Login Protection
│   ├── Rate limit (attempts per minute per IP)
│   └── Account lockout threshold (0 = disabled)
└── Network
    └── Trusted proxy CIDRs
```

Future sections (hidden until implemented): Roles & Permissions, SSO/OIDC, API Key Policies.

#### 3b. Configurable Session Duration

Default 24 hours. Managed via Security settings. Replaces hardcoded `DefaultSessionDuration`.

#### 3c. Max Concurrent Sessions Per User

Default 10. When a new login exceeds the limit, evict the oldest session. Configurable via Security settings.

#### 3d. Revoke All Sessions API

New endpoint: `POST /api/v1/auth/users/{id}/revoke-sessions`. Requires `admin` role or the user themselves. Uses existing `sessionStore.DeleteByUserID()`. Wired into user management UI.

#### 3e. Remove Bearer-as-Session-Token (Strategy 3)

Remove the undocumented `Authorization: Bearer <session-id>` path (without `cpam_` prefix) in `DualAuthMiddleware`. Sessions use cookies only; API keys use Bearer tokens. Clean separation prevents session ID leakage in HTTP headers and proxy logs.

#### 3f. Password Policy Hardening

- Minimum 12 characters (up from 8), configurable via Security settings
- Maximum 72 characters (bcrypt truncation boundary — reject with clear error)
- No complexity requirements (length-based per NIST 800-63B)

#### 3g. CSRF Protection

CSRF token middleware for session-authenticated state-changing requests (POST/PATCH/DELETE). Server generates a random token, sends it in `X-CSRF-Token` response header on GET requests. Frontend includes it in subsequent state-changing requests. API key requests are exempt (no cookies = no CSRF risk).

## Sprint 20: OIDC + Enterprise Auth (Issues Pre-Created)

These are filed as GitHub issues for the next sprint:

### SSO/OIDC Provider Integration
- OIDC client in `internal/auth/oidc.go`
- Generic OIDC discovery (`.well-known/openid-configuration`)
- Configuration via Security settings page
- Login flow: `/api/v1/auth/oidc/login` → IdP redirect → `/api/v1/auth/oidc/callback` → local session
- OIDC claim → CloudPAM role mapping (configurable)
- Frontend: "Sign in with SSO" button on login page

### OIDC User Provisioning (JIT)
- Auto-create local user on first OIDC login
- Map IdP groups/claims to CloudPAM roles
- Configurable default role for new OIDC users
- Option to disable local auth once OIDC is configured

### Custom RBAC Roles
- Admin-defined custom roles beyond the 4 built-in ones
- UI in Settings > Security > Roles & Permissions
- `roles` + `role_permissions` tables
- Custom `resource:action` permission sets

### API Key Policy Management
- Configurable default API key expiry
- Maximum allowed scopes per role
- Rotation reminders / forced expiry
- Audit log entries for approaching expiration

### Account Lockout & Failed Login Tracking
- Track failed login attempts per username in DB
- Lock after N failures (configurable, default disabled)
- Auto-unlock after configurable cooldown
- Admin manual unlock via user management UI

## Migration Impact

- New migration: `settings` table
- `CLOUDPAM_AUTH_ENABLED` env var removed (breaking change — document in CHANGELOG)
- `RegisterRoutes()` removed, only `RegisterProtectedRoutes()` remains
- Existing deployments with `CLOUDPAM_AUTH_ENABLED=false` will now require setup on first boot
