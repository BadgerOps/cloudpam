package http

import (
	"encoding/json"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

// setupProtectedTestServer creates a Server that uses RegisterProtectedRoutes with a real key store.
// Returns the mux, store, keyStore, and a valid API key plaintext for requests.
func setupProtectedTestServer(t *testing.T) (*stdhttp.ServeMux, storage.Store, string) {
	t.Helper()
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, st, logger, nil, nil)

	keyStore := auth.NewMemoryKeyStore()
	slogger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	// Create admin API key
	plaintext, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
		Name:   "admin-test",
		Scopes: []string{"*"},
	})
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if err := keyStore.Create(t.Context(), apiKey); err != nil {
		t.Fatalf("store api key: %v", err)
	}

	srv.RegisterProtectedRoutes(keyStore, auth.NewMemorySessionStore(), auth.NewMemoryUserStore(), slogger)
	return mux, st, plaintext
}

// doProtected sends a request with an Authorization header.
func doProtected(t *testing.T, mux *stdhttp.ServeMux, method, path, body, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// --- Protected Pool Handler Tests ---

func TestProtectedPools_ListRequiresAuth(t *testing.T) {
	mux, _, _ := setupProtectedTestServer(t)

	// No auth header → 401
	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools", "", "")
	if rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPools_ListWithAuth(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPools_CreateWithAuth(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	body := `{"name":"test-pool","cidr":"10.0.0.0/8"}`
	rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/pools", body, key)
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPools_MethodNotAllowed(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodPut, "/api/v1/pools", "", key)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Protected Pool Subroutes Tests ---

func TestProtectedPoolSubroutes_GetPool(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	p, _ := st.CreatePool(t.Context(), domain.CreatePool{Name: "net", CIDR: "10.0.0.0/8"})

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools/"+itoa(p.ID), "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_DeletePool(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	p, _ := st.CreatePool(t.Context(), domain.CreatePool{Name: "net", CIDR: "10.0.0.0/8"})

	rr := doProtected(t, mux, stdhttp.MethodDelete, "/api/v1/pools/"+itoa(p.ID), "", key)
	if rr.Code != stdhttp.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_Hierarchy(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	p, _ := st.CreatePool(t.Context(), domain.CreatePool{Name: "net", CIDR: "10.0.0.0/8"})
	_ = p

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools/hierarchy", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_HierarchyMethodNotAllowed(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/pools/hierarchy", "", key)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_Stats(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	p, _ := st.CreatePool(t.Context(), domain.CreatePool{Name: "net", CIDR: "10.0.0.0/8"})

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools/"+itoa(p.ID)+"/stats", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_Blocks(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	p, _ := st.CreatePool(t.Context(), domain.CreatePool{Name: "net", CIDR: "10.0.0.0/8"})

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools/"+itoa(p.ID)+"/blocks?new_prefix_len=16", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_InvalidID(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools/abc", "", key)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_PatchPool(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	p, _ := st.CreatePool(t.Context(), domain.CreatePool{Name: "net", CIDR: "10.0.0.0/8"})

	rr := doProtected(t, mux, stdhttp.MethodPatch, "/api/v1/pools/"+itoa(p.ID), `{"name":"updated"}`, key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_ForceDelete(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	parent, _ := st.CreatePool(t.Context(), domain.CreatePool{Name: "net", CIDR: "10.0.0.0/8"})
	if _, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "sub", CIDR: "10.0.0.0/16", ParentID: &parent.ID}); err != nil {
		t.Fatal(err)
	}

	rr := doProtected(t, mux, stdhttp.MethodDelete, "/api/v1/pools/"+itoa(parent.ID)+"?force=true", "", key)
	if rr.Code != stdhttp.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_EmptyPath(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	// Request to /api/v1/pools/ with trailing slash but no ID
	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools/", "", key)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_MethodNotAllowed(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	p, _ := st.CreatePool(t.Context(), domain.CreatePool{Name: "net", CIDR: "10.0.0.0/8"})

	rr := doProtected(t, mux, stdhttp.MethodPut, "/api/v1/pools/"+itoa(p.ID), "", key)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Protected Account Handler Tests ---

func TestProtectedAccounts_ListWithAuth(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/accounts", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAccounts_CreateWithAuth(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	body := `{"key":"aws:111111111111","name":"Prod"}`
	rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/accounts", body, key)
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAccounts_MethodNotAllowed(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodPut, "/api/v1/accounts", "", key)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Protected Account Subroutes Tests ---

func TestProtectedAccountSubroutes_Get(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	a, _ := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:111111111111", Name: "Prod"})

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/accounts/"+itoa(a.ID), "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAccountSubroutes_Delete(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	a, _ := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:111111111111", Name: "Prod"})

	rr := doProtected(t, mux, stdhttp.MethodDelete, "/api/v1/accounts/"+itoa(a.ID), "", key)
	if rr.Code != stdhttp.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAccountSubroutes_Patch(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	a, _ := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:111111111111", Name: "Prod"})

	rr := doProtected(t, mux, stdhttp.MethodPatch, "/api/v1/accounts/"+itoa(a.ID), `{"name":"Updated"}`, key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAccountSubroutes_EmptyID(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/accounts/", "", key)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAccountSubroutes_InvalidID(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/accounts/abc", "", key)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAccountSubroutes_MethodNotAllowed(t *testing.T) {
	mux, st, key := setupProtectedTestServer(t)

	a, _ := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:111111111111", Name: "Prod"})

	rr := doProtected(t, mux, stdhttp.MethodPut, "/api/v1/accounts/"+itoa(a.ID), "", key)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAccountSubroutes_NotFound(t *testing.T) {
	mux, _, key := setupProtectedTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/accounts/999", "", key)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Viewer role (limited permissions) Tests ---

func setupViewerProtectedTestServer(t *testing.T) (*stdhttp.ServeMux, storage.Store, string) {
	t.Helper()
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, st, logger, nil, nil)

	keyStore := auth.NewMemoryKeyStore()
	slogger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	// Create viewer API key (read-only scopes)
	plaintext, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
		Name:   "viewer-test",
		Scopes: []string{"pools:read", "accounts:read"},
	})
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if err := keyStore.Create(t.Context(), apiKey); err != nil {
		t.Fatalf("store api key: %v", err)
	}

	srv.RegisterProtectedRoutes(keyStore, auth.NewMemorySessionStore(), auth.NewMemoryUserStore(), slogger)
	return mux, st, plaintext
}

func TestProtectedPools_ViewerCannotCreate(t *testing.T) {
	mux, _, key := setupViewerProtectedTestServer(t)

	body := `{"name":"test","cidr":"10.0.0.0/8"}`
	rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/pools", body, key)
	if rr.Code != stdhttp.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPools_ViewerCanList(t *testing.T) {
	mux, _, key := setupViewerProtectedTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAccounts_ViewerCannotCreate(t *testing.T) {
	mux, _, key := setupViewerProtectedTestServer(t)

	body := `{"key":"aws:111111111111","name":"Prod"}`
	rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/accounts", body, key)
	if rr.Code != stdhttp.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_ViewerCannotDelete(t *testing.T) {
	mux, st, key := setupViewerProtectedTestServer(t)

	p, _ := st.CreatePool(t.Context(), domain.CreatePool{Name: "net", CIDR: "10.0.0.0/8"})

	rr := doProtected(t, mux, stdhttp.MethodDelete, "/api/v1/pools/"+itoa(p.ID), "", key)
	if rr.Code != stdhttp.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedPoolSubroutes_ViewerCannotPatch(t *testing.T) {
	mux, st, key := setupViewerProtectedTestServer(t)

	p, _ := st.CreatePool(t.Context(), domain.CreatePool{Name: "net", CIDR: "10.0.0.0/8"})

	rr := doProtected(t, mux, stdhttp.MethodPatch, "/api/v1/pools/"+itoa(p.ID), `{"name":"x"}`, key)
	if rr.Code != stdhttp.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- AuthServer protected routes tests ---

func setupProtectedAuthTestServer(t *testing.T) (*stdhttp.ServeMux, *auth.MemoryKeyStore, string) {
	t.Helper()
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	auditLogger := audit.NewMemoryAuditLogger()
	srv := NewServer(mux, st, logger, nil, auditLogger)

	keyStore := auth.NewMemoryKeyStore()
	slogger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	// Create admin API key
	plaintext, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
		Name:   "admin-test",
		Scopes: []string{"*"},
	})
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if err := keyStore.Create(t.Context(), apiKey); err != nil {
		t.Fatalf("store api key: %v", err)
	}

	authSrv := NewAuthServer(srv, keyStore, auditLogger)
	authSrv.RegisterProtectedAuthRoutes(slogger)
	// Also register public routes for health etc
	srv.mux.HandleFunc("/healthz", srv.handleHealth)

	return mux, keyStore, plaintext
}

func TestProtectedAuth_CreateKey(t *testing.T) {
	mux, _, key := setupProtectedAuthTestServer(t)

	body := `{"name":"New Key","scopes":["pools:read"]}`
	rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/auth/keys", body, key)
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAuth_ListKeys(t *testing.T) {
	mux, _, key := setupProtectedAuthTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/auth/keys", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAuth_MethodNotAllowed(t *testing.T) {
	mux, _, key := setupProtectedAuthTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodPut, "/api/v1/auth/keys", "", key)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAuth_RevokeKey(t *testing.T) {
	mux, _, key := setupProtectedAuthTestServer(t)

	// Create a key to revoke
	body := `{"name":"Revokable"}`
	rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/auth/keys", body, key)
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct{ ID string }
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	rr = doProtected(t, mux, stdhttp.MethodDelete, "/api/v1/auth/keys/"+resp.ID, "", key)
	if rr.Code != stdhttp.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAuth_RevokeEmptyID(t *testing.T) {
	mux, _, key := setupProtectedAuthTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodDelete, "/api/v1/auth/keys/", "", key)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAuth_KeyByIDMethodNotAllowed(t *testing.T) {
	mux, _, key := setupProtectedAuthTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/auth/keys/some-id", "", key)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- AuthServer.handleAuditList + parseInt tests ---

func TestProtectedAuth_AuditList(t *testing.T) {
	mux, _, key := setupProtectedAuthTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/audit", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Events []any `json:"events"`
		Total  int   `json:"total"`
		Limit  int   `json:"limit"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Limit != 50 {
		t.Errorf("expected default limit 50, got %d", resp.Limit)
	}
}

func TestProtectedAuth_AuditListPagination(t *testing.T) {
	mux, _, key := setupProtectedAuthTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/audit?limit=10&offset=5", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Limit != 10 {
		t.Errorf("expected limit 10, got %d", resp.Limit)
	}
	if resp.Offset != 5 {
		t.Errorf("expected offset 5, got %d", resp.Offset)
	}
}

func TestProtectedAuth_AuditListMethodNotAllowed(t *testing.T) {
	mux, _, key := setupProtectedAuthTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/audit", "", key)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedAuth_AuditListMaxLimit(t *testing.T) {
	mux, _, key := setupProtectedAuthTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/audit?limit=5000", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Limit != 1000 {
		t.Errorf("expected limit capped at 1000, got %d", resp.Limit)
	}
}

func TestProtectedAuth_AuditListTimeFilters(t *testing.T) {
	mux, _, key := setupProtectedAuthTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/audit?since=2024-01-01T00:00:00Z&until=2024-12-31T23:59:59Z", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- parseInt tests ---

func TestParseInt(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"42", 42, false},
		{"0", 0, false},
		{"100", 100, false},
		{"abc", 0, true},
		{"12x", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseInt(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInt(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseInt(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// --- RegisterProtectedRoutes tests ---

func TestRegisterProtectedRoutes_NilLogger(t *testing.T) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, st, logger, nil, nil)

	keyStore := auth.NewMemoryKeyStore()
	// Pass nil logger — should use slog.Default() and not panic
	srv.RegisterProtectedRoutes(keyStore, auth.NewMemorySessionStore(), auth.NewMemoryUserStore(), nil)

	// Verify health endpoint still works (public route)
	req := httptest.NewRequest(stdhttp.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// itoa is already defined in integration_test.go (same package)
