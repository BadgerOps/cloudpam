package http

import (
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

// --- Import Accounts Tests ---

func TestImportAccounts_MethodNotAllowed(t *testing.T) {
	srv, _ := setupTestServer()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/import/accounts", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestImportAccounts_EmptyCSV(t *testing.T) {
	srv, _ := setupTestServer()
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/accounts", strings.NewReader(""))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	// Empty body is technically invalid CSV with < 2 rows
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestImportAccounts_HeaderOnly(t *testing.T) {
	srv, _ := setupTestServer()
	body := "key,name\n"
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/accounts", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for header-only CSV, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestImportAccounts_MissingRequiredColumns(t *testing.T) {
	srv, _ := setupTestServer()
	body := "provider,description\naws,test\n"
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/accounts", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for missing key/name columns, got %d", rr.Code)
	}
}

func TestImportAccounts_Success(t *testing.T) {
	srv, st := setupTestServer()
	body := "key,name,provider,description\naws:111,Prod,aws,Production\naws:222,Dev,aws,Development\n"
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/accounts", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if int(resp["created"].(float64)) != 2 {
		t.Errorf("expected 2 created, got %v", resp["created"])
	}

	// Verify accounts exist in store
	accs, _ := st.ListAccounts(t.Context())
	if len(accs) != 2 {
		t.Errorf("expected 2 accounts in store, got %d", len(accs))
	}
}

func TestImportAccounts_WithOptionalColumns(t *testing.T) {
	srv, st := setupTestServer()
	body := "key,name,provider,platform,tier,environment,regions\naws:111,Prod,aws,aws,enterprise,production,us-east-1;us-west-2\n"
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/accounts", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	accs, _ := st.ListAccounts(t.Context())
	if len(accs) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accs))
	}
	if accs[0].Platform != "aws" {
		t.Errorf("expected platform aws, got %s", accs[0].Platform)
	}
	if accs[0].Tier != "enterprise" {
		t.Errorf("expected tier enterprise, got %s", accs[0].Tier)
	}
}

func TestImportAccounts_EmptyKeyOrName(t *testing.T) {
	srv, _ := setupTestServer()
	body := "key,name\n,Prod\naws:222,\n"
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/accounts", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if int(resp["created"].(float64)) != 0 {
		t.Errorf("expected 0 created, got %v", resp["created"])
	}
	errs := resp["errors"].([]interface{})
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errs))
	}
}

// --- Import Pools Tests ---

func TestImportPools_MethodNotAllowed(t *testing.T) {
	srv, _ := setupTestServer()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/import/pools", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestImportPools_MissingColumns(t *testing.T) {
	srv, _ := setupTestServer()
	body := "name,description\ntest,desc\n"
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/pools", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for missing cidr column, got %d", rr.Code)
	}
}

func TestImportPools_Success(t *testing.T) {
	srv, st := setupTestServer()
	body := "name,cidr\nroot,10.0.0.0/8\nchild,10.0.0.0/16\n"
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/pools", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if int(resp["created"].(float64)) != 2 {
		t.Errorf("expected 2 created, got %v", resp["created"])
	}

	pools, _ := st.ListPools(t.Context())
	if len(pools) != 2 {
		t.Errorf("expected 2 pools in store, got %d", len(pools))
	}
}

func TestImportPools_WithHierarchy(t *testing.T) {
	srv, st := setupTestServer()
	// Import pools with parent_id column — parent first, then child references parent by old ID
	body := "id,name,cidr,parent_id\n100,root,10.0.0.0/8,\n200,child,10.0.0.0/16,100\n"
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/pools", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if int(resp["created"].(float64)) != 2 {
		t.Errorf("expected 2 created, got %v", resp["created"])
	}

	// Verify child has parent set
	pools, _ := st.ListPools(t.Context())
	var child *struct{ parentID *int64 }
	for _, p := range pools {
		if p.Name == "child" {
			child = &struct{ parentID *int64 }{parentID: p.ParentID}
		}
	}
	if child == nil {
		t.Fatal("child pool not found")
	}
	if child.parentID == nil {
		t.Error("child should have a parent_id")
	}
}

func TestImportPools_WithAccountKey(t *testing.T) {
	srv, st := setupTestServer()

	// Create an account first
	st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:111111111111", Name: "Prod"})

	body := "name,cidr,account_key\nnet,10.0.0.0/8,aws:111111111111\n"
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/pools", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if int(resp["created"].(float64)) != 1 {
		t.Errorf("expected 1 created, got %v", resp["created"])
	}
}

func TestImportPools_BadAccountKey(t *testing.T) {
	srv, _ := setupTestServer()

	body := "name,cidr,account_key\nnet,10.0.0.0/8,nonexistent\n"
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/pools", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200 (with errors list), got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if int(resp["created"].(float64)) != 0 {
		t.Errorf("expected 0 created, got %v", resp["created"])
	}
	errs := resp["errors"].([]interface{})
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestImportPools_BadParentID(t *testing.T) {
	srv, _ := setupTestServer()

	body := "name,cidr,parent_id\nchild,10.0.0.0/16,999\n"
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/pools", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200 (with errors list), got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	errs := resp["errors"].([]interface{})
	if len(errs) != 1 {
		t.Errorf("expected 1 error for bad parent_id, got %d", len(errs))
	}
}

// --- writeStoreErr Tests ---

func TestWriteStoreErr(t *testing.T) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{
		Level:  "info",
		Format: "json",
		Output: io.Discard,
	})
	srv := NewServer(mux, st, logger, nil, nil)

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"not found", storage.ErrNotFound, stdhttp.StatusNotFound},
		{"conflict", storage.ErrConflict, stdhttp.StatusConflict},
		{"validation", storage.ErrValidation, stdhttp.StatusBadRequest},
		{"unknown", errors.New("something else"), stdhttp.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			srv.writeStoreErr(t.Context(), rr, tt.err)
			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}

// --- NewServerWithSlog Tests ---

func TestNewServerWithSlog(t *testing.T) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()

	// With nil slog logger
	srv := NewServerWithSlog(mux, st, nil)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.logger == nil {
		t.Error("expected non-nil logger even with nil slog input")
	}
}

// --- TestSentry Endpoint Tests ---

func TestTestSentry_DefaultResponse(t *testing.T) {
	srv, _ := setupTestServer()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/test-sentry", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	// Should return usage info
	var resp map[string]string
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp["message"] == "" {
		t.Error("expected non-empty message field")
	}
}

func TestTestSentry_MessageType(t *testing.T) {
	srv, _ := setupTestServer()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/test-sentry?type=message", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestTestSentry_ErrorType(t *testing.T) {
	srv, _ := setupTestServer()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/test-sentry?type=error", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

// --- Force Delete Tests ---

func TestPoolDelete_ForceTrue(t *testing.T) {
	srv, _ := setupTestServer()

	// Create parent with child
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/8"}`, stdhttp.StatusCreated)
	var parent poolDTO
	_ = json.NewDecoder(rr.Body).Decode(&parent)

	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools",
		`{"name":"child","cidr":"10.0.0.0/16","parent_id":`+itoa(parent.ID)+`}`,
		stdhttp.StatusCreated)

	// Non-force delete should fail
	doJSON(t, srv.mux, stdhttp.MethodDelete, "/api/v1/pools/"+itoa(parent.ID), "", stdhttp.StatusConflict)

	// Force delete should succeed
	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/pools/"+itoa(parent.ID)+"?force=true", nil)
	rrDel := httptest.NewRecorder()
	srv.mux.ServeHTTP(rrDel, req)
	if rrDel.Code != stdhttp.StatusNoContent {
		t.Fatalf("expected 204 on force delete, got %d: %s", rrDel.Code, rrDel.Body.String())
	}
}

func TestAccountDelete_ForceTrue(t *testing.T) {
	srv, _ := setupTestServer()

	// Create account and pool referencing it
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"aws:111111111111","name":"Prod"}`, stdhttp.StatusCreated)
	var acc struct{ ID int64 `json:"id"` }
	_ = json.NewDecoder(rr.Body).Decode(&acc)

	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools",
		`{"name":"net","cidr":"10.0.0.0/8","account_id":`+itoa(acc.ID)+`}`,
		stdhttp.StatusCreated)

	// Non-force delete should fail
	doJSON(t, srv.mux, stdhttp.MethodDelete, "/api/v1/accounts/"+itoa(acc.ID), "", stdhttp.StatusConflict)

	// Force delete should succeed
	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/accounts/"+itoa(acc.ID)+"?force=true", nil)
	rrDel := httptest.NewRecorder()
	srv.mux.ServeHTTP(rrDel, req)
	if rrDel.Code != stdhttp.StatusNoContent {
		t.Fatalf("expected 204 on force delete, got %d: %s", rrDel.Code, rrDel.Body.String())
	}
}

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}
