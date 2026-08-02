package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cloudpam/internal/domain"
)

// --- pool handlers ---

func TestPoolsRoutingErrorsCov(t *testing.T) {
	srv, st := setupTestServer()
	pool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "root", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	t.Run("collection method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPut, "/api/v1/pools", ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != "GET, POST" {
			t.Fatalf("Allow = %q", allow)
		}
	})

	cases := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantErr  string
	}{
		{"empty id", http.MethodGet, "/api/v1/pools/", http.StatusNotFound, "not found"},
		{"non numeric id", http.MethodGet, "/api/v1/pools/abc", http.StatusBadRequest, "invalid pool id"},
		{"unknown pool", http.MethodGet, "/api/v1/pools/9999", http.StatusNotFound, "not found"},
		{"hierarchy wrong method", http.MethodPost, "/api/v1/pools/hierarchy", http.StatusMethodNotAllowed, "method not allowed"},
		{"blocks wrong method", http.MethodPost, "/api/v1/pools/" + itoa(pool.ID) + "/blocks", http.StatusMethodNotAllowed, "method not allowed"},
		{"stats wrong method", http.MethodPost, "/api/v1/pools/" + itoa(pool.ID) + "/stats", http.StatusMethodNotAllowed, "method not allowed"},
		{"detail wrong method", http.MethodPut, "/api/v1/pools/" + itoa(pool.ID), http.StatusMethodNotAllowed, "method not allowed"},
		{"unknown pool stats", http.MethodGet, "/api/v1/pools/9999/stats", http.StatusNotFound, ""},
		{"delete unknown pool", http.MethodDelete, "/api/v1/pools/9999", http.StatusNotFound, "not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, srv.mux, tc.method, tc.path, ""), tc.wantCode)
			if tc.wantErr != "" {
				if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
					t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
				}
			}
		})
	}
}

func TestPoolsHierarchyAndStatsCov(t *testing.T) {
	srv, st := setupTestServer()
	parent, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "root", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	if _, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "child", CIDR: "10.0.1.0/24", ParentID: &parent.ID}); err != nil {
		t.Fatalf("create child: %v", err)
	}

	t.Run("invalid root_id", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/pools/hierarchy?root_id=abc", ""), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid root_id" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("unknown root_id", func(t *testing.T) {
		assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/pools/hierarchy?root_id=9999", ""), http.StatusNotFound)
	})

	t.Run("scoped hierarchy", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/pools/hierarchy?root_id="+itoa(parent.ID), ""), http.StatusOK)
		var resp struct {
			Pools []domain.PoolWithStats `json:"pools"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp.Pools) != 1 || resp.Pools[0].ID != parent.ID {
			t.Fatalf("unexpected hierarchy: %+v", resp.Pools)
		}
	})

	t.Run("pool stats", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/pools/"+itoa(parent.ID)+"/stats", ""), http.StatusOK)
		var stats domain.PoolStats
		if err := json.Unmarshal(rr.Body.Bytes(), &stats); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if stats.TotalIPs == 0 {
			t.Fatalf("expected non-zero total addresses: %+v", stats)
		}
	})

	t.Run("list with stats", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/pools?include_stats=true", ""), http.StatusOK)
		var pools []struct {
			ID    int64            `json:"id"`
			Stats domain.PoolStats `json:"stats"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &pools); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(pools) != 2 {
			t.Fatalf("len(pools) = %d, want 2", len(pools))
		}
		for _, p := range pools {
			if p.Stats.TotalIPs == 0 {
				t.Fatalf("pool %d missing stats: %+v", p.ID, p.Stats)
			}
		}
	})
}

func TestCreatePoolValidationCov(t *testing.T) {
	srv, _ := setupTestServer()

	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{`},
		{"blank name", `{"name":"   ","cidr":"10.0.0.0/16"}`},
		{"invalid cidr", `{"name":"p","cidr":"not-a-cidr"}`},
		{"ipv6 cidr", `{"name":"p","cidr":"2001:db8::/32"}`},
		{"host bits set", `{"name":"p","cidr":"10.0.0.1/16"}`},
		{"invalid type", `{"name":"p","cidr":"10.0.0.0/16","type":"wormhole"}`},
		{"invalid status", `{"name":"p","cidr":"10.0.0.0/16","status":"pending-ish"}`},
		{"invalid source", `{"name":"p","cidr":"10.0.0.0/16","source":"telepathy"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/pools", tc.body), http.StatusBadRequest)
			if e := decodeErrCov(t, rr); e.Error == "" {
				t.Fatal("expected an error message")
			}
		})
	}

	rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/pools", ""), http.StatusOK)
	var pools []poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &pools); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("rejected pools must not be stored, got %d", len(pools))
	}
}

func TestUpdatePoolValidationCov(t *testing.T) {
	srv, st := setupTestServer()
	pool, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "root", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	path := "/api/v1/pools/" + itoa(pool.ID)

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantErr  string
	}{
		{"malformed json", `{`, http.StatusBadRequest, "invalid json"},
		{"blank name", `{"name":"   "}`, http.StatusBadRequest, ""},
		{"invalid type", `{"type":"wormhole"}`, http.StatusBadRequest, "invalid pool type"},
		{"invalid status", `{"status":"maybe"}`, http.StatusBadRequest, "invalid pool status"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPatch, path, tc.body), tc.wantCode)
			if tc.wantErr != "" {
				if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
					t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
				}
			}
		})
	}

	t.Run("patch on an unknown pool", func(t *testing.T) {
		assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPatch, "/api/v1/pools/9999", `{"name":"x"}`), http.StatusNotFound)
	})

	t.Run("updates metadata and preserves the account", func(t *testing.T) {
		acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "a", Provider: "aws"})
		if err != nil {
			t.Fatalf("create account: %v", err)
		}
		assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPatch, path, `{"account_id":`+itoa(acct.ID)+`}`), http.StatusOK)

		// A patch that omits account_id must retain the existing assignment.
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPatch, path,
			`{"name":"renamed","type":"vpc","status":"active","description":"d","tags":{"env":"prod"}}`), http.StatusOK)
		var updated domain.Pool
		if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if updated.Name != "renamed" || updated.Type != domain.PoolTypeVPC || updated.Status != domain.PoolStatusActive {
			t.Fatalf("unexpected pool: %+v", updated)
		}
		if updated.AccountID == nil || *updated.AccountID != acct.ID {
			t.Fatalf("account assignment was lost: %+v", updated.AccountID)
		}
		if updated.Tags["env"] != "prod" || updated.Description != "d" {
			t.Fatalf("metadata not applied: %+v", updated)
		}
	})
}

// --- account handlers ---

func TestAccountsRoutingAndValidationCov(t *testing.T) {
	srv, st := setupTestServer()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	t.Run("collection method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPut, "/api/v1/accounts", ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != "GET, POST" {
			t.Fatalf("Allow = %q", allow)
		}
	})

	routing := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantErr  string
	}{
		{"empty id", http.MethodGet, "/api/v1/accounts/", http.StatusNotFound, "not found"},
		{"non numeric id", http.MethodGet, "/api/v1/accounts/abc", http.StatusBadRequest, "invalid id"},
		{"unknown account", http.MethodGet, "/api/v1/accounts/9999", http.StatusNotFound, "not found"},
		{"detail wrong method", http.MethodPut, "/api/v1/accounts/" + itoa(acct.ID), http.StatusMethodNotAllowed, "method not allowed"},
		{"patch unknown account", http.MethodPatch, "/api/v1/accounts/9999", http.StatusNotFound, "not found"},
		{"delete unknown account", http.MethodDelete, "/api/v1/accounts/9999", http.StatusNotFound, "not found"},
	}
	for _, tc := range routing {
		t.Run(tc.name, func(t *testing.T) {
			body := ""
			if tc.method == http.MethodPatch {
				body = `{"name":"x"}`
			}
			rr := assertStatusCov(t, doReqCov(t, srv.mux, tc.method, tc.path, body), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}

	create := []struct {
		name string
		body string
	}{
		{"malformed json", `{`},
		{"missing key", `{"name":"x"}`},
		{"invalid key", `{"key":"bad key!","name":"x"}`},
		{"missing name", `{"key":"aws:2"}`},
		{"duplicate key", `{"key":"aws:1","name":"dupe"}`},
	}
	for _, tc := range create {
		t.Run("create "+tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/accounts", tc.body), http.StatusBadRequest)
			if e := decodeErrCov(t, rr); e.Error == "" {
				t.Fatal("expected an error message")
			}
		})
	}

	t.Run("patch malformed json", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPatch, "/api/v1/accounts/"+itoa(acct.ID), `{`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid json" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("patch invalid name", func(t *testing.T) {
		long := strings.Repeat("a", 5000)
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPatch, "/api/v1/accounts/"+itoa(acct.ID),
			`{"name":"`+long+`"}`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error == "" {
			t.Fatal("expected a validation error")
		}
	})

	t.Run("patch updates the account", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPatch, "/api/v1/accounts/"+itoa(acct.ID),
			`{"name":"prod-renamed","platform":"eks"}`), http.StatusOK)
		var updated domain.Account
		if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if updated.Name != "prod-renamed" {
			t.Fatalf("name = %q", updated.Name)
		}
	})
}

func TestDeleteAccountCascadeCov(t *testing.T) {
	srv, st := setupTestServer()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "p", CIDR: "10.0.0.0/16", AccountID: &acct.ID}); err != nil {
		t.Fatalf("create pool: %v", err)
	}

	rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodDelete, "/api/v1/accounts/"+itoa(acct.ID), ""), http.StatusConflict)
	if e := decodeErrCov(t, rr); e.Error == "" {
		t.Fatal("expected a conflict message when pools reference the account")
	}

	assertStatusCov(t, doReqCov(t, srv.mux, http.MethodDelete, "/api/v1/accounts/"+itoa(acct.ID)+"?force=yes", ""), http.StatusNoContent)
	if _, found, err := st.GetAccount(t.Context(), acct.ID); err != nil || found {
		t.Fatalf("account should be gone (found=%v, err=%v)", found, err)
	}
}

// --- block handlers ---

func TestBlocksListPaginationCov(t *testing.T) {
	srv, st := setupTestServer()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws", Platform: "eks"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	parent, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "root", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := st.CreatePool(t.Context(), domain.CreatePool{
			Name: "child" + itoa(int64(i)), CIDR: "10.0." + itoa(int64(i)) + ".0/24",
			ParentID: &parent.ID, AccountID: &acct.ID,
		}); err != nil {
			t.Fatalf("create child: %v", err)
		}
	}

	type blocksResp struct {
		Items []struct {
			ID              int64  `json:"id"`
			AccountName     string `json:"account_name"`
			AccountPlatform string `json:"account_platform"`
			ParentName      string `json:"parent_name"`
		} `json:"items"`
		Total    int `json:"total"`
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}

	get := func(t *testing.T, query string) blocksResp {
		t.Helper()
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/blocks"+query, ""), http.StatusOK)
		var resp blocksResp
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return resp
	}

	all := get(t, "")
	if all.Total != 3 || len(all.Items) != 3 {
		t.Fatalf("unexpected default listing: %+v", all)
	}
	if all.Items[0].AccountName != "prod" || all.Items[0].AccountPlatform != "eks" || all.Items[0].ParentName != "root" {
		t.Fatalf("account/parent metadata missing: %+v", all.Items[0])
	}

	if page2 := get(t, "?page=2&page_size=2"); len(page2.Items) != 1 || page2.Total != 3 {
		t.Fatalf("unexpected second page: %+v", page2)
	}
	if past := get(t, "?page=9&page_size=2"); len(past.Items) != 0 || past.Total != 3 {
		t.Fatalf("page past the end should be empty: %+v", past)
	}
	if unpaged := get(t, "?page_size=all"); unpaged.PageSize != 0 || len(unpaged.Items) != 3 {
		t.Fatalf("page_size=all should disable paging: %+v", unpaged)
	}
	if capped := get(t, "?page_size=100000"); capped.PageSize != 500 {
		t.Fatalf("page_size should be capped at 500, got %d", capped.PageSize)
	}
	if filtered := get(t, "?pools="+itoa(parent.ID)+"&accounts="+itoa(acct.ID)); filtered.Total != 3 {
		t.Fatalf("filters dropped matching rows: %+v", filtered)
	}
	if none := get(t, "?accounts=9999"); none.Total != 0 {
		t.Fatalf("non-matching account filter returned %+v", none)
	}

	for _, bad := range []string{"?page_size=abc", "?page_size=-1"} {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/blocks"+bad, ""), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid page_size" {
			t.Fatalf("%s: error = %q", bad, e.Error)
		}
	}
	for _, bad := range []string{"?page=abc", "?page=0", "?page=-2"} {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/blocks"+bad, ""), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid page" {
			t.Fatalf("%s: error = %q", bad, e.Error)
		}
	}
}

// --- schema handlers ---

func TestSchemaCheckAndApplyValidationCov(t *testing.T) {
	srv, st := setupTestServer()
	if _, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "existing", CIDR: "10.20.0.0/16"}); err != nil {
		t.Fatalf("create pool: %v", err)
	}

	for _, path := range []string{"/api/v1/schema/check", "/api/v1/schema/apply"} {
		t.Run(path+" method not allowed", func(t *testing.T) {
			assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, path, ""), http.StatusMethodNotAllowed)
		})
		t.Run(path+" malformed json", func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, path, `{`), http.StatusBadRequest)
			if e := decodeErrCov(t, rr); e.Error != "invalid json" {
				t.Fatalf("error = %q", e.Error)
			}
		})
		t.Run(path+" empty pools", func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, path, `{"pools":[]}`), http.StatusBadRequest)
			if e := decodeErrCov(t, rr); e.Error != "pools array is required" {
				t.Fatalf("error = %q", e.Error)
			}
		})
	}

	t.Run("check rejects out-of-range prefixes", func(t *testing.T) {
		for _, cidr := range []string{"10.0.0.0/4", "10.0.0.0/31", "10.0.0.0/32", "not-a-cidr"} {
			rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/schema/check",
				`{"pools":[{"name":"p","cidr":"`+cidr+`"}]}`), http.StatusBadRequest)
			if e := decodeErrCov(t, rr); !strings.HasPrefix(e.Error, "pool 0") {
				t.Fatalf("%s: error = %q", cidr, e.Error)
			}
		}
	})

	t.Run("check reports containment relationships", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/schema/check",
			`{"pools":[{"name":"inner","cidr":"10.20.1.0/24"},{"name":"outer","cidr":"10.0.0.0/8"},{"name":"clean","cidr":"192.168.0.0/16"}]}`), http.StatusOK)
		var resp struct {
			Conflicts []struct {
				PlannedCIDR string `json:"planned_cidr"`
				OverlapType string `json:"overlap_type"`
			} `json:"conflicts"`
			TotalPools    int `json:"total_pools"`
			ConflictCount int `json:"conflict_count"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.TotalPools != 3 || resp.ConflictCount != 2 {
			t.Fatalf("unexpected summary: %+v", resp)
		}
		byCIDR := map[string]string{}
		for _, c := range resp.Conflicts {
			byCIDR[c.PlannedCIDR] = c.OverlapType
		}
		if byCIDR["10.20.1.0/24"] != "contained_by" {
			t.Fatalf("inner overlap type = %q, want contained_by", byCIDR["10.20.1.0/24"])
		}
		if byCIDR["10.0.0.0/8"] != "contains" {
			t.Fatalf("outer overlap type = %q, want contains", byCIDR["10.0.0.0/8"])
		}
	})

	t.Run("apply validation", func(t *testing.T) {
		cases := []struct {
			name    string
			body    string
			wantSub string
		}{
			{"invalid status", `{"pools":[{"ref":"a","name":"a","cidr":"172.16.0.0/16"}],"status":"maybe"}`, "invalid status"},
			{"missing ref", `{"pools":[{"name":"a","cidr":"172.16.0.0/16"}]}`, "ref is required"},
			{"duplicate ref", `{"pools":[{"ref":"a","name":"a","cidr":"172.16.0.0/16"},{"ref":"a","name":"b","cidr":"172.17.0.0/16"}]}`, "duplicate ref"},
			{"invalid name", `{"pools":[{"ref":"a","name":"","cidr":"172.16.0.0/16"}]}`, "pool 0 (a)"},
			{"invalid cidr", `{"pools":[{"ref":"a","name":"a","cidr":"nope"}]}`, "pool 0 (a)"},
			{"forward parent ref", `{"pools":[{"ref":"child","name":"c","cidr":"172.16.1.0/24","parent_ref":"root"},{"ref":"root","name":"r","cidr":"172.16.0.0/16"}]}`, "parent_ref"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/schema/apply", tc.body), http.StatusBadRequest)
				if e := decodeErrCov(t, rr); !strings.Contains(e.Error, tc.wantSub) {
					t.Fatalf("error = %q, want it to contain %q", e.Error, tc.wantSub)
				}
			})
		}
	})
}

// --- update handlers ---

func TestUpdateHandlersControlDirDisabledCov(t *testing.T) {
	updateSrv := newTestUpdateServer(t, nil)
	updateSrv.controlDir = ""

	t.Run("upgrade is not implemented", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/updates/upgrade", nil)
		rr := httptest.NewRecorder()
		updateSrv.handleTriggerUpgrade(rr, req)
		assertStatusCov(t, rr, http.StatusNotImplemented)
		if e := decodeErrCov(t, rr); e.Error != "in-app upgrade is not enabled" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("status reports unsupported", func(t *testing.T) {
		for _, h := range []struct {
			name string
			fn   func(http.ResponseWriter, *http.Request)
			path string
		}{
			{"status", updateSrv.handleGetUpgradeStatus, "/api/v1/updates/status"},
			{"ack", updateSrv.handleAcknowledgeUpgradeStatus, "/api/v1/updates/status/ack"},
		} {
			req := httptest.NewRequest(http.MethodGet, h.path, nil)
			rr := httptest.NewRecorder()
			h.fn(rr, req)
			assertStatusCov(t, rr, http.StatusOK)
			var resp map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp["status"] != "unsupported" || resp["supported"] != false {
				t.Fatalf("%s: unexpected response %+v", h.name, resp)
			}
		}
	})
}

func TestUpdateStatusLifecycleCov(t *testing.T) {
	updateSrv := newTestUpdateServer(t, nil)
	controlDir := updateSrv.controlDir
	statusPath := filepath.Join(controlDir, upgradeStatusFile)

	t.Run("missing status file is idle", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil)
		rr := httptest.NewRecorder()
		updateSrv.handleGetUpgradeStatus(rr, req)
		assertStatusCov(t, rr, http.StatusOK)
		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["status"] != "idle" || resp["supported"] != true {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("ack rejects a non-completed upgrade", func(t *testing.T) {
		if err := os.WriteFile(statusPath, []byte(`{"status":"running","upgrade_id":"u1"}`), 0o600); err != nil {
			t.Fatalf("write status: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/updates/status/ack", nil)
		rr := httptest.NewRecorder()
		updateSrv.handleAcknowledgeUpgradeStatus(rr, req)
		assertStatusCov(t, rr, http.StatusConflict)
		if e := decodeErrCov(t, rr); e.Error != "upgrade is not completed" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("running status is returned verbatim", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil)
		rr := httptest.NewRecorder()
		updateSrv.handleGetUpgradeStatus(rr, req)
		assertStatusCov(t, rr, http.StatusOK)
		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["status"] != "running" || resp["upgrade_id"] != "u1" {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("malformed status file is a server error", func(t *testing.T) {
		if err := os.WriteFile(statusPath, []byte(`{not json`), 0o600); err != nil {
			t.Fatalf("write status: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil)
		rr := httptest.NewRecorder()
		updateSrv.handleGetUpgradeStatus(rr, req)
		assertStatusCov(t, rr, http.StatusInternalServerError)
		if e := decodeErrCov(t, rr); e.Error != "failed to read upgrade status" {
			t.Fatalf("error = %q", e.Error)
		}

		req = httptest.NewRequest(http.MethodPost, "/api/v1/updates/status/ack", nil)
		rr = httptest.NewRecorder()
		updateSrv.handleAcknowledgeUpgradeStatus(rr, req)
		assertStatusCov(t, rr, http.StatusInternalServerError)
	})

	t.Run("completed status can be acknowledged", func(t *testing.T) {
		if err := os.WriteFile(statusPath, []byte(`{"status":"completed","upgrade_id":"u2","target_version":"9.9.9"}`), 0o600); err != nil {
			t.Fatalf("write status: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/updates/status/ack", nil)
		rr := httptest.NewRecorder()
		updateSrv.handleAcknowledgeUpgradeStatus(rr, req)
		assertStatusCov(t, rr, http.StatusOK)

		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["status"] != "idle" || resp["acknowledged"] != true || resp["upgrade_id"] != "u2" {
			t.Fatalf("unexpected ack response: %+v", resp)
		}

		ack, err := readJSONFile(filepath.Join(controlDir, upgradeAckFile))
		if err != nil {
			t.Fatalf("read ack file: %v", err)
		}
		if ack["upgrade_id"] != "u2" || ack["target_version"] != "9.9.9" {
			t.Fatalf("unexpected ack file: %+v", ack)
		}

		// After acknowledgement the status endpoint reports idle again.
		req = httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil)
		rr = httptest.NewRecorder()
		updateSrv.handleGetUpgradeStatus(rr, req)
		assertStatusCov(t, rr, http.StatusOK)
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["status"] != "idle" {
			t.Fatalf("status = %v, want idle after acknowledgement", resp["status"])
		}
	})
}

func TestUpdateLoadReleasesErrorsCov(t *testing.T) {
	t.Run("non-200 response", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer api.Close()
		updateSrv := newTestUpdateServer(t, api)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/updates?force=true", nil)
		rr := httptest.NewRecorder()
		updateSrv.handleCheckUpdates(rr, req)
		assertStatusCov(t, rr, http.StatusOK)
		assertUnfetchableReleasesCov(t, rr)
	})

	t.Run("malformed json", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{not json`))
		}))
		defer api.Close()
		updateSrv := newTestUpdateServer(t, api)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/updates?force=true", nil)
		rr := httptest.NewRecorder()
		updateSrv.handleCheckUpdates(rr, req)
		assertStatusCov(t, rr, http.StatusOK)
		assertUnfetchableReleasesCov(t, rr)
	})

	t.Run("upgrade reports a release metadata failure", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer api.Close()
		updateSrv := newTestUpdateServer(t, api)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/updates/upgrade", nil)
		rr := httptest.NewRecorder()
		updateSrv.handleTriggerUpgrade(rr, req)
		assertStatusCov(t, rr, http.StatusInternalServerError)
		if e := decodeErrCov(t, rr); e.Error != "failed to fetch release metadata" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("no stable release", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"tag_name": "v1.0.0-rc1", "draft": false, "prerelease": true},
				{"tag_name": "v1.0.0", "draft": true, "prerelease": false},
				{"tag_name": "nightly", "draft": false, "prerelease": false},
			})
		}))
		defer api.Close()
		updateSrv := newTestUpdateServer(t, api)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/updates/upgrade", nil)
		rr := httptest.NewRecorder()
		updateSrv.handleTriggerUpgrade(rr, req)
		assertStatusCov(t, rr, http.StatusInternalServerError)
		if e := decodeErrCov(t, rr); e.Error != "no stable release found" {
			t.Fatalf("error = %q", e.Error)
		}
	})
}

func TestUpdateTriggerUpgradeConflictCov(t *testing.T) {
	updateSrv := newTestUpdateServer(t, nil)
	statusPath := filepath.Join(updateSrv.controlDir, upgradeStatusFile)
	if err := os.WriteFile(statusPath, []byte(`{"status":"running"}`), 0o600); err != nil {
		t.Fatalf("write status: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/updates/upgrade", nil)
	rr := httptest.NewRecorder()
	updateSrv.handleTriggerUpgrade(rr, req)
	assertStatusCov(t, rr, http.StatusConflict)
	if e := decodeErrCov(t, rr); e.Error != "upgrade already in progress" {
		t.Fatalf("error = %q", e.Error)
	}

	if _, err := os.Stat(filepath.Join(updateSrv.controlDir, upgradeRequestedFile)); !os.IsNotExist(err) {
		t.Fatal("no upgrade request file should be written while an upgrade is running")
	}
}

// assertUnfetchableReleasesCov checks the degraded update-check payload.
func assertUnfetchableReleasesCov(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "could not fetch releases" {
		t.Fatalf("error = %v, want a fetch failure", resp["error"])
	}
	if resp["update_available"] != false || resp["upgrade_supported"] != false {
		t.Fatalf("unexpected degraded payload: %+v", resp)
	}
}
