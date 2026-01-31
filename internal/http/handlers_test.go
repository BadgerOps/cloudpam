package http

import (
	"context"
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
	logger := observability.NewLogger(observability.Config{
		Level:  "info",
		Format: "json",
		Output: io.Discard,
	})
	srv := NewServer(mux, st, logger, nil)
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

	// update name via PATCH (RESTful path)
	child := pools[1]
	doJSON(t, srv.mux, stdhttp.MethodPatch, "/api/v1/pools/"+strconv.FormatInt(child.ID, 10), `{"name":"child2"}`, stdhttp.StatusOK)

	// delete without force should fail (has child for root)
	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/pools/"+strconv.FormatInt(root.ID, 10), nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}

	// delete with force
	doJSON(t, srv.mux, stdhttp.MethodDelete, "/api/v1/pools/"+strconv.FormatInt(root.ID, 10)+"?force=1", "", stdhttp.StatusNoContent)
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

	// delete pool invalid id (REST path)
	req = httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/pools/notanint", nil)
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
	// delete invalid id (REST path)
	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/accounts/bad", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAccounts_Subroutes_CRUD(t *testing.T) {
	srv, _ := setupTestServer()
	// Create account
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"aws:999999999999","name":"Sandbox"}`, stdhttp.StatusCreated)
	var acc struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &acc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// GET /api/v1/accounts/{id}
	doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/accounts/"+strconv.FormatInt(acc.ID, 10), "", stdhttp.StatusOK)

	// PATCH /api/v1/accounts/{id}
	doJSON(t, srv.mux, stdhttp.MethodPatch, "/api/v1/accounts/"+strconv.FormatInt(acc.ID, 10), `{"name":"Sandbox2"}`, stdhttp.StatusOK)

	// Create pool referencing the account, to induce delete conflict
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.200.0.0/16","account_id":`+strconv.FormatInt(acc.ID, 10)+`}`, stdhttp.StatusCreated)

	// DELETE /api/v1/accounts/{id} without force -> expect 409
	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/accounts/"+strconv.FormatInt(acc.ID, 10), nil)
	res := httptest.NewRecorder()
	srv.mux.ServeHTTP(res, req)
	if res.Code != stdhttp.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}

	// DELETE with force -> 204
	doJSON(t, srv.mux, stdhttp.MethodDelete, "/api/v1/accounts/"+strconv.FormatInt(acc.ID, 10)+"?force=1", "", stdhttp.StatusNoContent)
}

func TestAccounts_UpdateMetadata_Tier(t *testing.T) {
	srv, _ := setupTestServer()
	// Create account
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"aws:222222222222","name":"Staging"}`, stdhttp.StatusCreated)
	var acc struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &acc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// PATCH tier + platform + environment
	body := `{"platform":"aws","tier":"sbx","environment":"stg","regions":["us-west-2"]}`
	doJSON(t, srv.mux, stdhttp.MethodPatch, "/api/v1/accounts/"+strconv.FormatInt(acc.ID, 10), body, stdhttp.StatusOK)
	// GET and verify
	rr = doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/accounts/"+strconv.FormatInt(acc.ID, 10), "", stdhttp.StatusOK)
	var out struct {
		Platform, Tier, Environment string
		Regions                     []string
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Platform != "aws" || out.Tier != "sbx" || out.Environment != "stg" || len(out.Regions) != 1 || out.Regions[0] != "us-west-2" {
		t.Fatalf("metadata mismatch: %+v", out)
	}
}

func TestPools_Overlap_SiblingsAndBlocksAnnotation(t *testing.T) {
	srv, _ := setupTestServer()
	// create parent
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.10.0.0/16"}`, stdhttp.StatusCreated)
	var root poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &root); err != nil {
		t.Fatalf("unmarshal root: %v", err)
	}
	// create child /24
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"c24","cidr":"10.10.1.0/24","parent_id":`+strconv.FormatInt(root.ID, 10)+`}`, stdhttp.StatusCreated)
	// attempt overlapping sibling /29 -> 400
	bad := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/pools", strings.NewReader(`{"name":"c29","cidr":"10.10.1.0/29","parent_id":`+strconv.FormatInt(root.ID, 10)+`}`))
	bad.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	srv.mux.ServeHTTP(res, bad)
	if res.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	// Blocks annotation: /26 blocks that intersect 10.10.1.0/24 should be marked ExistsElsewhere
	path := "/api/v1/pools/" + strconv.FormatInt(root.ID, 10) + "/blocks?new_prefix_len=26&page_size=all"
	rr2 := doJSON(t, srv.mux, stdhttp.MethodGet, path, "", stdhttp.StatusOK)
	var env struct {
		Items []struct {
			CIDR            string `json:"cidr"`
			ExistsElsewhere bool   `json:"exists_elsewhere"`
		}
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Items) == 0 {
		t.Fatalf("expected items")
	}
	// Find a /26 inside 10.10.1.0/24
	var found bool
	for _, it := range env.Items {
		if strings.HasPrefix(it.CIDR, "10.10.1.") && it.ExistsElsewhere {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected an ExistsElsewhere block within 10.10.1.0/24")
	}
}

func TestBlocks_PaginationWindow(t *testing.T) {
	srv, _ := setupTestServer()
	// parent /16
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/16"}`, stdhttp.StatusCreated)
	var root poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &root); err != nil {
		t.Fatalf("unmarshal root: %v", err)
	}
	// page_size=3, page=2 over /24s
	path := "/api/v1/pools/" + strconv.FormatInt(root.ID, 10) + "/blocks?new_prefix_len=24&page_size=3&page=2"
	rr2 := doJSON(t, srv.mux, stdhttp.MethodGet, path, "", stdhttp.StatusOK)
	var env struct {
		Items []struct {
			CIDR string `json:"cidr"`
		}
		Total    int `json:"total"`
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Total != 256 {
		t.Fatalf("expected total 256, got %d", env.Total)
	}
	if len(env.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(env.Items))
	}
	if env.Items[0].CIDR != "10.0.3.0/24" || env.Items[2].CIDR != "10.0.5.0/24" {
		t.Fatalf("unexpected window: %+v", env.Items)
	}
}

func TestAnalytics_MetadataInBlocks(t *testing.T) {
	srv, _ := setupTestServer()
	// create two accounts with different metadata
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"aws:111111111111","name":"Prod"}`, stdhttp.StatusCreated)
	var a1 struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &a1)
	rr = doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"gcp:my-project-dev","name":"Dev"}`, stdhttp.StatusCreated)
	var a2 struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &a2)
	// patch metadata and verify persistence
	doJSON(t, srv.mux, stdhttp.MethodPatch, "/api/v1/accounts/"+strconv.FormatInt(a1.ID, 10), `{"platform":"aws","environment":"prd","regions":["us-east-1","us-west-2"]}`, stdhttp.StatusOK)
	ar := doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/accounts/"+strconv.FormatInt(a1.ID, 10), "", stdhttp.StatusOK)
	var acc1 struct {
		Platform, Environment string
		Regions               []string
	}
	if err := json.Unmarshal(ar.Body.Bytes(), &acc1); err != nil {
		t.Fatalf("unmarshal acc1: %v", err)
	}
	if acc1.Platform != "aws" || acc1.Environment != "prd" || len(acc1.Regions) == 0 {
		t.Fatalf("account1 metadata not set: %+v", acc1)
	}
	doJSON(t, srv.mux, stdhttp.MethodPatch, "/api/v1/accounts/"+strconv.FormatInt(a2.ID, 10), `{"platform":"gcp","environment":"dev","regions":["us-east1"]}`, stdhttp.StatusOK)
	ar = doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/accounts/"+strconv.FormatInt(a2.ID, 10), "", stdhttp.StatusOK)
	var acc2 struct {
		Platform, Environment string
		Regions               []string
	}
	if err := json.Unmarshal(ar.Body.Bytes(), &acc2); err != nil {
		t.Fatalf("unmarshal acc2: %v", err)
	}
	if acc2.Platform != "gcp" || acc2.Environment != "dev" || len(acc2.Regions) == 0 {
		t.Fatalf("account2 metadata not set: %+v", acc2)
	}
	// parent and children
	rr = doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.50.0.0/16"}`, stdhttp.StatusCreated)
	var root poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &root); err != nil {
		t.Fatalf("unmarshal root: %v", err)
	}
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"prodnet","cidr":"10.50.1.0/24","parent_id":`+strconv.FormatInt(root.ID, 10)+`,"account_id":`+strconv.FormatInt(a1.ID, 10)+`}`, stdhttp.StatusCreated)
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"devnet","cidr":"10.50.2.0/24","parent_id":`+strconv.FormatInt(root.ID, 10)+`,"account_id":`+strconv.FormatInt(a2.ID, 10)+`}`, stdhttp.StatusCreated)

	// fetch analytics rows
	rr = doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/blocks?page_size=all", "", stdhttp.StatusOK)
	var env struct {
		Items []struct {
			AccountID                                        *int64 `json:"account_id"`
			AccountName, AccountPlatform, AccountEnvironment string
			AccountRegions                                   []string
		}
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Items) < 2 {
		t.Fatalf("expected at least 2 items")
	}
	var seenProd, seenDev bool
	for _, it := range env.Items {
		if it.AccountID == nil {
			continue
		}
		if *it.AccountID == a1.ID {
			seenProd = true
		}
		if *it.AccountID == a2.ID {
			seenDev = true
		}
	}
	if !seenProd || !seenDev {
		t.Fatalf("did not see both account rows")
	}

	// apply accounts and pools filters combination
	rr = doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/blocks?accounts="+strconv.FormatInt(a1.ID, 10)+"&pools="+strconv.FormatInt(root.ID, 10)+"&page_size=all", "", stdhttp.StatusOK)
	var env2 struct {
		Items []struct {
			AccountID *int64 `json:"account_id"`
		}
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, it := range env2.Items {
		if it.AccountID == nil || *it.AccountID != a1.ID {
			t.Fatalf("filter mismatch: %+v", it)
		}
	}
}

func TestBlocks_HostsAndNoExistsElsewhere(t *testing.T) {
	srv, _ := setupTestServer()
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.60.0.0/16"}`, stdhttp.StatusCreated)
	var root poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// request /26 blocks
	rr = doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/pools/"+strconv.FormatInt(root.ID, 10)+"/blocks?new_prefix_len=26&page_size=3&page=1", "", stdhttp.StatusOK)
	var env struct {
		Items []struct {
			Hosts           uint64 `json:"hosts"`
			ExistsElsewhere bool   `json:"exists_elsewhere"`
		}
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Items) == 0 {
		t.Fatalf("expected some items")
	}
	// /26 has 64 addresses; usableHostsIPv4 returns total-2 for <=30
	if env.Items[0].Hosts != 62 {
		t.Fatalf("expected 62 hosts for /26, got %d", env.Items[0].Hosts)
	}
	for _, it := range env.Items {
		if it.ExistsElsewhere {
			t.Fatalf("should not be marked exists elsewhere in empty subtree")
		}
	}
}
func TestAccountsHandlers_AndBlocks(t *testing.T) {
	srv, _ := setupTestServer()

	// create account
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"aws:123456789012","name":"Prod"}`, stdhttp.StatusCreated)
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
		Items []struct {
			AccountID *int64 `json:"account_id"`
		}
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal rows: %v", err)
	}
	if env.Total == 0 || len(env.Items) == 0 || env.Items[0].AccountID == nil || *env.Items[0].AccountID != acc.ID {
		t.Fatalf("expected rows for account %d", acc.ID)
	}

	// delete account without force should fail due to referencing pools
	rr = doJSON(t, srv.mux, stdhttp.MethodDelete, "/api/v1/accounts/"+strconv.FormatInt(acc.ID, 10), "", stdhttp.StatusConflict)
	_ = rr
	// with force should succeed
	doJSON(t, srv.mux, stdhttp.MethodDelete, "/api/v1/accounts/"+strconv.FormatInt(acc.ID, 10)+"?force=1", "", stdhttp.StatusNoContent)
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
	var e struct {
		Error string `json:"error"`
	}
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
	if err := json.Unmarshal(rr.Body.Bytes(), &root); err != nil {
		t.Fatalf("unmarshal root: %v", err)
	}

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
	eq := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"eq","cidr":"10.0.0.0/8","parent_id":`+strconv.FormatInt(root.ID, 10)+`}`, stdhttp.StatusBadRequest)
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

func TestOpenAPISpecEndpoint(t *testing.T) {
	srv, _ := setupTestServer()

	req := httptest.NewRequest(stdhttp.MethodGet, "/openapi.yaml", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/yaml" {
		t.Fatalf("unexpected content-type: %q", got)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "openapi: 3.1.0") {
		if len(body) > 80 {
			body = body[:80]
		}
		t.Fatalf("spec body missing openapi version, got: %q", body)
	}

	req = httptest.NewRequest(stdhttp.MethodPost, "/openapi.yaml", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for non-GET, got %d", rr.Code)
	}
}

func TestHealthzEndpoint(t *testing.T) {
	srv, _ := setupTestServer()

	req := httptest.NewRequest(stdhttp.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", resp["status"])
	}
}

func TestReadyzEndpoint(t *testing.T) {
	srv, _ := setupTestServer()

	req := httptest.NewRequest(stdhttp.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %q", resp.Status)
	}
	if resp.Checks["database"] != "ok" {
		t.Fatalf("expected database check ok, got %q", resp.Checks["database"])
	}
}

func TestReadyzEndpointMethodNotAllowed(t *testing.T) {
	srv, _ := setupTestServer()

	req := httptest.NewRequest(stdhttp.MethodPost, "/readyz", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", rr.Code)
	}
}

// failingStore is a mock store that always fails database operations
type failingStore struct {
	storage.MemoryStore
}

func (f *failingStore) ListPools(ctx context.Context) ([]domain.Pool, error) {
	return nil, errors.New("database connection failed")
}

func TestReadyzEndpointDatabaseFailure(t *testing.T) {
	st := &failingStore{}
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{
		Level:  "info",
		Format: "json",
		Output: io.Discard,
	})
	srv := NewServer(mux, st, logger, nil)
	srv.RegisterRoutes()

	req := httptest.NewRequest(stdhttp.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}

	var resp struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Status != "unhealthy" {
		t.Fatalf("expected status unhealthy, got %q", resp.Status)
	}
	if resp.Checks["database"] != "error" {
		t.Fatalf("expected database check error, got %q", resp.Checks["database"])
	}
}

func TestMetricsEndpoint(t *testing.T) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{
		Level:  "info",
		Format: "json",
		Output: io.Discard,
	})
	// Create with metrics enabled
	metrics := observability.NewMetrics(observability.MetricsConfig{
		Namespace: "cloudpam",
		Version:   "test",
	})
	srv := NewServer(mux, st, logger, metrics)
	srv.RegisterRoutes()

	// Make a request to trigger metrics recording
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/16"}`, stdhttp.StatusCreated)

	// Fetch metrics endpoint
	req := httptest.NewRequest(stdhttp.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Check for expected metrics
	if !strings.Contains(body, "cloudpam_info") {
		t.Errorf("expected cloudpam_info metric in output")
	}
	if !strings.Contains(body, `version="test"`) {
		t.Errorf("expected version label in output")
	}
	if !strings.Contains(body, "cloudpam_rate_limit_requests_total") {
		t.Errorf("expected cloudpam_rate_limit_requests_total metric in output")
	}
	if !strings.Contains(body, "cloudpam_active_connections") {
		t.Errorf("expected cloudpam_active_connections metric in output")
	}

	// Check content-type
	contentType := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/plain") {
		t.Errorf("expected Content-Type text/plain, got %q", contentType)
	}
}

func TestMetricsEndpointDisabled(t *testing.T) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{
		Level:  "info",
		Format: "json",
		Output: io.Discard,
	})
	// Create without metrics (nil)
	srv := NewServer(mux, st, logger, nil)
	srv.RegisterRoutes()

	// Metrics endpoint should 404 when metrics is nil
	req := httptest.NewRequest(stdhttp.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404 when metrics disabled, got %d", rr.Code)
	}
}

func TestExportEndpoint(t *testing.T) {
	srv, _ := setupTestServer()

	// Create test data
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"aws:123456789012","name":"TestAccount"}`, stdhttp.StatusCreated)
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/16"}`, stdhttp.StatusCreated)

	// Test missing datasets parameter
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/export", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for missing datasets, got %d", rr.Code)
	}

	// Test invalid datasets
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/export?datasets=invalid", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for invalid datasets, got %d", rr.Code)
	}

	// Test method not allowed
	req = httptest.NewRequest(stdhttp.MethodPost, "/api/v1/export?datasets=accounts", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", rr.Code)
	}

	// Test successful export with accounts
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/export?datasets=accounts", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("expected Content-Type application/zip, got %q", ct)
	}

	// Test successful export with pools
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/export?datasets=pools", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Test successful export with all datasets
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/export?datasets=accounts,pools,blocks", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestListAccountsEndpoint(t *testing.T) {
	srv, _ := setupTestServer()

	// Test empty list
	rr := doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/accounts", "", stdhttp.StatusOK)
	var accounts []struct{}
	if err := json.Unmarshal(rr.Body.Bytes(), &accounts); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("expected empty list, got %d", len(accounts))
	}

	// Create an account
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"aws:123456789012","name":"Test"}`, stdhttp.StatusCreated)

	// Test list with data
	rr = doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/accounts", "", stdhttp.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &accounts); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
}

func TestPoolHandlers_MethodNotAllowed(t *testing.T) {
	srv, _ := setupTestServer()

	// Test PUT on /api/v1/pools
	req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/pools", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}

	// Test PUT on /api/v1/accounts
	req = httptest.NewRequest(stdhttp.MethodPut, "/api/v1/accounts", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestAccountSubroutes_NotFound(t *testing.T) {
	srv, _ := setupTestServer()

	// Empty path after /accounts/
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/accounts/", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	// Non-existent account
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/accounts/9999", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	// Invalid method on account subroute
	req = httptest.NewRequest(stdhttp.MethodPost, "/api/v1/accounts/1", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestPoolSubroutes_NotFound(t *testing.T) {
	srv, _ := setupTestServer()

	// Empty path after /pools/
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/pools/", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	// Non-existent pool
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/pools/9999", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	// Invalid method on pool subroute
	req = httptest.NewRequest(stdhttp.MethodPost, "/api/v1/pools/1", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}

	// Blocks endpoint on non-existent pool
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/pools/9999/blocks?new_prefix_len=24", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	// POST on blocks endpoint
	req = httptest.NewRequest(stdhttp.MethodPost, "/api/v1/pools/1/blocks?new_prefix_len=24", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestAccountPatch_InvalidJSON(t *testing.T) {
	srv, _ := setupTestServer()

	// Create an account first
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"aws:123456789012","name":"Test"}`, stdhttp.StatusCreated)
	var acc struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &acc)

	// Try PATCH with invalid JSON
	req := httptest.NewRequest(stdhttp.MethodPatch, "/api/v1/accounts/"+strconv.FormatInt(acc.ID, 10), strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPoolPatch_InvalidJSON(t *testing.T) {
	srv, _ := setupTestServer()

	// Create a pool first
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/16"}`, stdhttp.StatusCreated)
	var pool poolDTO
	json.Unmarshal(rr.Body.Bytes(), &pool)

	// Try PATCH with invalid JSON
	req := httptest.NewRequest(stdhttp.MethodPatch, "/api/v1/pools/"+strconv.FormatInt(pool.ID, 10), strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestBlocksEndpoint_Pagination(t *testing.T) {
	srv, _ := setupTestServer()

	// Create pool
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/16"}`, stdhttp.StatusCreated)
	var pool poolDTO
	json.Unmarshal(rr.Body.Bytes(), &pool)

	// Test invalid page_size
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/blocks?page_size=-1", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for negative page_size, got %d", rr.Code)
	}

	// Test invalid page
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/blocks?page=0", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for page=0, got %d", rr.Code)
	}

	// Test invalid page format
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/blocks?page=abc", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for invalid page, got %d", rr.Code)
	}

	// Test page_size=all
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/blocks?page_size=all", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestBlocksForPool_Pagination(t *testing.T) {
	srv, _ := setupTestServer()

	// Create pool
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root","cidr":"10.0.0.0/16"}`, stdhttp.StatusCreated)
	var pool poolDTO
	json.Unmarshal(rr.Body.Bytes(), &pool)
	poolPath := "/api/v1/pools/" + strconv.FormatInt(pool.ID, 10) + "/blocks"

	// Test invalid page_size
	req := httptest.NewRequest(stdhttp.MethodGet, poolPath+"?new_prefix_len=24&page_size=-1", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for negative page_size, got %d", rr.Code)
	}

	// Test invalid page
	req = httptest.NewRequest(stdhttp.MethodGet, poolPath+"?new_prefix_len=24&page=0", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected 400 for page=0, got %d", rr.Code)
	}
}

func TestIndexHandler_NotFoundPath(t *testing.T) {
	srv, _ := setupTestServer()

	// Non-root path should 404
	req := httptest.NewRequest(stdhttp.MethodGet, "/nonexistent", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}
