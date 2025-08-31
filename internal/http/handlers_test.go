package http

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"cloudpam/internal/storage"
)

type poolDTO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CIDR      string `json:"cidr"`
	ParentID  *int64 `json:"parent_id"`
	AccountID *int64 `json:"account_id"`
}

func setupTestServer() (*Server, *storage.MemoryStore) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	srv := NewServer(mux, st)
	srv.RegisterRoutes()
	return srv, st
}

func doJSON(t *testing.T, mux *stdhttp.ServeMux, method, path, body string, code int) *httptest.ResponseRecorder {
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

func TestPoolsHandlers_CRUD(t *testing.T) {
	srv, _ := setupTestServer()

	// create top-level pool
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/16"}`, stdhttp.StatusCreated)

	// list should have 1
	rr := doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/pools", "", stdhttp.StatusOK)
	var pools []poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &pools); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(pools))
	}
	root := pools[0]

	// create sub-pool
	body := `{"name":"child","cidr":"10.0.1.0/24","parent_id":` + strconv.FormatInt(root.ID, 10) + `}`
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", body, stdhttp.StatusCreated)

	// list should have 2
	rr = doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/pools", "", stdhttp.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &pools); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pools) != 2 {
		t.Fatalf("expected 2 pools, got %d", len(pools))
	}

	// update name via PATCH
	child := pools[1]
	q := url.Values{"id": []string{strconv.FormatInt(child.ID, 10)}}
	doJSON(t, srv.mux, stdhttp.MethodPatch, "/api/v1/pools?"+q.Encode(), `{"name":"child2"}`, stdhttp.StatusOK)

	// delete without force should fail (has child for root)
	q = url.Values{"id": []string{strconv.FormatInt(root.ID, 10)}}
	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/pools?"+q.Encode(), nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}

	// delete with force
	q.Set("force", "1")
	doJSON(t, srv.mux, stdhttp.MethodDelete, "/api/v1/pools?"+q.Encode(), "", stdhttp.StatusNoContent)
}

func TestPoolsHandlers_Negative(t *testing.T) {
	srv, _ := setupTestServer()

	// invalid JSON
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":`, stdhttp.StatusBadRequest)

	// missing fields
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":""}`, stdhttp.StatusBadRequest)

	// invalid cidr format and invalid address
	rrBad := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"x","cidr":"10.0.0/24"}`, stdhttp.StatusBadRequest)
	if !strings.Contains(rrBad.Body.String(), "invalid cidr") {
		t.Fatalf("expected body to mention invalid cidr, got: %q", rrBad.Body.String())
	}

	// create a valid parent
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/16"}`, stdhttp.StatusCreated)
	var root poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// child outside parent
	body := `{"name":"child","cidr":"10.1.0.0/24","parent_id":` + strconv.FormatInt(root.ID, 10) + `}`
	rrBad = doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", body, stdhttp.StatusBadRequest)
	if !strings.Contains(rrBad.Body.String(), "invalid sub-pool cidr") {
		t.Fatalf("expected invalid sub-pool error, got: %q", rrBad.Body.String())
	}

	// blocks endpoint missing new_prefix_len
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/pools/"+strconv.FormatInt(root.ID, 10)+"/blocks", nil)
	rr2 := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr2, req)
	if rr2.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr2.Code)
	}

	// invalid new_prefix_len value
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/pools/"+strconv.FormatInt(root.ID, 10)+"/blocks?new_prefix_len=abc", nil)
	rr2 = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr2, req)
	if rr2.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr2.Code)
	}

	// new_prefix_len less than parent bits
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/pools/"+strconv.FormatInt(root.ID, 10)+"/blocks?new_prefix_len=8", nil)
	rr2 = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr2, req)
	if rr2.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for too small prefix, got %d", rr2.Code)
	}
    if !strings.Contains(rr2.Body.String(), "between") && !strings.Contains(rr2.Body.String(), "invalid new_prefix_len") {
        t.Fatalf("expected range/invalid message, got: %q", rr2.Body.String())
    }

	// new_prefix_len greater than 32
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/pools/"+strconv.FormatInt(root.ID, 10)+"/blocks?new_prefix_len=33", nil)
	rr2 = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr2, req)
	if rr2.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for too large prefix, got %d", rr2.Code)
	}
    if !strings.Contains(rr2.Body.String(), "between") && !strings.Contains(rr2.Body.String(), "invalid new_prefix_len") {
        t.Fatalf("expected range/invalid message, got: %q", rr2.Body.String())
    }

	// delete pool invalid id
	req = httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/pools?id=notanint", nil)
	rr2 = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr2, req)
	if rr2.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr2.Code)
	}
}

func TestAccountsHandlers_Negative(t *testing.T) {
	srv, _ := setupTestServer()
	// invalid json
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":`, stdhttp.StatusBadRequest)
	// missing required
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"","name":""}`, stdhttp.StatusBadRequest)
	// delete invalid id
	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/accounts?id=bad", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAccountsHandlers_AndBlocks(t *testing.T) {
	srv, _ := setupTestServer()

	// create account
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"aws:123","name":"Prod"}`, stdhttp.StatusCreated)
	var acc struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &acc); err != nil {
		t.Fatalf("unmarshal account: %v", err)
	}

	// create parent + child pools assigned to account
	rr = doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/16","account_id":`+strconv.FormatInt(acc.ID, 10)+`}`, stdhttp.StatusCreated)
	var root poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &root); err != nil {
		t.Fatalf("unmarshal root: %v", err)
	}
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"child","cidr":"10.0.1.0/24","parent_id":`+strconv.FormatInt(root.ID, 10)+`,"account_id":`+strconv.FormatInt(acc.ID, 10)+`}`, stdhttp.StatusCreated)

	// blocks for root should mark assigned
	path := "/api/v1/pools/" + strconv.FormatInt(root.ID, 10) + "/blocks?new_prefix_len=24&page_size=all"
	rr = doJSON(t, srv.mux, stdhttp.MethodGet, path, "", stdhttp.StatusOK)
	var resp struct {
		Items []struct {
			CIDR         string `json:"cidr"`
			Used         bool   `json:"used"`
			AssignedName string `json:"assigned_name"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal blocks: %v", err)
	}
	if resp.Total == 0 {
		t.Fatalf("expected some blocks")
	}
	// find the child cidr and ensure used
	var found bool
	for _, it := range resp.Items {
		if it.Used && it.AssignedName == "child" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected to find assigned child block")
	}

    // global blocks filter by account (request all for simple assertion)
    rr = doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/blocks?accounts="+strconv.FormatInt(acc.ID, 10)+"&page_size=all", "", stdhttp.StatusOK)
    var env struct {
        Items []struct{ AccountID *int64 `json:"account_id"` }
        Total int `json:"total"`
    }
    if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
        t.Fatalf("unmarshal rows: %v", err)
    }
    if env.Total == 0 || len(env.Items) == 0 || env.Items[0].AccountID == nil || *env.Items[0].AccountID != acc.ID {
        t.Fatalf("expected rows for account %d", acc.ID)
    }

	// delete account without force should fail due to referencing pools
	rr = doJSON(t, srv.mux, stdhttp.MethodDelete, "/api/v1/accounts?id="+strconv.FormatInt(acc.ID, 10), "", stdhttp.StatusConflict)
	_ = rr
	// with force should succeed
	doJSON(t, srv.mux, stdhttp.MethodDelete, "/api/v1/accounts?id="+strconv.FormatInt(acc.ID, 10)+"&force=1", "", stdhttp.StatusNoContent)
}

func TestErrorEnvelope_JSON(t *testing.T) {
    srv, _ := setupTestServer()
    // create a valid parent
    rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/16"}`, stdhttp.StatusCreated)
    var root poolDTO
    if err := json.Unmarshal(rr.Body.Bytes(), &root); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    // missing new_prefix_len -> should be JSON error envelope
    req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/pools/"+strconv.FormatInt(root.ID, 10)+"/blocks", nil)
    res := httptest.NewRecorder()
    srv.mux.ServeHTTP(res, req)
    if res.Code != stdhttp.StatusBadRequest {
        t.Fatalf("expected 400, got %d", res.Code)
    }
    if ct := res.Header().Get("Content-Type"); ct == "" || ct[:16] != "application/json" {
        t.Fatalf("expected json content-type, got %q", ct)
    }
    var e struct{ Error string `json:"error"` }
    if err := json.Unmarshal(res.Body.Bytes(), &e); err != nil {
        t.Fatalf("not json: %v: %s", err, res.Body.String())
    }
    if e.Error == "" {
        t.Fatalf("expected error message")
    }
}

func TestPoolsHandlers_OverlapProtection(t *testing.T) {
    srv, _ := setupTestServer()

    // Create a large top-level pool
    rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/8"}`, stdhttp.StatusCreated)
    var root poolDTO
    if err := json.Unmarshal(rr.Body.Bytes(), &root); err != nil { t.Fatalf("unmarshal root: %v", err) }

    // Create a child /24 inside it (in a region we won't reuse later)
    body := `{"name":"c24","cidr":"10.1.0.0/24","parent_id":` + strconv.FormatInt(root.ID, 10) + `}`
    _ = doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", body, stdhttp.StatusCreated)

    // Attempt overlapping /29 under the same parent -> should fail
    body = `{"name":"c29","cidr":"10.1.0.0/29","parent_id":` + strconv.FormatInt(root.ID, 10) + `}`
    bad := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", body, stdhttp.StatusBadRequest)
    if !strings.Contains(bad.Body.String(), "overlap") {
        t.Fatalf("expected overlap error, got: %s", bad.Body.String())
    }

    // Attempt child equal to parent prefix -> should fail (must be stricter than parent)
    eq := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"eq","cidr":"10.0.0.0/8","parent_id":`+strconv.FormatInt(root.ID,10)+`}`, stdhttp.StatusBadRequest)
    if !strings.Contains(eq.Body.String(), "greater") && !strings.Contains(eq.Body.String(), "invalid sub-pool") {
        t.Fatalf("expected strict subset error, got: %s", eq.Body.String())
    }

    // Attempt another overlapping top-level root -> should fail
    bad2 := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root2","cidr":"10.0.0.0/16"}`, stdhttp.StatusBadRequest)
    if !strings.Contains(bad2.Body.String(), "overlap") {
        t.Fatalf("expected overlap error for top-level, got: %s", bad2.Body.String())
    }

    // Additional cross-tree global tests are constrained by strict subset rules.
}
