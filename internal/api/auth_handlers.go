package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// AuthServer extends Server with API key management and audit functionality.
type AuthServer struct {
	*Server
	keyStore      auth.KeyStore
	sessionStore  auth.SessionStore
	userStore     auth.UserStore
	auditLogger   audit.AuditLogger
	settingsStore storage.SettingsStore
}

// NewAuthServer creates a new AuthServer with auth and audit capabilities.
func NewAuthServer(s *Server, keyStore auth.KeyStore, auditLogger audit.AuditLogger) *AuthServer {
	return &AuthServer{
		Server:      s,
		keyStore:    keyStore,
		auditLogger: auditLogger,
	}
}

// NewAuthServerWithStores creates a new AuthServer with full store access.
func NewAuthServerWithStores(s *Server, keyStore auth.KeyStore, sessionStore auth.SessionStore, userStore auth.UserStore, auditLogger audit.AuditLogger) *AuthServer {
	return &AuthServer{
		Server:       s,
		keyStore:     keyStore,
		sessionStore: sessionStore,
		userStore:    userStore,
		auditLogger:  auditLogger,
	}
}

// SetSettingsStore attaches runtime security policy settings to the auth server.
func (as *AuthServer) SetSettingsStore(store storage.SettingsStore) {
	as.settingsStore = store
}

// RegisterAuthRoutes registers the auth API endpoints without RBAC.
// For backward compatibility. Use RegisterProtectedAuthRoutes for RBAC enforcement.
// Note: Audit endpoint is registered by Server.RegisterRoutes() for unprotected access.
func (as *AuthServer) RegisterAuthRoutes() {
	// API key management
	as.mux.HandleFunc("/api/v1/auth/keys", as.handleAPIKeys)
	as.mux.HandleFunc("/api/v1/auth/keys/", as.handleAPIKeyByID)
}

// RegisterProtectedAuthRoutes registers the auth and audit API endpoints with RBAC.
// Routes require authentication and appropriate permissions:
// - /api/v1/auth/keys: requires apikeys:* permissions
// - /api/v1/audit: requires audit:read permission
func (as *AuthServer) RegisterProtectedAuthRoutes(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	// Use dual auth middleware when session/user stores are available,
	// otherwise fall back to API key only.
	var authMW Middleware
	if as.sessionStore != nil && as.userStore != nil {
		authMW = DualAuthMiddleware(as.keyStore, as.sessionStore, as.userStore, true, logger)
	} else {
		authMW = AuthMiddleware(as.keyStore, true, logger)
	}

	// API key management - requires apikeys permissions
	as.mux.Handle("/api/v1/auth/keys", authMW(as.protectedAPIKeysHandler(logger)))
	as.mux.Handle("/api/v1/auth/keys/", authMW(as.protectedAPIKeyByIDHandler(logger)))

	// Audit endpoints - requires audit:read permission
	auditReadMW := RequirePermissionMiddleware(auth.ResourceAudit, auth.ActionRead, logger)
	as.mux.Handle("/api/v1/audit", authMW(auditReadMW(http.HandlerFunc(as.handleAuditList))))
}

// protectedAPIKeysHandler returns a handler for /api/v1/auth/keys with RBAC.
func (as *AuthServer) protectedAPIKeysHandler(logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		role := auth.GetEffectiveRole(ctx)

		switch r.Method {
		case http.MethodPost:
			// Create API key requires apikeys:create
			if !auth.HasPermission(role, auth.ResourceAPIKeys, auth.ActionCreate) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			as.createAPIKey(w, r)
		case http.MethodGet:
			// List API keys requires apikeys:list or apikeys:read
			if !auth.HasPermission(role, auth.ResourceAPIKeys, auth.ActionList) &&
				!auth.HasPermission(role, auth.ResourceAPIKeys, auth.ActionRead) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			as.listAPIKeys(w, r)
		default:
			w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
			as.writeErr(ctx, w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})
}

// protectedAPIKeyByIDHandler returns a handler for /api/v1/auth/keys/{id} with RBAC.
func (as *AuthServer) protectedAPIKeyByIDHandler(logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		role := auth.GetEffectiveRole(ctx)

		path := strings.TrimPrefix(r.URL.Path, "/api/v1/auth/keys/")
		id := strings.Trim(path, "/")

		if id == "" {
			as.writeErr(ctx, w, http.StatusNotFound, "not found", "")
			return
		}

		switch r.Method {
		case http.MethodDelete:
			// Revoke API key requires apikeys:delete
			if !auth.HasPermission(role, auth.ResourceAPIKeys, auth.ActionDelete) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			as.revokeAPIKey(w, r, id)
		default:
			w.Header().Set("Allow", http.MethodDelete)
			as.writeErr(ctx, w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})
}

// handleAPIKeys handles POST /api/v1/auth/keys (create) and GET /api/v1/auth/keys (list).
func (as *AuthServer) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		as.createAPIKey(w, r)
	case http.MethodGet:
		as.listAPIKeys(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		as.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// createAPIKey creates a new API key.
// POST /api/v1/auth/keys
// Request: {"name": "CI Pipeline", "scopes": ["pools:read"], "expires_in_days": 90}
// Response: {"id": "...", "key": "cpam_xxx...", "prefix": "cpam_xxx", "name": "...", "scopes": [...], "created_at": "...", "expires_at": "..."}
func (as *AuthServer) createAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var input struct {
		Name          string   `json:"name"`
		Scopes        []string `json:"scopes"`
		ExpiresInDays *int     `json:"expires_in_days"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		as.writeErr(ctx, w, http.StatusBadRequest, "invalid json", "")
		return
	}

	// Validate name
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		as.writeErr(ctx, w, http.StatusBadRequest, "name is required", "")
		return
	}
	if len(input.Name) > 255 {
		as.writeErr(ctx, w, http.StatusBadRequest, "name too long", "maximum 255 characters")
		return
	}

	settings := as.securitySettings(ctx)

	// Validate scopes
	for _, scope := range input.Scopes {
		if !auth.IsValidAPIKeyScope(scope) {
			as.writeErr(ctx, w, http.StatusBadRequest, "invalid scope", scope)
			return
		}
	}

	// Prevent scope elevation: callers cannot create keys with higher privileges than their own role.
	// Only enforced when the caller is authenticated (callerRole != RoleNone).
	callerRole := auth.GetEffectiveRole(r.Context())
	if callerRole != auth.RoleNone {
		requestedRole := auth.GetRoleFromScopes(input.Scopes)
		if auth.RoleLevel(requestedRole) > auth.RoleLevel(callerRole) {
			as.writeErr(r.Context(), w, http.StatusForbidden, "scope elevation denied",
				"requested scopes require a higher privilege level than your current role")
			return
		}
		if deniedScope := deniedAPIKeyScope(settings, callerRole, input.Scopes); deniedScope != "" {
			as.writeErr(r.Context(), w, http.StatusForbidden, "scope denied by API key policy", deniedScope)
			return
		}
	}

	// Calculate expiration
	expiresAt, err := apiKeyExpiresAt(time.Now().UTC(), input.ExpiresInDays, settings)
	if err != nil {
		as.writeErr(ctx, w, http.StatusBadRequest, "invalid expires_in_days", err.Error())
		return
	}

	// Generate the API key
	plaintext, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
		Name:      input.Name,
		Scopes:    input.Scopes,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		as.writeErr(ctx, w, http.StatusInternalServerError, "failed to generate key", err.Error())
		return
	}

	// Set owner if created by a session-authenticated user.
	if user := auth.UserFromContext(ctx); user != nil {
		apiKey.OwnerID = &user.ID
	}

	// Store the key
	if err := as.keyStore.Create(ctx, apiKey); err != nil {
		as.writeErr(ctx, w, http.StatusInternalServerError, "failed to store key", err.Error())
		return
	}

	// Log creation
	fields := appendRequestID(ctx, []any{
		"key_id", apiKey.ID,
		"key_prefix", apiKey.Prefix,
		"key_name", apiKey.Name,
	})
	if apiKey.OwnerID != nil {
		fields = append(fields, "owner_id", *apiKey.OwnerID)
	}
	as.logger.InfoContext(ctx, "api key created", fields...)

	// Return the key with the plaintext (only time it's shown)
	response := struct {
		ID        string     `json:"id"`
		Key       string     `json:"key"`
		Prefix    string     `json:"prefix"`
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		OwnerID   *string    `json:"owner_id,omitempty"`
		CreatedAt time.Time  `json:"created_at"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}{
		ID:        apiKey.ID,
		Key:       plaintext,
		Prefix:    apiKey.Prefix,
		Name:      apiKey.Name,
		Scopes:    apiKey.Scopes,
		OwnerID:   apiKey.OwnerID,
		CreatedAt: apiKey.CreatedAt,
		ExpiresAt: apiKey.ExpiresAt,
	}

	writeJSON(w, http.StatusCreated, response)
}

// listAPIKeys lists API keys (without secrets).
// Admins see all keys; non-admin session users see only their own keys.
// GET /api/v1/auth/keys
func (as *AuthServer) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	role := auth.GetEffectiveRole(ctx)
	user := auth.UserFromContext(ctx)

	var keys []*auth.APIKey
	var err error

	// Non-admin session users only see their own keys.
	if user != nil && role != auth.RoleAdmin {
		keys, err = as.keyStore.ListByOwner(ctx, user.ID)
	} else {
		keys, err = as.keyStore.List(ctx)
	}
	if err != nil {
		as.writeErr(ctx, w, http.StatusInternalServerError, "failed to list keys", err.Error())
		return
	}

	settings := as.securitySettings(ctx)
	now := time.Now().UTC()

	// Transform to response format (never include hash)
	type keyResponse struct {
		ID            string     `json:"id"`
		Prefix        string     `json:"prefix"`
		Name          string     `json:"name"`
		Scopes        []string   `json:"scopes"`
		OwnerID       *string    `json:"owner_id,omitempty"`
		CreatedAt     time.Time  `json:"created_at"`
		ExpiresAt     *time.Time `json:"expires_at,omitempty"`
		LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
		Revoked       bool       `json:"revoked"`
		AgeDays       int        `json:"age_days"`
		ExpiresInDays *int       `json:"expires_in_days,omitempty"`
		ExpiryStatus  string     `json:"expiry_status"`
		RotationDue   bool       `json:"rotation_due"`
	}

	response := struct {
		Keys []keyResponse `json:"keys"`
	}{
		Keys: make([]keyResponse, len(keys)),
	}

	for i, k := range keys {
		status, expiresInDays, rotationDue := apiKeyExpiryStatus(k, settings, now)
		if rotationDue {
			as.logAPIKeyRotationDue(ctx, k, expiresInDays)
		}
		response.Keys[i] = keyResponse{
			ID:            k.ID,
			Prefix:        k.Prefix,
			Name:          k.Name,
			Scopes:        k.Scopes,
			OwnerID:       k.OwnerID,
			CreatedAt:     k.CreatedAt,
			ExpiresAt:     k.ExpiresAt,
			LastUsedAt:    k.LastUsedAt,
			Revoked:       k.Revoked,
			AgeDays:       daysSince(k.CreatedAt, now),
			ExpiresInDays: expiresInDays,
			ExpiryStatus:  status,
			RotationDue:   rotationDue,
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (as *AuthServer) securitySettings(ctx context.Context) domain.SecuritySettings {
	if as.settingsStore == nil {
		return domain.DefaultSecuritySettings()
	}
	settings, err := as.settingsStore.GetSecuritySettings(ctx)
	if err != nil || settings == nil {
		return domain.DefaultSecuritySettings()
	}
	return *domain.NormalizeSecuritySettings(settings)
}

func apiKeyExpiresAt(now time.Time, requestedDays *int, settings domain.SecuritySettings) (*time.Time, error) {
	var days int
	explicit := requestedDays != nil
	switch {
	case explicit && *requestedDays < 0:
		return nil, fmt.Errorf("must be 0 or greater")
	case explicit:
		days = *requestedDays
	case settings.APIKeyDefaultExpiryDays > 0:
		days = settings.APIKeyDefaultExpiryDays
	}

	if settings.APIKeyMaxLifetimeDays > 0 {
		if days == 0 {
			days = settings.APIKeyMaxLifetimeDays
		} else if days > settings.APIKeyMaxLifetimeDays {
			if explicit {
				return nil, fmt.Errorf("must be less than or equal to API key maximum lifetime of %d days", settings.APIKeyMaxLifetimeDays)
			}
			days = settings.APIKeyMaxLifetimeDays
		}
	}

	if days <= 0 {
		return nil, nil
	}
	expiresAt := now.AddDate(0, 0, days)
	return &expiresAt, nil
}

func deniedAPIKeyScope(settings domain.SecuritySettings, role auth.Role, scopes []string) string {
	allowed, ok := settings.APIKeyAllowedScopesByRole[string(role)]
	if !ok {
		return ""
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, scope := range allowed {
		allowedSet[scope] = struct{}{}
	}
	for _, scope := range scopes {
		if _, ok := allowedSet[scope]; !ok {
			return scope
		}
	}
	return ""
}

func apiKeyExpiryStatus(key *auth.APIKey, settings domain.SecuritySettings, now time.Time) (string, *int, bool) {
	if key.Revoked {
		return "revoked", nil, false
	}
	if key.ExpiresAt == nil {
		return "no_expiry", nil, false
	}
	days := daysUntil(*key.ExpiresAt, now)
	if !key.ExpiresAt.After(now) {
		return "expired", &days, false
	}
	rotationDue := settings.APIKeyRotationReminderDays > 0 && days <= settings.APIKeyRotationReminderDays
	if rotationDue {
		return "expiring", &days, true
	}
	return "active", &days, false
}

func daysSince(t, now time.Time) int {
	if t.IsZero() || t.After(now) {
		return 0
	}
	return int(now.Sub(t).Hours() / 24)
}

func daysUntil(t, now time.Time) int {
	if !t.After(now) {
		return 0
	}
	d := t.Sub(now)
	days := int(d / (24 * time.Hour))
	if d%(24*time.Hour) != 0 {
		days++
	}
	return days
}

func (as *AuthServer) logAPIKeyRotationDue(ctx context.Context, key *auth.APIKey, expiresInDays *int) {
	if as.auditLogger == nil || key == nil {
		return
	}
	actor, actorType := auditActorFromContext(ctx)
	after := map[string]any{
		"prefix": key.Prefix,
	}
	if expiresInDays != nil {
		after["expires_in_days"] = *expiresInDays
	}
	if key.ExpiresAt != nil {
		after["expires_at"] = key.ExpiresAt.Format(time.RFC3339)
	}
	if events, err := as.auditLogger.GetByResource(ctx, audit.ResourceAPIKey, key.ID); err == nil {
		for _, event := range events {
			if event.Action != audit.ActionAPIKeyRotationDue || event.Changes == nil {
				continue
			}
			if event.Changes.After["expires_at"] == after["expires_at"] {
				return
			}
		}
	}
	_ = as.auditLogger.Log(ctx, &audit.AuditEvent{
		Timestamp:    time.Now().UTC(),
		Actor:        actor,
		ActorType:    actorType,
		Action:       audit.ActionAPIKeyRotationDue,
		ResourceType: audit.ResourceAPIKey,
		ResourceID:   key.ID,
		ResourceName: key.Name,
		Changes:      &audit.Changes{After: after},
		RequestID:    RequestIDFromContext(ctx),
		StatusCode:   http.StatusOK,
	})
}

func auditActorFromContext(ctx context.Context) (string, string) {
	if user := auth.UserFromContext(ctx); user != nil {
		return user.ID, audit.ActorTypeUser
	}
	if key := auth.APIKeyFromContext(ctx); key != nil {
		return key.Prefix, audit.ActorTypeAPIKey
	}
	return "anonymous", audit.ActorTypeAnonymous
}

// handleAPIKeyByID handles DELETE /api/v1/auth/keys/{id} (revoke).
func (as *AuthServer) handleAPIKeyByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/auth/keys/")
	id := strings.Trim(path, "/")

	if id == "" {
		as.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
		return
	}

	switch r.Method {
	case http.MethodDelete:
		as.revokeAPIKey(w, r, id)
	default:
		w.Header().Set("Allow", http.MethodDelete)
		as.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// revokeAPIKey revokes an API key (soft delete).
// Non-admin session users can only revoke their own keys.
// DELETE /api/v1/auth/keys/{id}
func (as *AuthServer) revokeAPIKey(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	// Check if key exists first
	key, err := as.keyStore.GetByID(ctx, id)
	if err != nil {
		as.writeErr(ctx, w, http.StatusInternalServerError, "failed to get key", err.Error())
		return
	}
	if key == nil {
		as.writeErr(ctx, w, http.StatusNotFound, "not found", "")
		return
	}

	// Non-admin session users can only revoke their own keys.
	role := auth.GetEffectiveRole(ctx)
	user := auth.UserFromContext(ctx)
	if user != nil && role != auth.RoleAdmin {
		if key.OwnerID == nil || *key.OwnerID != user.ID {
			writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden", Detail: "can only revoke your own keys"})
			return
		}
	}

	// Revoke the key
	if err := as.keyStore.Revoke(ctx, id); err != nil {
		if err == auth.ErrKeyNotFound {
			as.writeErr(ctx, w, http.StatusNotFound, "not found", "")
			return
		}
		as.writeErr(ctx, w, http.StatusInternalServerError, "failed to revoke key", err.Error())
		return
	}

	// Log revocation
	fields := appendRequestID(ctx, []any{
		"key_id", id,
		"key_prefix", key.Prefix,
		"key_name", key.Name,
	})
	as.logger.InfoContext(ctx, "api key revoked", fields...)

	w.WriteHeader(http.StatusNoContent)
}

// handleAuditList handles GET /api/v1/audit.
// Query params: limit, offset, actor, action, resource_type, since, until
func (as *AuthServer) handleAuditList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		as.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	ctx := r.Context()
	q := r.URL.Query()

	// Parse pagination
	limit := 50 // default
	if v := q.Get("limit"); v != "" {
		if parsed, err := parseInt(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 1000 {
		limit = 1000 // max
	}

	offset := 0
	if v := q.Get("offset"); v != "" {
		if parsed, err := parseInt(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Parse filters
	opts := audit.ListOptions{
		Limit:        limit,
		Offset:       offset,
		Actor:        q.Get("actor"),
		Action:       q.Get("action"),
		ResourceType: q.Get("resource_type"),
	}

	// Parse time filters
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			opts.Since = &t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			opts.Until = &t
		}
	}

	// Query audit logs
	events, total, err := as.auditLogger.List(ctx, opts)
	if err != nil {
		as.writeErr(ctx, w, http.StatusInternalServerError, "failed to list audit events", err.Error())
		return
	}

	// Build response
	response := struct {
		Events []*audit.AuditEvent `json:"events"`
		Total  int                 `json:"total"`
		Limit  int                 `json:"limit"`
		Offset int                 `json:"offset"`
	}{
		Events: events,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}

	writeJSON(w, http.StatusOK, response)
}

// parseInt parses a string to int, returning error if invalid.
func parseInt(s string) (int, error) {
	var v int
	_, err := strings.NewReader(s).Read([]byte{})
	if err != nil {
		return 0, err
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, http.ErrNotSupported
		}
		v = v*10 + int(c-'0')
	}
	return v, nil
}
