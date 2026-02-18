package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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

// RegisterOIDCAdminRoutes registers admin OIDC management routes (stub for Task 10).
func (os *OIDCServer) RegisterOIDCAdminRoutes(_ *slog.Logger) {
	// Admin routes will be implemented in Task 10.
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
