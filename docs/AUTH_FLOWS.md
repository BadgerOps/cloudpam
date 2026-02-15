# CloudPAM Authentication & Authorization

This document details the authentication and authorization architecture for CloudPAM.

## Overview

CloudPAM supports two authentication methods:
1. **OAuth 2.0 / OIDC** - For user sessions via web UI
2. **API Keys** - For programmatic access and integrations

Authorization uses Role-Based Access Control (RBAC) with future support for Attribute-Based Access Control (ABAC).

## Authentication Flows

### 1. OAuth 2.0 / OIDC Flow (User Sessions)

CloudPAM acts as an OIDC Relying Party, supporting any OIDC-compliant Identity Provider.

```
┌─────────┐      ┌──────────────┐      ┌─────────────────┐      ┌─────────┐
│  User   │      │  CloudPAM    │      │  Identity       │      │ CloudPAM│
│ Browser │      │  Frontend    │      │  Provider       │      │   API   │
└────┬────┘      └──────┬───────┘      └────────┬────────┘      └────┬────┘
     │                  │                       │                     │
     │  1. Click Login  │                       │                     │
     │─────────────────>│                       │                     │
     │                  │                       │                     │
     │  2. Redirect to IdP                      │                     │
     │<─────────────────│                       │                     │
     │                  │                       │                     │
     │  3. Authenticate with IdP               │                     │
     │─────────────────────────────────────────>│                     │
     │                  │                       │                     │
     │  4. IdP redirects with auth code         │                     │
     │<────────────────────────────────────────│                     │
     │                  │                       │                     │
     │  5. Send code to backend                │                     │
     │─────────────────────────────────────────────────────────────>│
     │                  │                       │                     │
     │                  │      6. Exchange code for tokens           │
     │                  │                       │<────────────────────│
     │                  │                       │                     │
     │                  │      7. Return tokens │                     │
     │                  │                       │────────────────────>│
     │                  │                       │                     │
     │  8. Set session cookie + return user     │                     │
     │<────────────────────────────────────────────────────────────│
     │                  │                       │                     │
```

#### Token Flow Details

1. **Authorization Request**: Frontend redirects to IdP with:
   - `client_id`: CloudPAM's registered client ID
   - `redirect_uri`: Callback URL (e.g., `https://cloudpam.example.com/auth/callback`)
   - `response_type`: `code`
   - `scope`: `openid profile email`
   - `state`: CSRF protection token
   - `nonce`: Replay attack protection

2. **Token Exchange**: Backend exchanges auth code for tokens:
   - `access_token`: Short-lived (1 hour), used for API calls
   - `id_token`: Contains user identity claims
   - `refresh_token`: Long-lived (7 days), used to refresh access tokens

3. **Session Management**:
   - HTTP-only secure cookie stores encrypted session ID
   - Session maps to user record and cached permissions
   - Access token refresh happens automatically before expiration

#### API Endpoints

```
POST /api/v1/auth/login
  - Initiates OAuth flow, returns redirect URL

GET /api/v1/auth/callback?code=...&state=...
  - Handles OAuth callback, exchanges code for tokens
  - Creates/updates user record
  - Returns session cookie

POST /api/v1/auth/refresh
  - Refreshes access token using refresh token
  - Called automatically by frontend

POST /api/v1/auth/logout
  - Invalidates session
  - Optionally triggers IdP logout (if supported)

GET /api/v1/auth/userinfo
  - Returns current user info from session
```

#### IdP Configuration

CloudPAM supports these Identity Providers:

| Provider | Configuration |
|----------|---------------|
| Okta | Standard OIDC discovery |
| Azure AD | Standard OIDC + tenant-specific endpoints |
| Google Workspace | Standard OIDC |
| Auth0 | Standard OIDC discovery |
| Keycloak | Standard OIDC discovery |
| Generic OIDC | Manual endpoint configuration |

**Required IdP Claims:**
- `sub` - Unique user identifier
- `email` - User email (required)
- `name` - Display name (optional)
- `picture` - Avatar URL (optional)
- `groups` - Group membership for role mapping (optional)

### 2. API Key Authentication

For programmatic access without user context (CI/CD, scripts, integrations).

```
┌──────────────┐                              ┌─────────────────┐
│   Client     │                              │   CloudPAM API  │
│ (Script/CI)  │                              │                 │
└──────┬───────┘                              └────────┬────────┘
       │                                               │
       │  Request with X-API-Key header               │
       │──────────────────────────────────────────────>│
       │                                               │
       │                        Validate key, check scopes
       │                                               │
       │  Response (or 401/403)                        │
       │<──────────────────────────────────────────────│
       │                                               │
```

#### API Key Format

```
cpam_v1_<prefix>_<secret>

Example: cpam_v1_abc12345_x7k9mN2pQr5tVw8yZa4bCd6eF
         │    │  │        │
         │    │  │        └─ Secret portion (never logged)
         │    │  └─ Prefix for identification (logged, displayed)
         │    └─ Version identifier
         └─ CloudPAM identifier
```

#### Key Storage

- API keys are hashed (Argon2id) before storage
- Only the prefix is stored in plaintext for identification
- Full key is shown exactly once at creation time

#### Key Scopes

API keys have granular scopes limiting their access:

| Scope | Description |
|-------|-------------|
| `pools:read` | Read pool information |
| `pools:write` | Create, update, delete pools |
| `accounts:read` | Read cloud account info |
| `accounts:write` | Manage cloud accounts |
| `discovery:read` | Read discovered resources |
| `discovery:sync` | Trigger discovery syncs |
| `schema:read` | Read schema plans/templates |
| `schema:write` | Create and apply schema plans |
| `audit:read` | Read audit logs |
| `users:read` | Read user information (admin) |
| `users:write` | Manage users (admin) |
| `org:read` | Read organization settings |
| `org:write` | Manage organization (admin) |

#### Rate Limiting

API keys have configurable rate limits:

```yaml
# Per-key configuration
rate_limit:
  requests_per_minute: 100   # Default
  requests_per_hour: 1000
  burst: 20                   # Max concurrent requests
```

## Authorization (RBAC)

### Built-in Roles

| Role | Description | Key Permissions |
|------|-------------|-----------------|
| **Admin** | Full access | All permissions |
| **Editor** | Manage resources | pools:*, accounts:*, schema:*, discovery:* |
| **Viewer** | Read-only access | *:read only |

### Permission Structure

Permissions follow the pattern: `resource:action`

```
pools:read          - View pools
pools:write         - Create/update pools
pools:delete        - Delete pools
pools:allocate      - Allocate from pools

accounts:read       - View cloud accounts
accounts:write      - Create/update accounts
accounts:delete     - Remove accounts
accounts:sync       - Trigger syncs

schema:read         - View plans/templates
schema:write        - Create/update plans
schema:apply        - Apply schema plans

discovery:read      - View discovered resources
discovery:link      - Link resources to pools
discovery:sync      - Trigger discovery

audit:read          - View audit logs
audit:export        - Export audit data

users:read          - View users
users:invite        - Invite new users
users:write         - Update users
users:delete        - Remove users

roles:read          - View roles
roles:write         - Manage custom roles

org:read            - View org settings
org:write           - Update org settings

teams:read          - View teams
teams:write         - Manage teams
```

### Custom Roles

Admins can create custom roles with specific permission sets:

```json
{
  "name": "Network Engineer",
  "description": "Can manage pools and view discovery",
  "permissions": [
    "pools:read",
    "pools:write",
    "pools:allocate",
    "discovery:read",
    "accounts:read",
    "schema:read"
  ]
}
```

### Team-Based Access (Pool Scoping)

Teams can have access scoped to specific pools:

```json
{
  "team": "us-east-team",
  "pool_access": [
    {
      "pool_id": "pool-us-east-prod",
      "access_level": "admin",
      "include_children": true
    },
    {
      "pool_id": "pool-us-east-staging",
      "access_level": "edit",
      "include_children": true
    }
  ]
}
```

Access levels:
- **view**: Read-only access to pool and children
- **edit**: Can allocate and modify pool properties
- **admin**: Full control including delete

### Permission Resolution

1. Check user's global role permissions
2. If denied, check team-based pool access
3. Apply most permissive access found

```go
func (a *Authorizer) CanAccess(user *User, resource Resource, action string) bool {
    // 1. Global role check
    if user.Role.HasPermission(resource.Type + ":" + action) {
        return true
    }

    // 2. Team-based pool access check
    if resource.Type == "pool" {
        for _, team := range user.Teams {
            if access := team.GetPoolAccess(resource.ID); access != nil {
                if access.AllowsAction(action) {
                    return true
                }
            }
        }
    }

    return false
}
```

## Session Security

### Cookie Configuration

```go
http.Cookie{
    Name:     "cloudpam_session",
    Value:    encryptedSessionID,
    Path:     "/",
    Domain:   ".cloudpam.example.com",
    MaxAge:   86400 * 7,  // 7 days
    Secure:   true,       // HTTPS only
    HttpOnly: true,       // No JS access
    SameSite: http.SameSiteLaxMode,
}
```

### Session Storage

```sql
CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    access_token_encrypted BYTEA NOT NULL,
    refresh_token_encrypted BYTEA NOT NULL,
    access_token_expires_at TIMESTAMPTZ NOT NULL,
    refresh_token_expires_at TIMESTAMPTZ NOT NULL,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    last_activity_at TIMESTAMPTZ DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(refresh_token_expires_at)
    WHERE revoked_at IS NULL;
```

### Token Encryption

Tokens are encrypted at rest using AES-256-GCM:

```go
func (s *SessionStore) EncryptToken(token string) ([]byte, error) {
    block, _ := aes.NewCipher(s.encryptionKey)
    gcm, _ := cipher.NewGCM(block)

    nonce := make([]byte, gcm.NonceSize())
    io.ReadFull(rand.Reader, nonce)

    return gcm.Seal(nonce, nonce, []byte(token), nil), nil
}
```

## User Provisioning

### Just-in-Time (JIT) Provisioning

When SSO is enabled with auto-provisioning:

1. User authenticates with IdP
2. CloudPAM receives ID token with claims
3. If user doesn't exist:
   - Create user record from claims
   - Assign default role
   - Map IdP groups to roles (if configured)
4. If user exists:
   - Update profile from claims
   - Re-evaluate role mapping

### Role Mapping from IdP Groups

```json
{
  "role_mapping": [
    {
      "claim": "groups",
      "value": "cloudpam-admins",
      "role_id": "role-admin"
    },
    {
      "claim": "groups",
      "value": "network-engineers",
      "role_id": "role-editor"
    },
    {
      "claim": "department",
      "value": "IT",
      "role_id": "role-viewer"
    }
  ],
  "default_role_id": "role-viewer"
}
```

## Security Headers

All API responses include:

```
Strict-Transport-Security: max-age=31536000; includeSubDomains
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'
Referrer-Policy: strict-origin-when-cross-origin
```

## Audit Logging

All authentication events are logged:

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "event_type": "auth.login",
  "actor": {
    "id": "user-123",
    "email": "user@example.com",
    "ip_address": "192.168.1.100",
    "user_agent": "Mozilla/5.0..."
  },
  "details": {
    "method": "oidc",
    "provider": "okta",
    "session_id": "sess-abc123"
  },
  "result": "success"
}
```

Logged events:
- `auth.login` - Successful login
- `auth.login_failed` - Failed login attempt
- `auth.logout` - User logout
- `auth.session_expired` - Session expiration
- `auth.token_refresh` - Token refresh
- `auth.api_key_used` - API key authentication
- `auth.api_key_created` - New API key created
- `auth.api_key_revoked` - API key revoked
- `auth.permission_denied` - Authorization failure

## Implementation Notes

### Go Middleware Chain

```go
func AuthMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 1. Try API key auth
        if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
            token, err := validateAPIKey(apiKey)
            if err != nil {
                respondUnauthorized(w, "Invalid API key")
                return
            }
            ctx := context.WithValue(r.Context(), "auth", &AuthContext{
                Type:   "api_key",
                Token:  token,
                Scopes: token.Scopes,
            })
            next.ServeHTTP(w, r.WithContext(ctx))
            return
        }

        // 2. Try session cookie auth
        session, err := getSessionFromCookie(r)
        if err != nil {
            respondUnauthorized(w, "Authentication required")
            return
        }

        // 3. Check if access token needs refresh
        if session.AccessTokenExpiresSoon() {
            if err := refreshAccessToken(session); err != nil {
                respondUnauthorized(w, "Session expired")
                return
            }
        }

        ctx := context.WithValue(r.Context(), "auth", &AuthContext{
            Type:    "session",
            User:    session.User,
            Session: session,
        })
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func RequirePermission(permission string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            auth := r.Context().Value("auth").(*AuthContext)

            if !auth.HasPermission(permission) {
                auditLog.Log("auth.permission_denied", auth, permission)
                respondForbidden(w, "Insufficient permissions")
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}
```

### Route Protection Example

```go
r := chi.NewRouter()

// Public routes
r.Get("/health", healthHandler)
r.Get("/ready", readyHandler)

// Auth routes
r.Route("/auth", func(r chi.Router) {
    r.Post("/login", loginHandler)
    r.Get("/callback", callbackHandler)
    r.With(AuthMiddleware).Post("/logout", logoutHandler)
    r.With(AuthMiddleware).Post("/refresh", refreshHandler)
})

// Protected API routes
r.Route("/api/v1", func(r chi.Router) {
    r.Use(AuthMiddleware)

    r.Route("/pools", func(r chi.Router) {
        r.With(RequirePermission("pools:read")).Get("/", listPools)
        r.With(RequirePermission("pools:write")).Post("/", createPool)
        r.With(RequirePermission("pools:read")).Get("/{id}", getPool)
        r.With(RequirePermission("pools:write")).Patch("/{id}", updatePool)
        r.With(RequirePermission("pools:delete")).Delete("/{id}", deletePool)
    })

    // Admin-only routes
    r.Route("/users", func(r chi.Router) {
        r.Use(RequirePermission("users:read"))
        r.Get("/", listUsers)
        r.With(RequirePermission("users:invite")).Post("/", inviteUser)
        // ...
    })
})
```

## Future: ABAC Support

Planned attribute-based access control will enable policies like:

```yaml
policies:
  - name: "region-restricted-access"
    effect: allow
    resources:
      - type: pool
        conditions:
          - attribute: tags.region
            operator: in
            value: "${user.allowed_regions}"
    actions: ["read", "allocate"]

  - name: "environment-write-restriction"
    effect: deny
    resources:
      - type: pool
        conditions:
          - attribute: tags.environment
            operator: equals
            value: "production"
    actions: ["write", "delete"]
    subjects:
      conditions:
        - attribute: role
          operator: not_in
          value: ["admin", "senior-engineer"]
```

This will be implemented as a policy evaluation engine that runs after RBAC checks.
