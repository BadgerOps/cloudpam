package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
)

// AuthServer extends Server with API key management and audit functionality.
type AuthServer struct {
	*Server
	keyStore    auth.KeyStore
	auditLogger audit.AuditLogger
}

// NewAuthServer creates a new AuthServer with auth and audit capabilities.
func NewAuthServer(s *Server, keyStore auth.KeyStore, auditLogger audit.AuditLogger) *AuthServer {
	return &AuthServer{
		Server:      s,
		keyStore:    keyStore,
		auditLogger: auditLogger,
	}
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

	// Auth middleware (required for all these endpoints)
	authMW := AuthMiddleware(as.keyStore, true, logger)

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

	// Validate scopes
	if len(input.Scopes) > 0 {
		validScopes := map[string]bool{
			"pools:read":     true,
			"pools:write":    true,
			"accounts:read":  true,
			"accounts:write": true,
			"audit:read":     true,
			"keys:read":      true,
			"keys:write":     true,
			"*":              true, // admin scope
		}
		for _, scope := range input.Scopes {
			if !validScopes[scope] {
				as.writeErr(ctx, w, http.StatusBadRequest, "invalid scope", scope)
				return
			}
		}
	}

	// Calculate expiration
	var expiresAt *time.Time
	if input.ExpiresInDays != nil && *input.ExpiresInDays > 0 {
		t := time.Now().UTC().AddDate(0, 0, *input.ExpiresInDays)
		expiresAt = &t
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
	as.logger.InfoContext(ctx, "api key created", fields...)

	// Return the key with the plaintext (only time it's shown)
	response := struct {
		ID        string     `json:"id"`
		Key       string     `json:"key"`
		Prefix    string     `json:"prefix"`
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		CreatedAt time.Time  `json:"created_at"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}{
		ID:        apiKey.ID,
		Key:       plaintext,
		Prefix:    apiKey.Prefix,
		Name:      apiKey.Name,
		Scopes:    apiKey.Scopes,
		CreatedAt: apiKey.CreatedAt,
		ExpiresAt: apiKey.ExpiresAt,
	}

	writeJSON(w, http.StatusCreated, response)
}

// listAPIKeys lists all API keys (without secrets).
// GET /api/v1/auth/keys
func (as *AuthServer) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keys, err := as.keyStore.List(ctx)
	if err != nil {
		as.writeErr(ctx, w, http.StatusInternalServerError, "failed to list keys", err.Error())
		return
	}

	// Transform to response format (never include hash)
	type keyResponse struct {
		ID         string     `json:"id"`
		Prefix     string     `json:"prefix"`
		Name       string     `json:"name"`
		Scopes     []string   `json:"scopes"`
		CreatedAt  time.Time  `json:"created_at"`
		ExpiresAt  *time.Time `json:"expires_at,omitempty"`
		LastUsedAt *time.Time `json:"last_used_at,omitempty"`
		Revoked    bool       `json:"revoked"`
	}

	response := struct {
		Keys []keyResponse `json:"keys"`
	}{
		Keys: make([]keyResponse, len(keys)),
	}

	for i, k := range keys {
		response.Keys[i] = keyResponse{
			ID:         k.ID,
			Prefix:     k.Prefix,
			Name:       k.Name,
			Scopes:     k.Scopes,
			CreatedAt:  k.CreatedAt,
			ExpiresAt:  k.ExpiresAt,
			LastUsedAt: k.LastUsedAt,
			Revoked:    k.Revoked,
		}
	}

	writeJSON(w, http.StatusOK, response)
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
