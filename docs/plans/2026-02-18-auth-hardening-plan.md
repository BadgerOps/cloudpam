# Auth Hardening Sprint 19 - Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make auth always-on, fix known security vulnerabilities, harden session management, and add a Security settings UI page.

**Architecture:** Remove the auth toggle so all deployments enforce authentication. Fix P0 security bugs (scope elevation, login rate limiting, trusted proxies, missing routes, audit attribution). Add a settings table for runtime security configuration. Build a Security settings page in the frontend under the existing Config section.

**Tech Stack:** Go 1.24, React/TypeScript/Vite, SQLite/PostgreSQL, Tailwind CSS, lucide-react icons

**IMPORTANT:** All shell commands must be prefixed with `nix develop --command` or run inside `nix develop` shell. Node/npm/npx/Go are NOT in the default PATH.

---

### Task 1: Auth-Always — Remove the Toggle

**Files:**
- Modify: `cmd/cloudpam/main.go` (lines 173-220)
- Modify: `internal/api/server.go` (lines 147-178 — remove `RegisterRoutes`)

**Step 1: Modify `cmd/cloudpam/main.go`**

Remove the `authEnabled` env var check and the else branch. The server always uses protected routes:

```go
// REMOVE these lines (~175-176):
// authEnabled := os.Getenv("CLOUDPAM_AUTH_ENABLED") == "true" || os.Getenv("CLOUDPAM_AUTH_ENABLED") == "1"
// needsSetup := len(existingUsers) == 0

// REPLACE the if/else block with just the protected path:
needsSetup := len(existingUsers) == 0

srv.RegisterProtectedRoutes(keyStore, sessionStore, userStore, logger.Slog())
authSrv := api.NewAuthServerWithStores(srv, keyStore, sessionStore, userStore, auditLogger)
authSrv.RegisterProtectedAuthRoutes(logger.Slog())
userSrv := api.NewUserServer(srv, keyStore, userStore, sessionStore, auditLogger)
userSrv.RegisterProtectedUserRoutes(logger.Slog())
dualMW := api.DualAuthMiddleware(keyStore, sessionStore, userStore, true, logger.Slog())
discoverySrv.RegisterProtectedDiscoveryRoutes(dualMW, logger.Slog())
analysisSrv.RegisterProtectedAnalysisRoutes(dualMW, logger.Slog())
recSrv.RegisterProtectedRecommendationRoutes(dualMW, logger.Slog())
aiSrv.RegisterProtectedAIPlanningRoutes(dualMW, logger.Slog())

if needsSetup {
    srv.SetNeedsSetup(true)
    logger.Info("first-boot setup required", "hint", "visit the UI to create an admin account")
} else {
    logger.Info("authentication enforced", "users", len(existingUsers))
}
```

Remove the log line about `CLOUDPAM_AUTH_ENABLED` and the "authentication disabled" message from the else branch.

**Step 2: Remove `RegisterRoutes()` from `internal/api/server.go`**

Delete the entire `RegisterRoutes()` method (lines 147-178). Keep only `RegisterProtectedRoutes()`.

**Step 3: Add import routes to `RegisterProtectedRoutes()`**

In `RegisterProtectedRoutes()`, after the export route registration, add:

```go
// Import routes (were missing from protected registration)
mux.Handle("POST /api/v1/import/accounts", dualMW(api.RequirePermissionMiddleware("accounts", "create", slogger)(http.HandlerFunc(s.handleImportAccounts))))
mux.Handle("POST /api/v1/import/pools", dualMW(api.RequirePermissionMiddleware("pools", "create", slogger)(http.HandlerFunc(s.handleImportPools))))
```

**Step 4: Update `handleHealth` to remove `auth_enabled` field**

In `internal/api/system_handlers.go`, the `/healthz` response should always report `auth_enabled: true`. Remove the dynamic field and hardcode it:

```go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, http.StatusOK, map[string]any{
        "status":             "ok",
        "auth_enabled":       true,
        "local_auth_enabled": true,
        "needs_setup":        s.needsSetup,
    })
}
```

**Step 5: Update testutil to always use protected routes**

In `internal/testutil/testutil.go`, update `NewTestServer` to call `RegisterProtectedRoutes` instead of `RegisterRoutes`, and always wire `DualAuthMiddleware`. This ensures tests reflect the production auth-always behavior.

**Step 6: Run tests**

```bash
nix develop --command go test ./... 2>&1
```

Expected: Some tests may fail because they relied on unprotected routes. Fix any that break by adding test API keys or session setup.

**Step 7: Run lint**

```bash
nix develop --command golangci-lint run ./... 2>&1
```

Expected: 0 issues

**Step 8: Commit**

```bash
git add -A && git commit -m "feat: auth-always default, remove CLOUDPAM_AUTH_ENABLED toggle

- Remove RegisterRoutes() (unprotected variant)
- All routes now use RegisterProtectedRoutes()
- Add missing import routes to protected registration
- BREAKING: CLOUDPAM_AUTH_ENABLED env var removed
- First boot always shows setup wizard"
```

---

### Task 2: Fix Audit Actor Attribution

**Files:**
- Modify: `internal/api/server.go` — `logAudit()` method (lines 116-136)

**Step 1: Write test for audit actor extraction**

In `internal/api/server_test.go`, add a test that verifies `logAudit` extracts the actor from context when a user or API key is present.

**Step 2: Fix `logAudit()` to extract actor from context**

```go
func (s *Server) logAudit(ctx context.Context, action, resourceType, resourceID, resourceName string, statusCode int) {
    if s.auditLogger == nil {
        return
    }

    actor := "anonymous"
    actorType := audit.ActorTypeAnonymous

    // Try to extract actor from auth context
    if user := auth.UserFromContext(ctx); user != nil {
        actor = user.Username
        actorType = audit.ActorTypeUser
    } else if key := auth.APIKeyFromContext(ctx); key != nil {
        actor = key.Name
        actorType = audit.ActorTypeAPIKey
    }

    event := &audit.AuditEvent{
        ID:           uuid.New().String(),
        Timestamp:    time.Now().UTC(),
        Actor:        actor,
        ActorType:    actorType,
        Action:       action,
        ResourceType: resourceType,
        ResourceID:   resourceID,
        ResourceName: resourceName,
        StatusCode:   statusCode,
    }

    // Extract request ID from context if available
    if reqID, ok := ctx.Value(requestIDKey).(string); ok {
        event.RequestID = reqID
    }

    if err := s.auditLogger.Log(ctx, event); err != nil {
        s.logger.Warn("audit log failed", "error", err)
    }
}
```

Note: This requires importing `"cloudpam/internal/auth"` in `server.go`. Check if it's already imported; if not, add it.

**Step 3: Run tests and commit**

```bash
nix develop --command go test ./internal/api/... -v 2>&1
git add -A && git commit -m "fix: extract audit actor from auth context instead of hardcoding anonymous"
```

---

### Task 3: API Key Scope Elevation Prevention

**Files:**
- Modify: `internal/api/auth_handlers.go` — `createAPIKey` method (lines 154-263)
- Modify: `internal/auth/rbac.go` — add `RoleLevel()` helper
- Test: `internal/api/auth_handlers_test.go`

**Step 1: Add `RoleLevel()` to `internal/auth/rbac.go`**

Add a function that returns the numeric privilege level of a role (for comparison):

```go
// RoleLevel returns the privilege level of a role for comparison.
// Higher values = more privileges.
func RoleLevel(r Role) int {
    switch r {
    case RoleAdmin:
        return 4
    case RoleOperator:
        return 3
    case RoleViewer:
        return 2
    case RoleAuditor:
        return 1
    default:
        return 0
    }
}
```

**Step 2: Write failing test**

In `internal/api/auth_handlers_test.go`, add a test that:
1. Creates a session for an `operator` user
2. Attempts to create an API key with `"*"` scope (admin-level)
3. Expects `403 Forbidden`

Also add a positive test:
1. Operator creates a key with `"pools:read"` scope
2. Expects `201 Created`

**Step 3: Add scope elevation check to `createAPIKey`**

In `internal/api/auth_handlers.go`, after the scope validation loop, add:

```go
// Check for scope elevation: caller cannot create keys with higher privileges
callerRole := auth.GetEffectiveRole(r.Context())
requestedRole := auth.GetRoleFromScopes(input.Scopes)
if auth.RoleLevel(requestedRole) > auth.RoleLevel(callerRole) {
    writeErr(w, http.StatusForbidden, "scope elevation denied", "requested scopes require a higher privilege level than your current role")
    return
}
```

**Step 4: Run tests and commit**

```bash
nix develop --command go test ./internal/api/... ./internal/auth/... -v 2>&1
git add -A && git commit -m "fix: prevent API key scope elevation — callers cannot grant higher privileges than their own role"
```

---

### Task 4: Trusted Proxy Configuration

**Files:**
- Modify: `internal/api/middleware.go` — `clientKey()` function (lines 283-297)
- Modify: `internal/api/middleware.go` — add `TrustedProxies` to config
- Test: `internal/api/middleware_test.go`

**Step 1: Add TrustedProxies field to rate limit or create a new config struct**

In `internal/api/middleware.go`, add a package-level variable for trusted proxies (will later be sourced from settings):

```go
// TrustedProxyConfig holds trusted proxy CIDR list.
type TrustedProxyConfig struct {
    CIDRs []netip.Prefix
}

// parseTrustedProxies parses a comma-separated list of CIDRs.
func ParseTrustedProxies(raw string) (*TrustedProxyConfig, error) {
    if raw == "" {
        return &TrustedProxyConfig{}, nil
    }
    var cidrs []netip.Prefix
    for _, s := range strings.Split(raw, ",") {
        s = strings.TrimSpace(s)
        if s == "" {
            continue
        }
        prefix, err := netip.ParsePrefix(s)
        if err != nil {
            return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", s, err)
        }
        cidrs = append(cidrs, prefix)
    }
    return &TrustedProxyConfig{CIDRs: cidrs}, nil
}

func (tc *TrustedProxyConfig) IsTrusted(remoteAddr string) bool {
    if len(tc.CIDRs) == 0 {
        return false
    }
    host, _, err := net.SplitHostPort(remoteAddr)
    if err != nil {
        return false
    }
    addr, err := netip.ParseAddr(host)
    if err != nil {
        return false
    }
    for _, cidr := range tc.CIDRs {
        if cidr.Contains(addr) {
            return true
        }
    }
    return false
}
```

**Step 2: Update `clientKey()` to respect trusted proxies**

Change `clientKey()` signature to accept a `*TrustedProxyConfig`:

```go
func clientKeyWithProxies(r *http.Request, proxies *TrustedProxyConfig) string {
    // Only trust X-Forwarded-For if the direct peer is a trusted proxy
    if proxies != nil && proxies.IsTrusted(r.RemoteAddr) {
        if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
            parts := strings.SplitN(xff, ",", 2)
            if ip := strings.TrimSpace(parts[0]); ip != "" {
                return ip
            }
        }
    }
    host, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        return r.RemoteAddr
    }
    return host
}
```

Update `RateLimitMiddleware` to accept and pass through the proxy config.

**Step 3: Wire in `cmd/cloudpam/main.go`**

Parse `CLOUDPAM_TRUSTED_PROXIES` env var at startup and pass to the rate limiter.

**Step 4: Write tests and commit**

Test cases: spoofed XFF without trusted proxy (should use RemoteAddr), spoofed XFF with trusted proxy (should use XFF), no XFF header.

```bash
nix develop --command go test ./internal/api/... -run TestTrustedProxy -v 2>&1
git add -A && git commit -m "fix: only trust X-Forwarded-For from configured trusted proxies"
```

---

### Task 5: Login Rate Limiting

**Files:**
- Modify: `internal/api/auth_handlers.go` — wrap login handler
- Modify: `internal/api/middleware.go` — add `LoginRateLimiter`
- Test: `internal/api/auth_handlers_test.go`

**Step 1: Create a per-IP login rate limiter**

In `internal/api/middleware.go`, add:

```go
// LoginRateLimitConfig configures per-IP login rate limiting.
type LoginRateLimitConfig struct {
    AttemptsPerMinute int
    ProxyConfig       *TrustedProxyConfig
}

// LoginRateLimitMiddleware wraps a handler with per-IP login rate limiting.
func LoginRateLimitMiddleware(cfg LoginRateLimitConfig) func(http.Handler) http.Handler {
    type ipEntry struct {
        limiter  *rate.Limiter
        lastSeen time.Time
    }
    var mu sync.Mutex
    clients := make(map[string]*ipEntry)

    // Cleanup stale entries every 5 minutes
    go func() {
        for {
            time.Sleep(5 * time.Minute)
            mu.Lock()
            for ip, entry := range clients {
                if time.Since(entry.lastSeen) > 10*time.Minute {
                    delete(clients, ip)
                }
            }
            mu.Unlock()
        }
    }()

    rps := rate.Limit(float64(cfg.AttemptsPerMinute) / 60.0)

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ip := clientKeyWithProxies(r, cfg.ProxyConfig)

            mu.Lock()
            entry, ok := clients[ip]
            if !ok {
                entry = &ipEntry{limiter: rate.NewLimiter(rps, cfg.AttemptsPerMinute)}
                clients[ip] = entry
            }
            entry.lastSeen = time.Now()
            mu.Unlock()

            if !entry.limiter.Allow() {
                w.Header().Set("Retry-After", "60")
                writeErr(w, http.StatusTooManyRequests, "too many login attempts", "try again later")
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

**Step 2: Wire login rate limiter to the login handler**

In `internal/api/auth_handlers.go`, where `handleLogin` is registered in `RegisterProtectedAuthRoutes`, wrap it:

```go
loginRL := LoginRateLimitMiddleware(LoginRateLimitConfig{
    AttemptsPerMinute: 5, // will later come from settings
    ProxyConfig:       proxyConfig,
})
mux.Handle("POST /api/v1/auth/login", loginRL(http.HandlerFunc(as.handleLogin)))
```

**Step 3: Write tests**

Test that 6 rapid login attempts from the same IP results in a 429 on the 6th.

**Step 4: Commit**

```bash
git add -A && git commit -m "feat: per-IP login rate limiting (5 attempts/min default)"
```

---

### Task 6: Remove Bearer-as-Session-Token (Strategy 3)

**Files:**
- Modify: `internal/api/middleware.go` — `DualAuthMiddleware` (lines 513-540)
- Test: `internal/api/middleware_test.go`

**Step 1: Write test confirming Strategy 3 currently works**

Test that `Authorization: Bearer <session-id>` (without `cpam_` prefix) authenticates via session. This test should pass now and fail after our change.

**Step 2: Remove Strategy 3 from `DualAuthMiddleware`**

In `DualAuthMiddleware`, delete the block starting around line 513 that handles Bearer tokens without the `cpam_` prefix as session lookups. After this change, if a Bearer token doesn't start with `cpam_`, return 401.

```go
// REMOVE this entire block:
// Strategy 3: Try Bearer token as session ID
// if sessionStore != nil { ... }

// REPLACE with:
// Unrecognized Bearer token format
if required {
    slogger.Warn("request failed", "status", 401, "error", "invalid bearer token format", "request_id", requestID)
    writeErr(w, http.StatusUnauthorized, "invalid bearer token", "bearer tokens must be API keys (cpam_ prefix)")
    return
}
```

**Step 3: Run tests, fix any that relied on Strategy 3, commit**

```bash
nix develop --command go test ./internal/api/... -v 2>&1
git add -A && git commit -m "security: remove undocumented bearer-as-session-token auth path"
```

---

### Task 7: Password Policy Hardening

**Files:**
- Modify: `internal/auth/password.go`
- Modify: `internal/api/system_handlers.go` — `handleSetup` password validation
- Modify: `internal/api/auth_handlers.go` — password change handlers
- Test: `internal/auth/password_test.go` (create if needed)

**Step 1: Add password validation function**

In `internal/auth/password.go`:

```go
const (
    DefaultMinPasswordLength = 12
    MaxPasswordLength        = 72 // bcrypt truncation boundary
)

// ValidatePassword checks password meets policy requirements.
func ValidatePassword(password string, minLength int) error {
    if minLength <= 0 {
        minLength = DefaultMinPasswordLength
    }
    if len(password) < minLength {
        return fmt.Errorf("password must be at least %d characters", minLength)
    }
    if len(password) > MaxPasswordLength {
        return fmt.Errorf("password must be at most %d characters (bcrypt limit)", MaxPasswordLength)
    }
    return nil
}
```

**Step 2: Write tests**

Test cases: too short, exactly min, max length, over max (73 chars), empty.

**Step 3: Update `handleSetup` to use `ValidatePassword`**

Replace `if len(req.Password) < 8` with `if err := auth.ValidatePassword(req.Password, 0); err != nil`.

**Step 4: Update `handleLogin`/password change handlers similarly**

**Step 5: Update `bootstrapAdmin` in `main.go` to validate password**

**Step 6: Update frontend `SetupPage.tsx` to show minimum 12 characters**

**Step 7: Run tests and commit**

```bash
nix develop --command go test ./internal/auth/... ./internal/api/... -v 2>&1
git add -A && git commit -m "feat: password policy hardening — min 12 chars, max 72 (bcrypt limit)"
```

---

### Task 8: Settings Table + Store Interface

**Files:**
- Create: `migrations/0016_settings.sql`
- Create: `internal/storage/settings.go` (interface)
- Create: `internal/storage/settings_memory.go` (in-memory implementation)
- Create: `internal/storage/sqlite/settings.go` (SQLite implementation)
- Create: `internal/domain/settings.go` (SecuritySettings type)

**Step 1: Define the SecuritySettings domain type**

In `internal/domain/settings.go`:

```go
package domain

// SecuritySettings holds runtime security configuration.
type SecuritySettings struct {
    SessionDurationHours   int      `json:"session_duration_hours"`
    MaxSessionsPerUser     int      `json:"max_sessions_per_user"`
    PasswordMinLength      int      `json:"password_min_length"`
    PasswordMaxLength      int      `json:"password_max_length"`
    LoginRateLimitPerMin   int      `json:"login_rate_limit_per_minute"`
    AccountLockoutAttempts int      `json:"account_lockout_attempts"`
    TrustedProxies         []string `json:"trusted_proxies"`
}

// DefaultSecuritySettings returns safe defaults.
func DefaultSecuritySettings() SecuritySettings {
    return SecuritySettings{
        SessionDurationHours:   24,
        MaxSessionsPerUser:     10,
        PasswordMinLength:      12,
        PasswordMaxLength:      72,
        LoginRateLimitPerMin:   5,
        AccountLockoutAttempts: 0,
        TrustedProxies:         []string{},
    }
}
```

**Step 2: Define the SettingsStore interface**

In `internal/storage/settings.go`:

```go
package storage

import (
    "context"
    "cloudpam/internal/domain"
)

// SettingsStore manages application settings.
type SettingsStore interface {
    GetSecuritySettings(ctx context.Context) (*domain.SecuritySettings, error)
    UpdateSecuritySettings(ctx context.Context, settings *domain.SecuritySettings) error
}
```

**Step 3: Create in-memory implementation**

In `internal/storage/settings_memory.go`:

```go
package storage

import (
    "context"
    "sync"
    "cloudpam/internal/domain"
)

type MemorySettingsStore struct {
    mu       sync.RWMutex
    security *domain.SecuritySettings
}

func NewMemorySettingsStore() *MemorySettingsStore {
    defaults := domain.DefaultSecuritySettings()
    return &MemorySettingsStore{security: &defaults}
}

func (s *MemorySettingsStore) GetSecuritySettings(_ context.Context) (*domain.SecuritySettings, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    copy := *s.security
    return &copy, nil
}

func (s *MemorySettingsStore) UpdateSecuritySettings(_ context.Context, settings *domain.SecuritySettings) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.security = settings
    return nil
}
```

**Step 4: Create SQLite migration**

`migrations/0016_settings.sql`:

```sql
-- Application settings (key-value with JSON values)
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
```

**Step 5: Create SQLite implementation**

In `internal/storage/sqlite/settings.go` (with `//go:build sqlite` tag):

```go
func (s *Store) GetSecuritySettings(ctx context.Context) (*domain.SecuritySettings, error) {
    var raw string
    err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'security'`).Scan(&raw)
    if err == sql.ErrNoRows {
        defaults := domain.DefaultSecuritySettings()
        return &defaults, nil
    }
    if err != nil {
        return nil, err
    }
    var settings domain.SecuritySettings
    if err := json.Unmarshal([]byte(raw), &settings); err != nil {
        return nil, err
    }
    return &settings, nil
}

func (s *Store) UpdateSecuritySettings(ctx context.Context, settings *domain.SecuritySettings) error {
    raw, err := json.Marshal(settings)
    if err != nil {
        return err
    }
    _, err = s.db.ExecContext(ctx,
        `INSERT INTO settings (key, value, updated_at) VALUES ('security', ?, datetime('now'))
         ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
        string(raw))
    return err
}
```

**Step 6: Run tests and commit**

```bash
nix develop --command go test ./internal/storage/... ./internal/domain/... -v 2>&1
git add -A && git commit -m "feat: settings table + SettingsStore interface with memory and SQLite implementations"
```

---

### Task 9: Security Settings API Endpoint

**Files:**
- Create: `internal/api/settings_handlers.go`
- Modify: `internal/api/server.go` — add SettingsServer and route registration
- Test: `internal/api/settings_handlers_test.go`

**Step 1: Create SettingsServer**

In `internal/api/settings_handlers.go`:

```go
package api

import (
    "encoding/json"
    "net/http"

    "cloudpam/internal/auth"
    "cloudpam/internal/domain"
    "cloudpam/internal/storage"
)

type SettingsServer struct {
    *Server
    settingsStore storage.SettingsStore
}

func NewSettingsServer(srv *Server, store storage.SettingsStore) *SettingsServer {
    return &SettingsServer{Server: srv, settingsStore: store}
}

func (ss *SettingsServer) RegisterProtectedSettingsRoutes(dualMW func(http.Handler) http.Handler, slogger interface{ /* slog.Logger */ }) {
    mux := ss.mux
    adminOnly := RequirePermissionMiddleware("settings", "write", slogger)
    adminRead := RequirePermissionMiddleware("settings", "read", slogger)

    mux.Handle("GET /api/v1/settings/security",
        dualMW(adminRead(http.HandlerFunc(ss.handleGetSecuritySettings))))
    mux.Handle("PATCH /api/v1/settings/security",
        dualMW(adminOnly(http.HandlerFunc(ss.handleUpdateSecuritySettings))))
}

func (ss *SettingsServer) handleGetSecuritySettings(w http.ResponseWriter, r *http.Request) {
    settings, err := ss.settingsStore.GetSecuritySettings(r.Context())
    if err != nil {
        writeErr(w, http.StatusInternalServerError, "failed to load settings", err.Error())
        return
    }
    writeJSON(w, http.StatusOK, settings)
}

func (ss *SettingsServer) handleUpdateSecuritySettings(w http.ResponseWriter, r *http.Request) {
    var input domain.SecuritySettings
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
        writeErr(w, http.StatusBadRequest, "invalid request body", err.Error())
        return
    }

    // Validate bounds
    if input.SessionDurationHours < 1 || input.SessionDurationHours > 720 {
        writeErr(w, http.StatusBadRequest, "invalid session_duration_hours", "must be between 1 and 720")
        return
    }
    if input.MaxSessionsPerUser < 1 || input.MaxSessionsPerUser > 100 {
        writeErr(w, http.StatusBadRequest, "invalid max_sessions_per_user", "must be between 1 and 100")
        return
    }
    if input.PasswordMinLength < 8 || input.PasswordMinLength > 72 {
        writeErr(w, http.StatusBadRequest, "invalid password_min_length", "must be between 8 and 72")
        return
    }
    if input.PasswordMaxLength < input.PasswordMinLength || input.PasswordMaxLength > 72 {
        writeErr(w, http.StatusBadRequest, "invalid password_max_length", "must be between min_length and 72")
        return
    }
    if input.LoginRateLimitPerMin < 1 || input.LoginRateLimitPerMin > 60 {
        writeErr(w, http.StatusBadRequest, "invalid login_rate_limit_per_minute", "must be between 1 and 60")
        return
    }

    if err := ss.settingsStore.UpdateSecuritySettings(r.Context(), &input); err != nil {
        writeErr(w, http.StatusInternalServerError, "failed to save settings", err.Error())
        return
    }

    ss.logAudit(r.Context(), "update", "settings", "security", "security_settings", http.StatusOK)
    writeJSON(w, http.StatusOK, input)
}
```

**Step 2: Add `ResourceSettings` to RBAC**

In `internal/auth/rbac.go`, add:

```go
ResourceSettings = "settings"
```

And grant admin role `settings:read` and `settings:write` in the permission cache initialization.

**Step 3: Wire SettingsServer in `main.go`**

After creating the settings store, create and register the settings server:

```go
settingsStore := storage.NewMemorySettingsStore() // or sqlite variant
settingsSrv := api.NewSettingsServer(srv, settingsStore)
settingsSrv.RegisterProtectedSettingsRoutes(dualMW, logger.Slog())
```

**Step 4: Write tests and commit**

```bash
nix develop --command go test ./internal/api/... -run TestSettings -v 2>&1
git add -A && git commit -m "feat: security settings API endpoint (GET/PATCH /api/v1/settings/security)"
```

---

### Task 10: Session Hardening — Configurable Duration + Max Sessions

**Files:**
- Modify: `internal/auth/session.go` — `SessionStore` interface, `NewSession` function
- Modify: `internal/api/auth_handlers.go` — `handleLogin` to use settings for session duration
- Test: `internal/auth/session_test.go`

**Step 1: Add `ListByUserID` to `SessionStore` interface**

```go
type SessionStore interface {
    Create(ctx context.Context, session *Session) error
    Get(ctx context.Context, id string) (*Session, error)
    Delete(ctx context.Context, id string) error
    DeleteByUserID(ctx context.Context, userID string) error
    ListByUserID(ctx context.Context, userID string) ([]*Session, error) // NEW
    Cleanup(ctx context.Context) (int, error)
}
```

Implement `ListByUserID` in `MemorySessionStore`, SQLite session store, and PostgreSQL session store.

**Step 2: Add session limit enforcement in `handleLogin`**

After creating a session successfully, check if the user has exceeded `MaxSessionsPerUser`. If so, delete the oldest sessions:

```go
// After session creation in handleLogin:
sessions, _ := as.sessionStore.ListByUserID(r.Context(), user.ID)
settings, _ := as.settingsStore.GetSecuritySettings(r.Context())
if len(sessions) > settings.MaxSessionsPerUser {
    // Sort by CreatedAt, delete oldest until within limit
    sort.Slice(sessions, func(i, j int) bool {
        return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
    })
    excess := len(sessions) - settings.MaxSessionsPerUser
    for i := 0; i < excess; i++ {
        _ = as.sessionStore.Delete(r.Context(), sessions[i].ID)
    }
}
```

**Step 3: Use settings for session duration in `handleLogin`**

Replace the hardcoded `auth.DefaultSessionDuration` with the value from `settingsStore.GetSecuritySettings()`.

**Step 4: Write tests and commit**

```bash
nix develop --command go test ./internal/auth/... ./internal/api/... -v 2>&1
git add -A && git commit -m "feat: configurable session duration and max concurrent sessions per user"
```

---

### Task 11: Revoke All Sessions API

**Files:**
- Modify: `internal/api/user_handlers.go` — add `handleRevokeSessions`
- Test: `internal/api/user_handlers_test.go` (or integration_test.go)

**Step 1: Add endpoint**

In `user_handlers.go`:

```go
func (us *UserServer) handleRevokeSessions(w http.ResponseWriter, r *http.Request) {
    userID := r.PathValue("id")
    if userID == "" {
        writeErr(w, http.StatusBadRequest, "missing user id", "")
        return
    }

    // Check authorization: admin or self
    callerRole := auth.GetEffectiveRole(r.Context())
    caller := auth.UserFromContext(r.Context())
    if callerRole != auth.RoleAdmin && (caller == nil || caller.ID != userID) {
        writeErr(w, http.StatusForbidden, "forbidden", "only admins or the user themselves can revoke sessions")
        return
    }

    if err := us.sessionStore.DeleteByUserID(r.Context(), userID); err != nil {
        writeErr(w, http.StatusInternalServerError, "failed to revoke sessions", err.Error())
        return
    }

    us.logAuditEvent(r, "revoke_sessions", "user", userID, "", http.StatusOK)
    writeJSON(w, http.StatusOK, map[string]string{"status": "sessions revoked"})
}
```

**Step 2: Register route in `RegisterProtectedUserRoutes`**

```go
mux.Handle("POST /api/v1/auth/users/{id}/revoke-sessions", dualMW(http.HandlerFunc(us.handleRevokeSessions)))
```

**Step 3: Write tests and commit**

```bash
nix develop --command go test ./internal/api/... -run TestRevokeSessions -v 2>&1
git add -A && git commit -m "feat: revoke all sessions API endpoint for user management"
```

---

### Task 12: CSRF Protection Middleware

**Files:**
- Create: `internal/api/csrf.go`
- Modify: `cmd/cloudpam/main.go` — add CSRF middleware to chain
- Modify: `ui/src/api/client.ts` — send CSRF token header
- Test: `internal/api/csrf_test.go`

**Step 1: Implement CSRF middleware**

In `internal/api/csrf.go`:

```go
package api

import (
    "crypto/rand"
    "encoding/hex"
    "net/http"

    "cloudpam/internal/auth"
)

const csrfTokenLength = 32
const csrfHeaderName = "X-CSRF-Token"
const csrfCookieName = "csrf_token"

// CSRFMiddleware adds CSRF protection for session-authenticated state-changing requests.
// API key authenticated requests are exempt (no cookies = no CSRF risk).
func CSRFMiddleware() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Skip for safe methods
            if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
                // Set CSRF token cookie if not present
                if _, err := r.Cookie(csrfCookieName); err != nil {
                    token := generateCSRFToken()
                    http.SetCookie(w, &http.Cookie{
                        Name:     csrfCookieName,
                        Value:    token,
                        Path:     "/",
                        HttpOnly: false, // JS needs to read it
                        Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
                        SameSite: http.SameSiteLaxMode,
                    })
                }
                next.ServeHTTP(w, r)
                return
            }

            // For state-changing methods: skip CSRF check if API key authenticated
            if key := auth.APIKeyFromContext(r.Context()); key != nil {
                next.ServeHTTP(w, r)
                return
            }

            // Skip CSRF for setup and login endpoints (no session yet)
            if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/setup" {
                next.ServeHTTP(w, r)
                return
            }

            // Validate CSRF token from header matches cookie
            cookie, err := r.Cookie(csrfCookieName)
            if err != nil {
                writeErr(w, http.StatusForbidden, "CSRF token missing", "csrf_token cookie required")
                return
            }
            headerToken := r.Header.Get(csrfHeaderName)
            if headerToken == "" || headerToken != cookie.Value {
                writeErr(w, http.StatusForbidden, "CSRF token invalid", "X-CSRF-Token header must match csrf_token cookie")
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}

func generateCSRFToken() string {
    b := make([]byte, csrfTokenLength)
    _, _ = rand.Read(b)
    return hex.EncodeToString(b)
}
```

**Step 2: Add CSRF middleware to the handler chain in `main.go`**

Insert it in the middleware stack after the auth middleware but before route handlers.

**Step 3: Update the frontend API client**

In `ui/src/api/client.ts`, read the `csrf_token` cookie and include it as `X-CSRF-Token` header on POST/PATCH/DELETE requests:

```typescript
function getCSRFToken(): string | null {
  const match = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]+)/)
  return match ? match[1] : null
}

// In the fetch wrapper, for non-GET requests:
const csrfToken = getCSRFToken()
if (csrfToken && method !== 'GET') {
  headers['X-CSRF-Token'] = csrfToken
}
```

**Step 4: Write tests and commit**

Test: state-changing request without CSRF token returns 403. With matching token returns 200. API key requests bypass CSRF.

```bash
nix develop --command go test ./internal/api/... -run TestCSRF -v 2>&1
nix develop --command bash -c 'cd ui && npx tsc --noEmit'
git add -A && git commit -m "feat: CSRF protection middleware for session-authenticated requests"
```

---

### Task 13: Security Settings UI Page

**Files:**
- Create: `ui/src/pages/SecuritySettingsPage.tsx`
- Create: `ui/src/hooks/useSettings.ts`
- Modify: `ui/src/App.tsx` — add route
- Modify: `ui/src/components/Sidebar.tsx` — add nav link

**Step 1: Create the settings API hook**

In `ui/src/hooks/useSettings.ts`:

```typescript
import { useState, useEffect, useCallback } from 'react'
import { apiClient } from '../api/client'

export interface SecuritySettings {
  session_duration_hours: number
  max_sessions_per_user: number
  password_min_length: number
  password_max_length: number
  login_rate_limit_per_minute: number
  account_lockout_attempts: number
  trusted_proxies: string[]
}

export function useSecuritySettings() {
  const [settings, setSettings] = useState<SecuritySettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchSettings = useCallback(async () => {
    try {
      setLoading(true)
      const resp = await apiClient.get('/api/v1/settings/security')
      setSettings(resp)
      setError(null)
    } catch (err: any) {
      setError(err.message || 'Failed to load security settings')
    } finally {
      setLoading(false)
    }
  }, [])

  const updateSettings = useCallback(async (updated: SecuritySettings) => {
    const resp = await apiClient.patch('/api/v1/settings/security', updated)
    setSettings(resp)
    return resp
  }, [])

  useEffect(() => { fetchSettings() }, [fetchSettings])

  return { settings, loading, error, updateSettings, refetch: fetchSettings }
}
```

**Step 2: Create the SecuritySettingsPage component**

In `ui/src/pages/SecuritySettingsPage.tsx`, create a form-based page with sections for Session Management, Password Policy, Login Protection, and Network. Use the same Tailwind patterns as the existing config pages (`ApiKeysPage.tsx`, `UsersPage.tsx`). Include Save button, loading states, and toast notifications via `useToast`.

The page should include:
- Session duration (number input, hours)
- Max sessions per user (number input)
- Password min length (number input)
- Password max length (number input, max 72)
- Login rate limit (number input, attempts per minute)
- Account lockout attempts (number input, 0 = disabled)
- Trusted proxies (textarea, one CIDR per line)

Include grayed-out sections for "Roles & Permissions (coming soon)" and "SSO/OIDC (coming soon)".

**Step 3: Add route and sidebar link**

In `ui/src/App.tsx`, add: `<Route path="/config/security" element={<SecuritySettingsPage />} />`

In `ui/src/components/Sidebar.tsx`, add a "Security" link under the Config section with a `Shield` icon from lucide-react.

**Step 4: Type check and commit**

```bash
nix develop --command bash -c 'cd ui && npx tsc --noEmit'
nix develop --command bash -c 'cd ui && npm run build'
git add -A && git commit -m "feat: security settings UI page under Config > Security"
```

---

### Task 14: Update Documentation and Version Bump

**Files:**
- Modify: `docs/CHANGELOG.md` — add v0.7.0 entry
- Modify: `CLAUDE.md` — update env vars section, remove CLOUDPAM_AUTH_ENABLED references
- Modify: `docs/openapi.yaml` — add settings endpoints

**Step 1: Update CHANGELOG**

Add v0.7.0 entry with:
- **BREAKING:** `CLOUDPAM_AUTH_ENABLED` removed — auth is always on
- **Added:** Security settings API + UI
- **Added:** CSRF protection middleware
- **Added:** Login rate limiting
- **Added:** Trusted proxy configuration
- **Added:** Configurable session duration and max concurrent sessions
- **Added:** Revoke all sessions API
- **Added:** Password policy hardening (min 12, max 72)
- **Fixed:** API key scope elevation prevention
- **Fixed:** Missing import routes in protected mode
- **Fixed:** Audit actor attribution from auth context
- **Removed:** Bearer-as-session-token auth path

**Step 2: Update CLAUDE.md**

- Remove `CLOUDPAM_AUTH_ENABLED` from Environment Variables section
- Add `CLOUDPAM_TRUSTED_PROXIES` to Environment Variables
- Add `/api/v1/settings/security` to API endpoints list
- Add `/api/v1/auth/users/{id}/revoke-sessions` to API endpoints list
- Update the "authentication disabled" hint

**Step 3: Update OpenAPI spec**

Add the settings endpoint schemas to `docs/openapi.yaml`.

**Step 4: Final test run**

```bash
nix develop --command go test ./... 2>&1
nix develop --command golangci-lint run ./... 2>&1
nix develop --command go build ./cmd/cloudpam 2>&1
nix develop --command go build -tags sqlite ./cmd/cloudpam 2>&1
nix develop --command bash -c 'cd ui && npm run build'
```

**Step 5: Commit**

```bash
git add -A && git commit -m "docs: v0.7.0 changelog, updated CLAUDE.md, OpenAPI spec for auth hardening"
```

---

### Task 15: Final Verification

**Step 1: Full build and test**

```bash
nix develop --command go test -race ./... 2>&1
nix develop --command golangci-lint run ./... 2>&1
nix develop --command go build ./cmd/cloudpam 2>&1
nix develop --command go build -tags sqlite ./cmd/cloudpam 2>&1
nix develop --command bash -c 'cd ui && npx tsc --noEmit && npm run build'
```

**Step 2: Manual smoke test**

```bash
# Start fresh (delete any existing db)
rm -f cloudpam.db
nix develop --command bash -c 'SQLITE_DSN="file:cloudpam.db?cache=shared&_fk=1" go run -tags sqlite ./cmd/cloudpam'
```

Verify:
- `/healthz` returns `needs_setup: true`
- Navigating to `http://localhost:8080` redirects to setup wizard
- After creating admin, redirects to login
- After login, all routes work
- `/api/v1/settings/security` returns default settings
- Security settings page loads in UI

**Step 3: Create PR**

```bash
git push -u origin <branch-name>
gh pr create --title "feat: auth hardening sprint 19 (v0.7.0)" --body "..."
```
