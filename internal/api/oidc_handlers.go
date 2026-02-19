package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/auth/oidc"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// OIDCServer extends Server with OIDC authentication capabilities.
type OIDCServer struct {
	*Server
	oidcStore     storage.OIDCProviderStore
	sessionStore  auth.SessionStore
	userStore     auth.UserStore
	settingsStore storage.SettingsStore
	encryptionKey []byte
	callbackURL   string
	providerCache sync.Map // id -> *oidc.Provider
}

// NewOIDCServer creates a new OIDCServer.
func NewOIDCServer(
	s *Server,
	oidcStore storage.OIDCProviderStore,
	sessionStore auth.SessionStore,
	userStore auth.UserStore,
	settingsStore storage.SettingsStore,
	encryptionKey []byte,
	callbackURL string,
) *OIDCServer {
	return &OIDCServer{
		Server:        s,
		oidcStore:     oidcStore,
		sessionStore:  sessionStore,
		userStore:     userStore,
		settingsStore: settingsStore,
		encryptionKey: encryptionKey,
		callbackURL:   callbackURL,
	}
}

// RegisterOIDCRoutes registers the OIDC public routes (no auth required).
func (os *OIDCServer) RegisterOIDCRoutes(_ *slog.Logger) {
	os.mux.HandleFunc("/api/v1/auth/oidc/login", os.handleOIDCLogin)
	os.mux.HandleFunc("/api/v1/auth/oidc/callback", os.handleOIDCCallback)
	os.mux.HandleFunc("/api/v1/auth/oidc/refresh", os.handleOIDCRefresh)
	os.mux.HandleFunc("/api/v1/auth/oidc/providers", os.handleListPublicProviders)
}

// RegisterOIDCAdminRoutes registers admin OIDC management routes with RBAC.
func (os *OIDCServer) RegisterOIDCAdminRoutes(dualMW func(http.Handler) http.Handler, slogger *slog.Logger) {
	adminRead := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionRead, slogger)
	adminWrite := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionWrite, slogger)

	os.mux.Handle("GET /api/v1/settings/oidc/providers",
		dualMW(adminRead(http.HandlerFunc(os.handleAdminListProviders))))
	os.mux.Handle("POST /api/v1/settings/oidc/providers",
		dualMW(adminWrite(http.HandlerFunc(os.handleAdminCreateProvider))))
	os.mux.Handle("GET /api/v1/settings/oidc/providers/{id}",
		dualMW(adminRead(http.HandlerFunc(os.handleAdminGetProvider))))
	os.mux.Handle("PATCH /api/v1/settings/oidc/providers/{id}",
		dualMW(adminWrite(http.HandlerFunc(os.handleAdminUpdateProvider))))
	os.mux.Handle("DELETE /api/v1/settings/oidc/providers/{id}",
		dualMW(adminWrite(http.HandlerFunc(os.handleAdminDeleteProvider))))
	os.mux.Handle("POST /api/v1/settings/oidc/providers/{id}/test",
		dualMW(adminWrite(http.HandlerFunc(os.handleAdminTestProvider))))
}

// RegisterOIDCAdminRoutesNoAuth registers admin OIDC routes without auth middleware (for tests).
func (os *OIDCServer) RegisterOIDCAdminRoutesNoAuth() {
	os.mux.HandleFunc("GET /api/v1/settings/oidc/providers", os.handleAdminListProviders)
	os.mux.HandleFunc("POST /api/v1/settings/oidc/providers", os.handleAdminCreateProvider)
	os.mux.HandleFunc("GET /api/v1/settings/oidc/providers/{id}", os.handleAdminGetProvider)
	os.mux.HandleFunc("PATCH /api/v1/settings/oidc/providers/{id}", os.handleAdminUpdateProvider)
	os.mux.HandleFunc("DELETE /api/v1/settings/oidc/providers/{id}", os.handleAdminDeleteProvider)
	os.mux.HandleFunc("POST /api/v1/settings/oidc/providers/{id}/test", os.handleAdminTestProvider)
}

// handleAdminListProviders returns all OIDC providers with secrets masked.
// GET /api/v1/settings/oidc/providers
func (os *OIDCServer) handleAdminListProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	providers, err := os.oidcStore.ListProviders(ctx)
	if err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to list providers", err.Error())
		return
	}

	for _, p := range providers {
		p.ClientSecretMasked = "****"
	}

	writeJSON(w, http.StatusOK, struct {
		Providers []*domain.OIDCProvider `json:"providers"`
	}{Providers: providers})
}

// handleAdminCreateProvider creates a new OIDC provider configuration.
// POST /api/v1/settings/oidc/providers
func (os *OIDCServer) handleAdminCreateProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var input struct {
		Name          string            `json:"name"`
		IssuerURL     string            `json:"issuer_url"`
		ClientID      string            `json:"client_id"`
		ClientSecret  string            `json:"client_secret"`
		Scopes        string            `json:"scopes"`
		RoleMapping   map[string]string `json:"role_mapping"`
		DefaultRole   string            `json:"default_role"`
		AutoProvision bool              `json:"auto_provision"`
		Enabled       bool              `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		os.writeErr(ctx, w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Validate required fields.
	if input.Name == "" {
		os.writeErr(ctx, w, http.StatusBadRequest, "name is required", "")
		return
	}
	if input.IssuerURL == "" {
		os.writeErr(ctx, w, http.StatusBadRequest, "issuer_url is required", "")
		return
	}
	if input.ClientID == "" {
		os.writeErr(ctx, w, http.StatusBadRequest, "client_id is required", "")
		return
	}
	if input.ClientSecret == "" {
		os.writeErr(ctx, w, http.StatusBadRequest, "client_secret is required", "")
		return
	}

	// Encrypt client secret.
	encSecret, err := oidc.Encrypt(input.ClientSecret, os.encryptionKey)
	if err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to encrypt client secret", err.Error())
		return
	}

	now := time.Now().UTC()
	provider := &domain.OIDCProvider{
		ID:                    uuid.New().String(),
		Name:                  input.Name,
		IssuerURL:             input.IssuerURL,
		ClientID:              input.ClientID,
		ClientSecretEncrypted: encSecret,
		Scopes:                input.Scopes,
		RoleMapping:           input.RoleMapping,
		DefaultRole:           input.DefaultRole,
		AutoProvision:         input.AutoProvision,
		Enabled:               input.Enabled,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	if err := os.oidcStore.CreateProvider(ctx, provider); err != nil {
		if errors.Is(err, storage.ErrDuplicateIssuer) {
			os.writeErr(ctx, w, http.StatusConflict, "provider with this issuer URL already exists", "")
			return
		}
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to create provider", err.Error())
		return
	}

	// Invalidate any cached provider with this ID.
	os.providerCache.Delete(provider.ID)

	os.logOIDCAudit(ctx, "create", "oidc_provider", provider.ID, provider.Name, http.StatusCreated)

	provider.ClientSecretMasked = "****"
	writeJSON(w, http.StatusCreated, provider)
}

// handleAdminGetProvider returns a single OIDC provider with secret masked.
// GET /api/v1/settings/oidc/providers/{id}
func (os *OIDCServer) handleAdminGetProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	provider, err := os.oidcStore.GetProvider(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			os.writeErr(ctx, w, http.StatusNotFound, "provider not found", "")
			return
		}
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to get provider", err.Error())
		return
	}

	provider.ClientSecretMasked = "****"
	writeJSON(w, http.StatusOK, provider)
}

// handleAdminUpdateProvider partially updates an OIDC provider.
// PATCH /api/v1/settings/oidc/providers/{id}
func (os *OIDCServer) handleAdminUpdateProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	provider, err := os.oidcStore.GetProvider(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			os.writeErr(ctx, w, http.StatusNotFound, "provider not found", "")
			return
		}
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to get provider", err.Error())
		return
	}

	// Decode partial update.
	var input map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		os.writeErr(ctx, w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if v, ok := input["name"]; ok {
		var name string
		if err := json.Unmarshal(v, &name); err == nil {
			provider.Name = name
		}
	}
	if v, ok := input["issuer_url"]; ok {
		var issuerURL string
		if err := json.Unmarshal(v, &issuerURL); err == nil {
			provider.IssuerURL = issuerURL
		}
	}
	if v, ok := input["client_id"]; ok {
		var clientID string
		if err := json.Unmarshal(v, &clientID); err == nil {
			provider.ClientID = clientID
		}
	}
	if v, ok := input["client_secret"]; ok {
		var clientSecret string
		if err := json.Unmarshal(v, &clientSecret); err == nil && clientSecret != "" {
			encSecret, encErr := oidc.Encrypt(clientSecret, os.encryptionKey)
			if encErr != nil {
				os.writeErr(ctx, w, http.StatusInternalServerError, "failed to encrypt client secret", encErr.Error())
				return
			}
			provider.ClientSecretEncrypted = encSecret
		}
	}
	if v, ok := input["scopes"]; ok {
		var scopes string
		if err := json.Unmarshal(v, &scopes); err == nil {
			provider.Scopes = scopes
		}
	}
	if v, ok := input["role_mapping"]; ok {
		var roleMapping map[string]string
		if err := json.Unmarshal(v, &roleMapping); err == nil {
			provider.RoleMapping = roleMapping
		}
	}
	if v, ok := input["default_role"]; ok {
		var defaultRole string
		if err := json.Unmarshal(v, &defaultRole); err == nil {
			provider.DefaultRole = defaultRole
		}
	}
	if v, ok := input["auto_provision"]; ok {
		var autoProvision bool
		if err := json.Unmarshal(v, &autoProvision); err == nil {
			provider.AutoProvision = autoProvision
		}
	}
	if v, ok := input["enabled"]; ok {
		var enabled bool
		if err := json.Unmarshal(v, &enabled); err == nil {
			provider.Enabled = enabled
		}
	}

	provider.UpdatedAt = time.Now().UTC()

	if err := os.oidcStore.UpdateProvider(ctx, provider); err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to update provider", err.Error())
		return
	}

	// Invalidate cached provider so changes take effect.
	os.providerCache.Delete(id)

	os.logOIDCAudit(ctx, "update", "oidc_provider", provider.ID, provider.Name, http.StatusOK)

	provider.ClientSecretMasked = "****"
	writeJSON(w, http.StatusOK, provider)
}

// handleAdminDeleteProvider removes an OIDC provider.
// DELETE /api/v1/settings/oidc/providers/{id}
func (os *OIDCServer) handleAdminDeleteProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	if err := os.oidcStore.DeleteProvider(ctx, id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			os.writeErr(ctx, w, http.StatusNotFound, "provider not found", "")
			return
		}
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to delete provider", err.Error())
		return
	}

	// Invalidate cached provider.
	os.providerCache.Delete(id)

	os.logOIDCAudit(ctx, "delete", "oidc_provider", id, "", http.StatusNoContent)

	w.WriteHeader(http.StatusNoContent)
}

// handleAdminTestProvider tests OIDC discovery for a configured provider.
// POST /api/v1/settings/oidc/providers/{id}/test
func (os *OIDCServer) handleAdminTestProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	provider, err := os.oidcStore.GetProvider(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			os.writeErr(ctx, w, http.StatusNotFound, "provider not found", "")
			return
		}
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to get provider", err.Error())
		return
	}

	// Decrypt client secret.
	clientSecret, err := oidc.Decrypt(provider.ClientSecretEncrypted, os.encryptionKey)
	if err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to decrypt client secret", "")
		return
	}

	// Attempt OIDC discovery.
	scopes := strings.Split(provider.Scopes, ",")
	for i := range scopes {
		scopes[i] = strings.TrimSpace(scopes[i])
	}
	if len(scopes) == 1 && scopes[0] == "" {
		scopes = nil
	}

	_, testErr := oidc.NewProvider(ctx, oidc.ProviderConfig{
		IssuerURL:    provider.IssuerURL,
		ClientID:     provider.ClientID,
		ClientSecret: clientSecret,
		RedirectURL:  os.callbackURL,
		Scopes:       scopes,
	})

	result := struct {
		Success   bool   `json:"success"`
		IssuerURL string `json:"issuer_url"`
		Message   string `json:"message,omitempty"`
	}{
		IssuerURL: provider.IssuerURL,
	}

	if testErr != nil {
		result.Success = false
		result.Message = fmt.Sprintf("discovery failed: %v", testErr)
		writeJSON(w, http.StatusOK, result)
		return
	}

	result.Success = true
	result.Message = "oidc discovery successful"
	writeJSON(w, http.StatusOK, result)
}

// handleOIDCLogin redirects the user to the OIDC identity provider.
// GET /api/v1/auth/oidc/login?provider_id=xxx
func (os *OIDCServer) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		os.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	ctx := r.Context()
	providerID := r.URL.Query().Get("provider_id")
	if providerID == "" {
		os.writeErr(ctx, w, http.StatusBadRequest, "provider_id is required", "")
		return
	}

	// Look up provider config.
	provCfg, err := os.oidcStore.GetProvider(ctx, providerID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			os.writeErr(ctx, w, http.StatusNotFound, "provider not found", "")
			return
		}
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to get provider", err.Error())
		return
	}
	if !provCfg.Enabled {
		os.writeErr(ctx, w, http.StatusBadRequest, "provider is disabled", "")
		return
	}

	// Decrypt client secret.
	clientSecret, err := oidc.Decrypt(provCfg.ClientSecretEncrypted, os.encryptionKey)
	if err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to decrypt client secret", "")
		return
	}

	// Get or create OIDC provider (cached).
	prov, err := os.getOrCreateProvider(ctx, provCfg, clientSecret)
	if err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to initialize oidc provider", err.Error())
		return
	}

	// Generate state: providerID:random
	randomBytes := make([]byte, 24)
	if _, err := rand.Read(randomBytes); err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to generate state", err.Error())
		return
	}
	nonce := base64.RawURLEncoding.EncodeToString(randomBytes)
	state := providerID + ":" + nonce

	// Store state+nonce in cookie (10 min expiry).
	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    state,
		Path:     "/api/v1/auth/oidc/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600, // 10 minutes
	})

	// Build auth URL with nonce.
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("nonce", nonce),
	}
	if prompt := r.URL.Query().Get("prompt"); prompt != "" {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", prompt))
	}

	authURL := prov.AuthCodeURL(state, opts...)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleOIDCCallback handles the OIDC callback after IdP authentication.
// GET /api/v1/auth/oidc/callback?code=xxx&state=xxx
func (os *OIDCServer) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		os.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	ctx := r.Context()
	isIframe := r.Header.Get("Sec-Fetch-Dest") == "iframe"

	// Check for error parameter from IdP (e.g., login_required for prompt=none).
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		// Sanitize errParam to prevent reflected XSS.
		safeErr := html.EscapeString(errParam)
		if isIframe {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><body><script>window.parent.postMessage({type:"oidc-refresh",success:false,error:%q},"*");</script></body></html>`, safeErr)
			return
		}
		http.Redirect(w, r, "/?error="+url.QueryEscape(errParam), http.StatusFound)
		return
	}

	// Read and validate params.
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		os.writeErr(ctx, w, http.StatusBadRequest, "missing code or state", "")
		return
	}

	// Validate state matches cookie.
	stateCookie, err := r.Cookie("oidc_state")
	if err != nil || stateCookie.Value != state {
		os.writeErr(ctx, w, http.StatusForbidden, "invalid state", "state mismatch")
		return
	}

	// Clear state cookie.
	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    "",
		Path:     "/api/v1/auth/oidc/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	// Parse provider_id from state (format: providerID:random).
	parts := strings.SplitN(state, ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		os.writeErr(ctx, w, http.StatusBadRequest, "malformed state", "")
		return
	}
	providerID := parts[0]

	// Look up provider config.
	provCfg, err := os.oidcStore.GetProvider(ctx, providerID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			os.writeErr(ctx, w, http.StatusNotFound, "provider not found", "")
			return
		}
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to get provider", err.Error())
		return
	}

	// Decrypt client secret and get/create provider.
	clientSecret, err := oidc.Decrypt(provCfg.ClientSecretEncrypted, os.encryptionKey)
	if err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to decrypt client secret", "")
		return
	}

	prov, err := os.getOrCreateProvider(ctx, provCfg, clientSecret)
	if err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to initialize oidc provider", err.Error())
		return
	}

	// Exchange code for claims.
	claims, err := prov.Exchange(ctx, code)
	if err != nil {
		os.writeErr(ctx, w, http.StatusUnauthorized, "token exchange failed", err.Error())
		return
	}

	// Look up user by OIDC identity.
	user, err := os.userStore.GetByOIDCIdentity(ctx, claims.Issuer, claims.Subject)
	if err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to look up user", err.Error())
		return
	}

	// JIT provisioning if user not found and auto_provision is enabled.
	if user == nil {
		if !provCfg.AutoProvision {
			os.writeErr(ctx, w, http.StatusForbidden, "user not provisioned", "auto-provisioning is disabled for this provider")
			return
		}

		role := oidc.MapRole(*claims, provCfg.RoleMapping, auth.ParseRole(provCfg.DefaultRole))
		if role == auth.RoleNone {
			role = auth.RoleViewer
		}

		// Generate a username from email or subject.
		username := claims.Email
		if username == "" {
			username = fmt.Sprintf("oidc_%s", claims.Subject)
		}

		now := time.Now().UTC()
		user = &auth.User{
			ID:           uuid.New().String(),
			Username:     username,
			Email:        claims.Email,
			DisplayName:  claims.Name,
			Role:         role,
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
			AuthProvider: "oidc",
			OIDCSubject:  claims.Subject,
			OIDCIssuer:   claims.Issuer,
		}

		if err := os.userStore.Create(ctx, user); err != nil {
			if err == auth.ErrUserExists {
				// Username collision: try with a suffix.
				user.Username = fmt.Sprintf("%s_%s", username, claims.Subject[:min(8, len(claims.Subject))])
				if err := os.userStore.Create(ctx, user); err != nil {
					os.writeErr(ctx, w, http.StatusInternalServerError, "failed to provision user", err.Error())
					return
				}
			} else {
				os.writeErr(ctx, w, http.StatusInternalServerError, "failed to provision user", err.Error())
				return
			}
		}

		os.logOIDCAudit(ctx, "oidc_provision", audit.ResourceUser, user.ID, user.Username, http.StatusCreated)
	}

	// Check if user is active.
	if !user.IsActive {
		os.logOIDCAudit(ctx, audit.ActionLoginFailed, audit.ResourceSession, "", user.Username, http.StatusForbidden)
		os.writeErr(ctx, w, http.StatusForbidden, "account disabled", "")
		return
	}

	// Update role from current claims (role sync on each login).
	newRole := oidc.MapRole(*claims, provCfg.RoleMapping, auth.ParseRole(provCfg.DefaultRole))
	if newRole != auth.RoleNone && newRole != user.Role {
		user.Role = newRole
		user.UpdatedAt = time.Now().UTC()
		_ = os.userStore.Update(ctx, user)
	}

	// Create session.
	session, err := auth.NewSession(user.ID, user.Role, auth.DefaultSessionDuration, map[string]string{
		"auth_provider": "oidc",
		"oidc_issuer":   claims.Issuer,
	})
	if err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to create session", err.Error())
		return
	}
	if err := os.sessionStore.Create(ctx, session); err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to store session", err.Error())
		return
	}

	// Update last login.
	now := time.Now().UTC()
	_ = os.userStore.UpdateLastLogin(ctx, user.ID, now)

	// Set session cookie.
	sameSite := http.SameSiteLaxMode
	if isSecure {
		sameSite = http.SameSiteStrictMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: sameSite,
		MaxAge:   int(auth.DefaultSessionDuration.Seconds()),
	})

	os.logOIDCAudit(ctx, audit.ActionLogin, audit.ResourceSession, session.ID, user.Username, http.StatusOK)

	// If this is an iframe-based silent re-auth, post a message to the parent window.
	if isIframe {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body><script>window.parent.postMessage({type:"oidc-refresh",success:true},"*");</script></body></html>`)
		return
	}

	// Redirect to the frontend.
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleOIDCRefresh handles silent re-authentication.
// POST /api/v1/auth/oidc/refresh
func (os *OIDCServer) handleOIDCRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		os.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	ctx := r.Context()

	// Get session from cookie.
	sessionCookie, err := r.Cookie("session")
	if err != nil || sessionCookie.Value == "" {
		os.writeErr(ctx, w, http.StatusUnauthorized, "no session", "")
		return
	}

	session, err := os.sessionStore.Get(ctx, sessionCookie.Value)
	if err != nil || session == nil {
		os.writeErr(ctx, w, http.StatusUnauthorized, "invalid session", "")
		return
	}

	// Look up user to find their OIDC issuer.
	user, err := os.userStore.GetByID(ctx, session.UserID)
	if err != nil || user == nil {
		os.writeErr(ctx, w, http.StatusUnauthorized, "user not found", "")
		return
	}

	if user.AuthProvider != "oidc" || user.OIDCIssuer == "" {
		os.writeErr(ctx, w, http.StatusBadRequest, "not an oidc session", "")
		return
	}

	// Find the provider by issuer.
	provCfg, err := os.oidcStore.GetProviderByIssuer(ctx, user.OIDCIssuer)
	if err != nil {
		os.writeErr(ctx, w, http.StatusNotFound, "oidc provider not found", "")
		return
	}

	// Return redirect URL for frontend iframe refresh.
	redirectURL := fmt.Sprintf("/api/v1/auth/oidc/login?provider_id=%s&prompt=none", provCfg.ID)
	writeJSON(w, http.StatusOK, struct {
		RedirectURL string `json:"redirect_url"`
	}{RedirectURL: redirectURL})
}

// handleListPublicProviders returns enabled OIDC providers (id and name only).
// GET /api/v1/auth/oidc/providers
func (os *OIDCServer) handleListPublicProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		os.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	ctx := r.Context()

	providers, err := os.oidcStore.ListEnabledProviders(ctx)
	if err != nil {
		os.writeErr(ctx, w, http.StatusInternalServerError, "failed to list providers", err.Error())
		return
	}

	type publicProvider struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	result := make([]publicProvider, len(providers))
	for i, p := range providers {
		result[i] = publicProvider{ID: p.ID, Name: p.Name}
	}

	writeJSON(w, http.StatusOK, struct {
		Providers []publicProvider `json:"providers"`
	}{Providers: result})
}

// getOrCreateProvider returns a cached OIDC Provider or creates a new one.
func (os *OIDCServer) getOrCreateProvider(ctx context.Context, provCfg *domain.OIDCProvider, clientSecret string) (*oidc.Provider, error) {
	if cached, ok := os.providerCache.Load(provCfg.ID); ok {
		return cached.(*oidc.Provider), nil
	}

	scopes := strings.Split(provCfg.Scopes, ",")
	for i := range scopes {
		scopes[i] = strings.TrimSpace(scopes[i])
	}
	if len(scopes) == 1 && scopes[0] == "" {
		scopes = nil
	}

	prov, err := oidc.NewProvider(ctx, oidc.ProviderConfig{
		IssuerURL:    provCfg.IssuerURL,
		ClientID:     provCfg.ClientID,
		ClientSecret: clientSecret,
		RedirectURL:  os.callbackURL,
		Scopes:       scopes,
	})
	if err != nil {
		return nil, err
	}

	os.providerCache.Store(provCfg.ID, prov)
	return prov, nil
}

// logOIDCAudit logs an audit event for OIDC operations.
func (os *OIDCServer) logOIDCAudit(ctx context.Context, action, resourceType, resourceID, resourceName string, statusCode int) {
	if os.auditLogger == nil {
		return
	}

	event := &audit.AuditEvent{
		Actor:        resourceName,
		ActorType:    audit.ActorTypeUser,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ResourceName: resourceName,
		StatusCode:   statusCode,
	}

	if reqID := ctx.Value(requestIDContextKey); reqID != nil {
		if id, ok := reqID.(string); ok {
			event.RequestID = id
		}
	}
	_ = os.auditLogger.Log(ctx, event)
}
