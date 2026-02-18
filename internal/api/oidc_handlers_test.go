package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/auth/oidc"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// testOIDCEnv holds the test dependencies for OIDC handler tests.
type testOIDCEnv struct {
	oidcServer    *OIDCServer
	oidcSrv       *httptest.Server
	privKey       *rsa.PrivateKey
	providerID    string
	encryptionKey []byte
}

// setupOIDCTestEnv creates a test environment with a mock OIDC IdP server,
// an OIDCServer with in-memory stores, and a pre-configured provider.
func setupOIDCTestEnv(t *testing.T) *testOIDCEnv {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	var idpSrv *httptest.Server

	idpMux := http.NewServeMux()
	idpMux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		disc := map[string]interface{}{
			"issuer":                 idpSrv.URL,
			"authorization_endpoint": idpSrv.URL + "/authorize",
			"token_endpoint":         idpSrv.URL + "/token",
			"jwks_uri":               idpSrv.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"subject_types_supported":               []string{"public"},
			"response_types_supported":              []string{"code"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(disc)
	})

	idpMux.HandleFunc("GET /keys", func(w http.ResponseWriter, r *http.Request) {
		jwks := jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{
				{
					Key:       &privKey.PublicKey,
					KeyID:     "test-key-1",
					Algorithm: string(jose.RS256),
					Use:       "sig",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	idpMux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		signerKey := jose.SigningKey{Algorithm: jose.RS256, Key: privKey}
		signerOpts := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key-1")
		signer, err := jose.NewSigner(signerKey, signerOpts)
		if err != nil {
			http.Error(w, fmt.Sprintf("create signer: %v", err), http.StatusInternalServerError)
			return
		}

		now := time.Now()
		claims := jwt.Claims{
			Issuer:    idpSrv.URL,
			Subject:   "user-123",
			Audience:  jwt.Audience{"test-client-id"},
			IssuedAt:  jwt.NewNumericDate(now),
			Expiry:    jwt.NewNumericDate(now.Add(time.Hour)),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
		}
		extraClaims := map[string]interface{}{
			"email":  "alice@example.com",
			"name":   "Alice",
			"groups": []string{"cloudpam-admins", "developers"},
		}

		rawJWT, err := jwt.Signed(signer).Claims(claims).Claims(extraClaims).Serialize()
		if err != nil {
			http.Error(w, fmt.Sprintf("sign jwt: %v", err), http.StatusInternalServerError)
			return
		}

		tokenResponse := map[string]interface{}{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"id_token":     rawJWT,
			"expires_in":   3600,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse)
	})

	idpSrv = httptest.NewServer(idpMux)
	t.Cleanup(idpSrv.Close)

	// Generate encryption key.
	encKey, err := oidc.GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("generate encryption key: %v", err)
	}

	// Encrypt a client secret.
	encSecret, err := oidc.Encrypt("test-client-secret", encKey)
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}

	// Create stores.
	oidcStore := storage.NewMemoryOIDCProviderStore()
	userStore := auth.NewMemoryUserStore()
	sessionStore := auth.NewMemorySessionStore()
	settingsStore := storage.NewMemorySettingsStore()

	providerID := "test-provider-1"
	provider := &domain.OIDCProvider{
		ID:                    providerID,
		Name:                  "Test IdP",
		IssuerURL:             idpSrv.URL,
		ClientID:              "test-client-id",
		ClientSecretEncrypted: encSecret,
		Scopes:                "openid,profile,email",
		RoleMapping:           map[string]string{"cloudpam-admins": "admin"},
		DefaultRole:           "viewer",
		AutoProvision:         true,
		Enabled:               true,
		CreatedAt:             time.Now().UTC(),
		UpdatedAt:             time.Now().UTC(),
	}

	if err := oidcStore.CreateProvider(context.Background(), provider); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// Create Server and OIDCServer.
	mux := http.NewServeMux()
	srv := NewServer(mux, nil, nil, nil, audit.NewMemoryAuditLogger())
	oidcSrv := NewOIDCServer(srv, oidcStore, sessionStore, userStore, settingsStore, encKey, "http://localhost:8080/api/v1/auth/oidc/callback")
	oidcSrv.RegisterOIDCRoutes(nil)

	return &testOIDCEnv{
		oidcServer:    oidcSrv,
		oidcSrv:       idpSrv,
		privKey:       privKey,
		providerID:    providerID,
		encryptionKey: encKey,
	}
}

func TestOIDCLogin_RedirectsToIdP(t *testing.T) {
	env := setupOIDCTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login?provider_id="+env.providerID, nil)
	rec := httptest.NewRecorder()

	env.oidcServer.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rec.Code, rec.Body.String())
	}

	location := rec.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header")
	}

	// Verify the redirect URL points to the mock IdP's authorize endpoint.
	if !strings.Contains(location, env.oidcSrv.URL+"/authorize") {
		t.Errorf("expected redirect to IdP authorize endpoint, got %s", location)
	}

	// Verify required OAuth2 params.
	checks := []string{
		"client_id=test-client-id",
		"response_type=code",
		"state=",
		"nonce=",
	}
	for _, check := range checks {
		if !strings.Contains(location, check) {
			t.Errorf("redirect URL missing %q: %s", check, location)
		}
	}

	// Verify oidc_state cookie was set.
	cookies := rec.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "oidc_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("expected oidc_state cookie")
	}
	if !stateCookie.HttpOnly {
		t.Error("oidc_state cookie should be httponly")
	}
	// Verify state contains provider ID.
	if !strings.HasPrefix(stateCookie.Value, env.providerID+":") {
		t.Errorf("state cookie should start with provider_id, got %s", stateCookie.Value)
	}
}

func TestOIDCLogin_UnknownProvider(t *testing.T) {
	env := setupOIDCTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login?provider_id=nonexistent", nil)
	rec := httptest.NewRecorder()

	env.oidcServer.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp apiError
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "provider not found" {
		t.Errorf("expected 'provider not found', got %q", resp.Error)
	}
}

func TestOIDCLogin_MissingProviderID(t *testing.T) {
	env := setupOIDCTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login", nil)
	rec := httptest.NewRecorder()

	env.oidcServer.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOIDCCallback_NewUser_JITProvision(t *testing.T) {
	env := setupOIDCTestEnv(t)

	// First, do the login to get the state cookie and provider cached.
	loginReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login?provider_id="+env.providerID, nil)
	loginRec := httptest.NewRecorder()
	env.oidcServer.mux.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusFound {
		t.Fatalf("login redirect expected 302, got %d", loginRec.Code)
	}

	// Extract the state cookie.
	var stateCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "oidc_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("missing oidc_state cookie from login")
	}

	// Now simulate the callback with the state and a mock code.
	callbackURL := fmt.Sprintf("/api/v1/auth/oidc/callback?code=mock-auth-code&state=%s", stateCookie.Value)
	callbackReq := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	callbackReq.AddCookie(stateCookie)
	callbackRec := httptest.NewRecorder()

	env.oidcServer.mux.ServeHTTP(callbackRec, callbackReq)

	if callbackRec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d: %s", callbackRec.Code, callbackRec.Body.String())
	}

	// Verify redirect to /.
	location := callbackRec.Header().Get("Location")
	if location != "/" {
		t.Errorf("expected redirect to /, got %s", location)
	}

	// Verify session cookie was set.
	var sessionCookie *http.Cookie
	for _, c := range callbackRec.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}
	if sessionCookie.Value == "" {
		t.Fatal("expected non-empty session cookie value")
	}

	// Verify user was created in the store.
	user, err := env.oidcServer.userStore.GetByOIDCIdentity(context.Background(), env.oidcSrv.URL, "user-123")
	if err != nil {
		t.Fatalf("GetByOIDCIdentity: %v", err)
	}
	if user == nil {
		t.Fatal("expected user to be provisioned")
	}
	if user.Email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %s", user.Email)
	}
	if user.AuthProvider != "oidc" {
		t.Errorf("expected auth_provider=oidc, got %s", user.AuthProvider)
	}
	if user.OIDCSubject != "user-123" {
		t.Errorf("expected oidc_subject=user-123, got %s", user.OIDCSubject)
	}
	// Should be admin because of cloudpam-admins group mapping.
	if user.Role != auth.RoleAdmin {
		t.Errorf("expected role=admin, got %s", user.Role)
	}

	// Verify session exists in store.
	session, err := env.oidcServer.sessionStore.Get(context.Background(), sessionCookie.Value)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session == nil {
		t.Fatal("expected session in store")
	}
	if session.UserID != user.ID {
		t.Errorf("session user_id=%s, expected %s", session.UserID, user.ID)
	}
}

func TestOIDCCallback_ExistingUser_SessionCreated(t *testing.T) {
	env := setupOIDCTestEnv(t)

	// Pre-create the user in the store.
	existingUser := &auth.User{
		ID:           "existing-user-id",
		Username:     "alice@example.com",
		Email:        "alice@example.com",
		Role:         auth.RoleViewer,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		AuthProvider: "oidc",
		OIDCSubject:  "user-123",
		OIDCIssuer:   env.oidcSrv.URL,
	}
	if err := env.oidcServer.userStore.Create(context.Background(), existingUser); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Login to get state.
	loginReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login?provider_id="+env.providerID, nil)
	loginRec := httptest.NewRecorder()
	env.oidcServer.mux.ServeHTTP(loginRec, loginReq)

	var stateCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "oidc_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("missing oidc_state cookie")
	}

	// Callback.
	callbackURL := fmt.Sprintf("/api/v1/auth/oidc/callback?code=mock-auth-code&state=%s", stateCookie.Value)
	callbackReq := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	callbackReq.AddCookie(stateCookie)
	callbackRec := httptest.NewRecorder()

	env.oidcServer.mux.ServeHTTP(callbackRec, callbackReq)

	if callbackRec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", callbackRec.Code, callbackRec.Body.String())
	}

	// Verify session cookie.
	var sessionCookie *http.Cookie
	for _, c := range callbackRec.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}

	// Verify session in store uses the existing user.
	session, err := env.oidcServer.sessionStore.Get(context.Background(), sessionCookie.Value)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.UserID != existingUser.ID {
		t.Errorf("session user_id=%s, expected %s", session.UserID, existingUser.ID)
	}

	// Verify role was synced to admin (from group mapping).
	updatedUser, _ := env.oidcServer.userStore.GetByID(context.Background(), existingUser.ID)
	if updatedUser.Role != auth.RoleAdmin {
		t.Errorf("expected role synced to admin, got %s", updatedUser.Role)
	}
}

func TestOIDCCallback_InvalidState(t *testing.T) {
	env := setupOIDCTestEnv(t)

	// Callback with wrong state cookie.
	callbackReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=mock-code&state=wrong-state", nil)
	callbackReq.AddCookie(&http.Cookie{
		Name:  "oidc_state",
		Value: "different-state",
	})
	callbackRec := httptest.NewRecorder()

	env.oidcServer.mux.ServeHTTP(callbackRec, callbackReq)

	if callbackRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", callbackRec.Code, callbackRec.Body.String())
	}

	var resp apiError
	_ = json.NewDecoder(callbackRec.Body).Decode(&resp)
	if resp.Error != "invalid state" {
		t.Errorf("expected 'invalid state', got %q", resp.Error)
	}
}

func TestOIDCCallback_NoCookie(t *testing.T) {
	env := setupOIDCTestEnv(t)

	// Callback with no state cookie at all.
	callbackReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=mock-code&state=some-state", nil)
	callbackRec := httptest.NewRecorder()

	env.oidcServer.mux.ServeHTTP(callbackRec, callbackReq)

	if callbackRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", callbackRec.Code, callbackRec.Body.String())
	}
}

func TestOIDCCallback_DeactivatedUser_Rejected(t *testing.T) {
	env := setupOIDCTestEnv(t)

	// Pre-create a deactivated user.
	deactivatedUser := &auth.User{
		ID:           "deactivated-user-id",
		Username:     "alice@example.com",
		Email:        "alice@example.com",
		Role:         auth.RoleViewer,
		IsActive:     false, // deactivated
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		AuthProvider: "oidc",
		OIDCSubject:  "user-123",
		OIDCIssuer:   env.oidcSrv.URL,
	}
	if err := env.oidcServer.userStore.Create(context.Background(), deactivatedUser); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Login to get state.
	loginReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login?provider_id="+env.providerID, nil)
	loginRec := httptest.NewRecorder()
	env.oidcServer.mux.ServeHTTP(loginRec, loginReq)

	var stateCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "oidc_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("missing oidc_state cookie")
	}

	// Callback.
	callbackURL := fmt.Sprintf("/api/v1/auth/oidc/callback?code=mock-auth-code&state=%s", stateCookie.Value)
	callbackReq := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	callbackReq.AddCookie(stateCookie)
	callbackRec := httptest.NewRecorder()

	env.oidcServer.mux.ServeHTTP(callbackRec, callbackReq)

	if callbackRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", callbackRec.Code, callbackRec.Body.String())
	}

	var resp apiError
	_ = json.NewDecoder(callbackRec.Body).Decode(&resp)
	if resp.Error != "account disabled" {
		t.Errorf("expected 'account disabled', got %q", resp.Error)
	}
}

func TestListPublicProviders(t *testing.T) {
	env := setupOIDCTestEnv(t)

	// Add a disabled provider too.
	encSecret, _ := oidc.Encrypt("secret2", env.encryptionKey)
	disabledProvider := &domain.OIDCProvider{
		ID:                    "disabled-provider",
		Name:                  "Disabled IdP",
		IssuerURL:             "https://disabled.example.com",
		ClientID:              "client2",
		ClientSecretEncrypted: encSecret,
		Enabled:               false,
		CreatedAt:             time.Now().UTC(),
		UpdatedAt:             time.Now().UTC(),
	}
	if err := env.oidcServer.oidcStore.CreateProvider(context.Background(), disabledProvider); err != nil {
		t.Fatalf("create disabled provider: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/providers", nil)
	rec := httptest.NewRecorder()

	env.oidcServer.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Providers []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Should only have the enabled provider.
	if len(resp.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(resp.Providers))
	}
	if resp.Providers[0].ID != env.providerID {
		t.Errorf("expected provider ID %s, got %s", env.providerID, resp.Providers[0].ID)
	}
	if resp.Providers[0].Name != "Test IdP" {
		t.Errorf("expected provider name 'Test IdP', got %s", resp.Providers[0].Name)
	}

	// Verify no secrets or config details leaked.
	raw := rec.Body.String()
	if strings.Contains(raw, "client_id") || strings.Contains(raw, "issuer_url") || strings.Contains(raw, "client_secret") {
		t.Error("response should not contain client_id, issuer_url, or client_secret")
	}
}

func TestOIDCCallback_AutoProvisionDisabled(t *testing.T) {
	env := setupOIDCTestEnv(t)

	// Disable auto-provisioning on the provider.
	provCfg, _ := env.oidcServer.oidcStore.GetProvider(context.Background(), env.providerID)
	provCfg.AutoProvision = false
	_ = env.oidcServer.oidcStore.UpdateProvider(context.Background(), provCfg)

	// Login to get state.
	loginReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login?provider_id="+env.providerID, nil)
	loginRec := httptest.NewRecorder()
	env.oidcServer.mux.ServeHTTP(loginRec, loginReq)

	var stateCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "oidc_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("missing oidc_state cookie")
	}

	// Callback — no existing user, auto-provisioning disabled.
	callbackURL := fmt.Sprintf("/api/v1/auth/oidc/callback?code=mock-auth-code&state=%s", stateCookie.Value)
	callbackReq := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	callbackReq.AddCookie(stateCookie)
	callbackRec := httptest.NewRecorder()

	env.oidcServer.mux.ServeHTTP(callbackRec, callbackReq)

	if callbackRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", callbackRec.Code, callbackRec.Body.String())
	}

	var resp apiError
	_ = json.NewDecoder(callbackRec.Body).Decode(&resp)
	if resp.Error != "user not provisioned" {
		t.Errorf("expected 'user not provisioned', got %q", resp.Error)
	}
}
