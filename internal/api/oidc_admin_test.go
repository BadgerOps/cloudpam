package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/auth/oidc"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// setupOIDCAdminTestEnv creates a test environment for OIDC admin handler tests.
// It returns an OIDCServer with in-memory stores and admin routes registered without auth middleware.
func setupOIDCAdminTestEnv(t *testing.T) *OIDCServer {
	t.Helper()

	encKey, err := oidc.GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("generate encryption key: %v", err)
	}

	oidcStore := storage.NewMemoryOIDCProviderStore()
	userStore := auth.NewMemoryUserStore()
	sessionStore := auth.NewMemorySessionStore()
	settingsStore := storage.NewMemorySettingsStore()

	mux := http.NewServeMux()
	srv := NewServer(mux, nil, nil, nil, audit.NewMemoryAuditLogger())
	oidcSrv := NewOIDCServer(srv, oidcStore, sessionStore, userStore, settingsStore, encKey, "http://localhost:8080/api/v1/auth/oidc/callback")
	oidcSrv.RegisterOIDCAdminRoutesNoAuth()

	return oidcSrv
}

// createTestProviderBody returns JSON bytes for creating a provider.
func createTestProviderBody(t *testing.T, name, issuerURL, clientID, clientSecret string) []byte {
	t.Helper()
	body := map[string]interface{}{
		"name":           name,
		"issuer_url":     issuerURL,
		"client_id":      clientID,
		"client_secret":  clientSecret,
		"scopes":         "openid,profile,email",
		"role_mapping":   map[string]string{"admins": "admin"},
		"default_role":   "viewer",
		"auto_provision": true,
		"enabled":        true,
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return b
}

func TestOIDCAdmin_CreateProvider(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	body := createTestProviderBody(t, "Test IdP", "https://idp.example.com", "client-1", "super-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	os.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var provider domain.OIDCProvider
	if err := json.NewDecoder(rec.Body).Decode(&provider); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if provider.ID == "" {
		t.Error("expected non-empty provider ID")
	}
	if provider.Name != "Test IdP" {
		t.Errorf("expected name 'Test IdP', got %q", provider.Name)
	}
	if provider.IssuerURL != "https://idp.example.com" {
		t.Errorf("expected issuer_url 'https://idp.example.com', got %q", provider.IssuerURL)
	}
	if provider.ClientSecretMasked != "****" {
		t.Errorf("expected masked secret '****', got %q", provider.ClientSecretMasked)
	}
	if provider.DefaultRole != "viewer" {
		t.Errorf("expected default_role 'viewer', got %q", provider.DefaultRole)
	}
	if !provider.AutoProvision {
		t.Error("expected auto_provision=true")
	}
	if !provider.Enabled {
		t.Error("expected enabled=true")
	}

	// Verify it was actually stored.
	stored, err := os.oidcStore.GetProvider(context.Background(), provider.ID)
	if err != nil {
		t.Fatalf("get stored provider: %v", err)
	}
	if stored.ClientSecretEncrypted == "" {
		t.Error("expected encrypted secret to be stored")
	}
	// Verify we can decrypt the stored secret.
	decrypted, err := oidc.Decrypt(stored.ClientSecretEncrypted, os.encryptionKey)
	if err != nil {
		t.Fatalf("decrypt stored secret: %v", err)
	}
	if decrypted != "super-secret" {
		t.Errorf("expected decrypted secret 'super-secret', got %q", decrypted)
	}
}

func TestOIDCAdmin_CreateProvider_MissingFields(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	tests := []struct {
		name     string
		body     map[string]interface{}
		wantErr  string
	}{
		{
			name:    "missing name",
			body:    map[string]interface{}{"issuer_url": "https://x.com", "client_id": "c", "client_secret": "s"},
			wantErr: "name is required",
		},
		{
			name:    "missing issuer_url",
			body:    map[string]interface{}{"name": "n", "client_id": "c", "client_secret": "s"},
			wantErr: "issuer_url is required",
		},
		{
			name:    "missing client_id",
			body:    map[string]interface{}{"name": "n", "issuer_url": "https://x.com", "client_secret": "s"},
			wantErr: "client_id is required",
		},
		{
			name:    "missing client_secret",
			body:    map[string]interface{}{"name": "n", "issuer_url": "https://x.com", "client_id": "c"},
			wantErr: "client_secret is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", bytes.NewReader(b))
			rec := httptest.NewRecorder()
			os.mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			var resp apiError
			_ = json.NewDecoder(rec.Body).Decode(&resp)
			if resp.Error != tt.wantErr {
				t.Errorf("expected error %q, got %q", tt.wantErr, resp.Error)
			}
		})
	}
}

func TestOIDCAdmin_CreateProvider_DuplicateIssuer(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	body := createTestProviderBody(t, "First IdP", "https://idp.example.com", "client-1", "secret-1")

	// Create first provider.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	os.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Create second provider with same issuer URL.
	body2 := createTestProviderBody(t, "Second IdP", "https://idp.example.com", "client-2", "secret-2")
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	os.mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var resp apiError
	_ = json.NewDecoder(rec2.Body).Decode(&resp)
	if resp.Error != "provider with this issuer URL already exists" {
		t.Errorf("expected duplicate issuer error, got %q", resp.Error)
	}
}

func TestOIDCAdmin_GetProvider_SecretMasked(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	// Create a provider first.
	body := createTestProviderBody(t, "My IdP", "https://get.example.com", "client-get", "my-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	os.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", rec.Code)
	}

	var created domain.OIDCProvider
	_ = json.NewDecoder(rec.Body).Decode(&created)

	// GET the provider.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings/oidc/providers/"+created.ID, nil)
	getRec := httptest.NewRecorder()
	os.mux.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	var provider domain.OIDCProvider
	_ = json.NewDecoder(getRec.Body).Decode(&provider)

	if provider.ClientSecretMasked != "****" {
		t.Errorf("expected masked secret '****', got %q", provider.ClientSecretMasked)
	}
	if provider.Name != "My IdP" {
		t.Errorf("expected name 'My IdP', got %q", provider.Name)
	}

	// Make sure the raw response does not contain the encrypted secret.
	rawBody := getRec.Body.String()
	stored, _ := os.oidcStore.GetProvider(context.Background(), created.ID)
	if stored.ClientSecretEncrypted != "" {
		// The json:"-" tag should prevent it from appearing.
		if bytes.Contains([]byte(rawBody), []byte(stored.ClientSecretEncrypted)) {
			t.Error("response should not contain encrypted secret")
		}
	}
}

func TestOIDCAdmin_GetProvider_NotFound(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/oidc/providers/nonexistent", nil)
	rec := httptest.NewRecorder()
	os.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOIDCAdmin_UpdateProvider(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	// Create a provider.
	body := createTestProviderBody(t, "Original Name", "https://update.example.com", "client-upd", "original-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	os.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", rec.Code)
	}

	var created domain.OIDCProvider
	_ = json.NewDecoder(rec.Body).Decode(&created)

	// PATCH: update name and scopes only.
	patchBody, _ := json.Marshal(map[string]interface{}{
		"name":   "Updated Name",
		"scopes": "openid,email",
	})
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/oidc/providers/"+created.ID, bytes.NewReader(patchBody))
	patchRec := httptest.NewRecorder()
	os.mux.ServeHTTP(patchRec, patchReq)

	if patchRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", patchRec.Code, patchRec.Body.String())
	}

	var updated domain.OIDCProvider
	_ = json.NewDecoder(patchRec.Body).Decode(&updated)

	if updated.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %q", updated.Name)
	}
	if updated.Scopes != "openid,email" {
		t.Errorf("expected scopes 'openid,email', got %q", updated.Scopes)
	}
	// IssuerURL should be unchanged.
	if updated.IssuerURL != "https://update.example.com" {
		t.Errorf("issuer_url should be unchanged, got %q", updated.IssuerURL)
	}
	if updated.ClientSecretMasked != "****" {
		t.Errorf("expected masked secret, got %q", updated.ClientSecretMasked)
	}

	// Verify the original secret is still valid (wasn't changed).
	stored, _ := os.oidcStore.GetProvider(context.Background(), created.ID)
	decrypted, err := oidc.Decrypt(stored.ClientSecretEncrypted, os.encryptionKey)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != "original-secret" {
		t.Errorf("expected secret unchanged, got %q", decrypted)
	}
}

func TestOIDCAdmin_UpdateProvider_ChangeSecret(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	// Create a provider.
	body := createTestProviderBody(t, "Secret Test", "https://secret.example.com", "client-sec", "old-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	os.mux.ServeHTTP(rec, req)

	var created domain.OIDCProvider
	_ = json.NewDecoder(rec.Body).Decode(&created)

	// PATCH: change the secret.
	patchBody, _ := json.Marshal(map[string]interface{}{
		"client_secret": "new-secret",
	})
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/oidc/providers/"+created.ID, bytes.NewReader(patchBody))
	patchRec := httptest.NewRecorder()
	os.mux.ServeHTTP(patchRec, patchReq)

	if patchRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", patchRec.Code, patchRec.Body.String())
	}

	// Verify the new secret.
	stored, _ := os.oidcStore.GetProvider(context.Background(), created.ID)
	decrypted, err := oidc.Decrypt(stored.ClientSecretEncrypted, os.encryptionKey)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != "new-secret" {
		t.Errorf("expected 'new-secret', got %q", decrypted)
	}
}

func TestOIDCAdmin_UpdateProvider_NotFound(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	patchBody, _ := json.Marshal(map[string]interface{}{"name": "whatever"})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/oidc/providers/nonexistent", bytes.NewReader(patchBody))
	rec := httptest.NewRecorder()
	os.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOIDCAdmin_DeleteProvider(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	// Create a provider.
	body := createTestProviderBody(t, "Delete Me", "https://delete.example.com", "client-del", "del-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	os.mux.ServeHTTP(rec, req)

	var created domain.OIDCProvider
	_ = json.NewDecoder(rec.Body).Decode(&created)

	// DELETE the provider.
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/settings/oidc/providers/"+created.ID, nil)
	delRec := httptest.NewRecorder()
	os.mux.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", delRec.Code, delRec.Body.String())
	}

	// Verify it's gone.
	_, err := os.oidcStore.GetProvider(context.Background(), created.ID)
	if err == nil {
		t.Error("expected error after deletion, got nil")
	}
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestOIDCAdmin_DeleteProvider_NotFound(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/settings/oidc/providers/nonexistent", nil)
	rec := httptest.NewRecorder()
	os.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOIDCAdmin_ListProviders(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	// Create two providers.
	body1 := createTestProviderBody(t, "Provider A", "https://a.example.com", "client-a", "secret-a")
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", bytes.NewReader(body1))
	rec1 := httptest.NewRecorder()
	os.mux.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("create A: expected 201, got %d", rec1.Code)
	}

	body2 := createTestProviderBody(t, "Provider B", "https://b.example.com", "client-b", "secret-b")
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	os.mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("create B: expected 201, got %d", rec2.Code)
	}

	// List all providers.
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings/oidc/providers", nil)
	listRec := httptest.NewRecorder()
	os.mux.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	var resp struct {
		Providers []domain.OIDCProvider `json:"providers"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(resp.Providers))
	}

	// All secrets should be masked.
	for _, p := range resp.Providers {
		if p.ClientSecretMasked != "****" {
			t.Errorf("provider %s: expected masked secret, got %q", p.ID, p.ClientSecretMasked)
		}
	}

	// Verify names present.
	names := map[string]bool{}
	for _, p := range resp.Providers {
		names[p.Name] = true
	}
	if !names["Provider A"] || !names["Provider B"] {
		t.Errorf("expected both providers in list, got %v", names)
	}
}

func TestOIDCAdmin_TestConnection(t *testing.T) {
	// For the test connection handler, we need a real mock OIDC IdP server.
	env := setupOIDCTestEnv(t)

	// Register admin routes (no auth) on the same mux.
	env.oidcServer.RegisterOIDCAdminRoutesNoAuth()

	// The test env already has a provider that points to the mock IdP.
	// POST /api/v1/settings/oidc/providers/{id}/test
	testReq := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers/"+env.providerID+"/test", nil)
	testRec := httptest.NewRecorder()
	env.oidcServer.mux.ServeHTTP(testRec, testReq)

	if testRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", testRec.Code, testRec.Body.String())
	}

	var result struct {
		Success   bool   `json:"success"`
		IssuerURL string `json:"issuer_url"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(testRec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success=true, got false: %s", result.Message)
	}
	if result.IssuerURL != env.oidcSrv.URL {
		t.Errorf("expected issuer_url=%s, got %s", env.oidcSrv.URL, result.IssuerURL)
	}
}

func TestOIDCAdmin_TestConnection_BadIssuer(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	// Create a provider with a bad issuer URL.
	body := createTestProviderBody(t, "Bad IdP", "https://nonexistent.invalid", "client-bad", "secret-bad")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	os.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", rec.Code)
	}

	var created domain.OIDCProvider
	_ = json.NewDecoder(rec.Body).Decode(&created)

	// Test connection — should fail but return 200 with success=false.
	testReq := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers/"+created.ID+"/test", nil)
	testRec := httptest.NewRecorder()
	os.mux.ServeHTTP(testRec, testReq)

	if testRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", testRec.Code, testRec.Body.String())
	}

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(testRec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result.Success {
		t.Error("expected success=false for bad issuer")
	}
	if result.Message == "" {
		t.Error("expected non-empty error message")
	}
}

func TestOIDCAdmin_TestConnection_NotFound(t *testing.T) {
	os := setupOIDCAdminTestEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers/nonexistent/test", nil)
	rec := httptest.NewRecorder()
	os.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
