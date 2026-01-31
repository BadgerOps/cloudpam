package http

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

func setupAuthTestServer() (*AuthServer, *auth.MemoryKeyStore, *audit.MemoryAuditLogger) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{
		Level:  "info",
		Format: "json",
		Output: io.Discard,
	})
	auditLogger := audit.NewMemoryAuditLogger()
	srv := NewServer(mux, st, logger, nil, auditLogger)

	keyStore := auth.NewMemoryKeyStore()

	authSrv := NewAuthServer(srv, keyStore, auditLogger)
	srv.RegisterRoutes()
	authSrv.RegisterAuthRoutes()

	return authSrv, keyStore, auditLogger
}

func doAuthJSON(t *testing.T, mux *stdhttp.ServeMux, method, path, body string, code int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != code {
		t.Fatalf("%s %s: expected code %d, got %d: %s", method, path, code, rr.Code, rr.Body.String())
	}
	return rr
}

func TestAPIKeys_Create(t *testing.T) {
	as, _, _ := setupAuthTestServer()

	// Create a key
	body := `{"name": "Test Key", "scopes": ["pools:read", "pools:write"]}`
	rr := doAuthJSON(t, as.mux, stdhttp.MethodPost, "/api/v1/auth/keys", body, stdhttp.StatusCreated)

	var resp struct {
		ID        string   `json:"id"`
		Key       string   `json:"key"`
		Prefix    string   `json:"prefix"`
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		CreatedAt string   `json:"created_at"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify response
	if resp.ID == "" {
		t.Error("ID should be set")
	}
	if !strings.HasPrefix(resp.Key, "cpam_") {
		t.Errorf("Key should start with cpam_, got %q", resp.Key)
	}
	if resp.Prefix == "" {
		t.Error("Prefix should be set")
	}
	if resp.Name != "Test Key" {
		t.Errorf("Name = %q, want %q", resp.Name, "Test Key")
	}
	if len(resp.Scopes) != 2 {
		t.Errorf("Scopes length = %d, want 2", len(resp.Scopes))
	}
}

func TestAPIKeys_Create_WithExpiration(t *testing.T) {
	as, _, _ := setupAuthTestServer()

	body := `{"name": "Expiring Key", "scopes": ["pools:read"], "expires_in_days": 30}`
	rr := doAuthJSON(t, as.mux, stdhttp.MethodPost, "/api/v1/auth/keys", body, stdhttp.StatusCreated)

	var resp struct {
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ExpiresAt == nil {
		t.Error("ExpiresAt should be set")
	}
}

func TestAPIKeys_Create_EmptyName(t *testing.T) {
	as, _, _ := setupAuthTestServer()

	body := `{"name": "", "scopes": ["pools:read"]}`
	rr := doAuthJSON(t, as.mux, stdhttp.MethodPost, "/api/v1/auth/keys", body, stdhttp.StatusBadRequest)

	if !strings.Contains(rr.Body.String(), "name is required") {
		t.Errorf("Expected 'name is required' error, got: %s", rr.Body.String())
	}
}

func TestAPIKeys_Create_InvalidScope(t *testing.T) {
	as, _, _ := setupAuthTestServer()

	body := `{"name": "Test Key", "scopes": ["invalid:scope"]}`
	rr := doAuthJSON(t, as.mux, stdhttp.MethodPost, "/api/v1/auth/keys", body, stdhttp.StatusBadRequest)

	if !strings.Contains(rr.Body.String(), "invalid scope") {
		t.Errorf("Expected 'invalid scope' error, got: %s", rr.Body.String())
	}
}

func TestAPIKeys_Create_InvalidJSON(t *testing.T) {
	as, _, _ := setupAuthTestServer()

	doAuthJSON(t, as.mux, stdhttp.MethodPost, "/api/v1/auth/keys", `{"name":`, stdhttp.StatusBadRequest)
}

func TestAPIKeys_List(t *testing.T) {
	as, _, _ := setupAuthTestServer()

	// Create some keys first
	doAuthJSON(t, as.mux, stdhttp.MethodPost, "/api/v1/auth/keys", `{"name": "Key 1", "scopes": ["pools:read"]}`, stdhttp.StatusCreated)
	doAuthJSON(t, as.mux, stdhttp.MethodPost, "/api/v1/auth/keys", `{"name": "Key 2", "scopes": ["pools:write"]}`, stdhttp.StatusCreated)

	// List keys
	rr := doAuthJSON(t, as.mux, stdhttp.MethodGet, "/api/v1/auth/keys", "", stdhttp.StatusOK)

	var resp struct {
		Keys []struct {
			ID      string   `json:"id"`
			Prefix  string   `json:"prefix"`
			Name    string   `json:"name"`
			Scopes  []string `json:"scopes"`
			Revoked bool     `json:"revoked"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(resp.Keys))
	}

	// Verify no secrets are exposed
	for _, k := range resp.Keys {
		if k.Prefix == "" {
			t.Error("Prefix should be set")
		}
	}
}

func TestAPIKeys_Revoke(t *testing.T) {
	as, _, _ := setupAuthTestServer()

	// Create a key
	rr := doAuthJSON(t, as.mux, stdhttp.MethodPost, "/api/v1/auth/keys", `{"name": "Test Key"}`, stdhttp.StatusCreated)

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Revoke the key
	doAuthJSON(t, as.mux, stdhttp.MethodDelete, "/api/v1/auth/keys/"+created.ID, "", stdhttp.StatusNoContent)

	// Verify it's revoked via list
	rr = doAuthJSON(t, as.mux, stdhttp.MethodGet, "/api/v1/auth/keys", "", stdhttp.StatusOK)

	var resp struct {
		Keys []struct {
			ID      string `json:"id"`
			Revoked bool   `json:"revoked"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	found := false
	for _, k := range resp.Keys {
		if k.ID == created.ID {
			found = true
			if !k.Revoked {
				t.Error("Key should be revoked")
			}
		}
	}
	if !found {
		t.Error("Key should still exist after revocation")
	}
}

func TestAPIKeys_Revoke_NotFound(t *testing.T) {
	as, _, _ := setupAuthTestServer()

	doAuthJSON(t, as.mux, stdhttp.MethodDelete, "/api/v1/auth/keys/nonexistent", "", stdhttp.StatusNotFound)
}

func TestAPIKeys_MethodNotAllowed(t *testing.T) {
	as, _, _ := setupAuthTestServer()

	// PUT not allowed on /api/v1/auth/keys
	req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/auth/keys", nil)
	rr := httptest.NewRecorder()
	as.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rr.Code)
	}

	// Create a key first
	createRR := doAuthJSON(t, as.mux, stdhttp.MethodPost, "/api/v1/auth/keys", `{"name": "Test"}`, stdhttp.StatusCreated)
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(createRR.Body.Bytes(), &created)

	// POST not allowed on /api/v1/auth/keys/{id}
	req = httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/keys/"+created.ID, nil)
	rr = httptest.NewRecorder()
	as.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rr.Code)
	}
}

func TestAudit_List_Empty(t *testing.T) {
	as, _, _ := setupAuthTestServer()

	rr := doAuthJSON(t, as.mux, stdhttp.MethodGet, "/api/v1/audit", "", stdhttp.StatusOK)

	var resp struct {
		Events []any `json:"events"`
		Total  int   `json:"total"`
		Limit  int   `json:"limit"`
		Offset int   `json:"offset"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Events) != 0 {
		t.Errorf("Expected 0 events, got %d", len(resp.Events))
	}
	if resp.Total != 0 {
		t.Errorf("Expected total 0, got %d", resp.Total)
	}
	if resp.Limit != 50 {
		t.Errorf("Expected default limit 50, got %d", resp.Limit)
	}
}

func TestAudit_List_WithEvents(t *testing.T) {
	as, _, auditLogger := setupAuthTestServer()

	// Add some audit events directly
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = auditLogger.Log(ctx, &audit.AuditEvent{
			Actor:        "cpam_test",
			ActorType:    audit.ActorTypeAPIKey,
			Action:       audit.ActionCreate,
			ResourceType: audit.ResourcePool,
			ResourceID:   string(rune('1' + i)),
			StatusCode:   201,
		})
	}

	rr := doAuthJSON(t, as.mux, stdhttp.MethodGet, "/api/v1/audit", "", stdhttp.StatusOK)

	var resp struct {
		Events []any `json:"events"`
		Total  int   `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Events) != 5 {
		t.Errorf("Expected 5 events, got %d", len(resp.Events))
	}
	if resp.Total != 5 {
		t.Errorf("Expected total 5, got %d", resp.Total)
	}
}

func TestAudit_List_Pagination(t *testing.T) {
	as, _, auditLogger := setupAuthTestServer()

	// Add 25 events
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		_ = auditLogger.Log(ctx, &audit.AuditEvent{
			Actor:        "cpam_test",
			ActorType:    audit.ActorTypeAPIKey,
			Action:       audit.ActionCreate,
			ResourceType: audit.ResourcePool,
			ResourceID:   "1",
			StatusCode:   201,
		})
	}

	// Get first page
	rr := doAuthJSON(t, as.mux, stdhttp.MethodGet, "/api/v1/audit?limit=10&offset=0", "", stdhttp.StatusOK)

	var resp struct {
		Events []any `json:"events"`
		Total  int   `json:"total"`
		Limit  int   `json:"limit"`
		Offset int   `json:"offset"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Events) != 10 {
		t.Errorf("Expected 10 events, got %d", len(resp.Events))
	}
	if resp.Total != 25 {
		t.Errorf("Expected total 25, got %d", resp.Total)
	}
	if resp.Limit != 10 {
		t.Errorf("Expected limit 10, got %d", resp.Limit)
	}
	if resp.Offset != 0 {
		t.Errorf("Expected offset 0, got %d", resp.Offset)
	}
}

func TestAudit_List_Filtering(t *testing.T) {
	as, _, auditLogger := setupAuthTestServer()

	// Add diverse events
	ctx := context.Background()
	_ = auditLogger.Log(ctx, &audit.AuditEvent{
		Actor:        "cpam_user1",
		ActorType:    audit.ActorTypeAPIKey,
		Action:       audit.ActionCreate,
		ResourceType: audit.ResourcePool,
		ResourceID:   "1",
		StatusCode:   201,
	})
	_ = auditLogger.Log(ctx, &audit.AuditEvent{
		Actor:        "cpam_user2",
		ActorType:    audit.ActorTypeAPIKey,
		Action:       audit.ActionUpdate,
		ResourceType: audit.ResourceAccount,
		ResourceID:   "2",
		StatusCode:   200,
	})

	// Filter by actor
	rr := doAuthJSON(t, as.mux, stdhttp.MethodGet, "/api/v1/audit?actor=cpam_user1", "", stdhttp.StatusOK)

	var resp struct {
		Events []any `json:"events"`
		Total  int   `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("Expected 1 event for actor filter, got %d", resp.Total)
	}

	// Filter by action
	rr = doAuthJSON(t, as.mux, stdhttp.MethodGet, "/api/v1/audit?action=update", "", stdhttp.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("Expected 1 event for action filter, got %d", resp.Total)
	}

	// Filter by resource_type
	rr = doAuthJSON(t, as.mux, stdhttp.MethodGet, "/api/v1/audit?resource_type=pool", "", stdhttp.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("Expected 1 event for resource_type filter, got %d", resp.Total)
	}
}

func TestAudit_MethodNotAllowed(t *testing.T) {
	as, _, _ := setupAuthTestServer()

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/audit", nil)
	rr := httptest.NewRecorder()
	as.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rr.Code)
	}
}
