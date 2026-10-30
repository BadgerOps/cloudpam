package api

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"strings"
	"testing"

	"cloudpam/internal/domain"
)

func TestSchemaCheck_NoConflicts(t *testing.T) {
	srv, _ := setupTestServer()

	body := `{"pools":[
		{"ref":"root","name":"Root","cidr":"10.0.0.0/8","type":"supernet","parent_ref":""},
		{"ref":"r0","name":"us-east-1","cidr":"10.0.0.0/12","type":"region","parent_ref":"root"}
	]}`
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/schema/check", body, stdhttp.StatusOK)

	var resp schemaCheckResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ConflictCount != 0 {
		t.Fatalf("expected 0 conflicts, got %d", resp.ConflictCount)
	}
	if resp.TotalPools != 2 {
		t.Fatalf("expected 2 total pools, got %d", resp.TotalPools)
	}
}

func TestSchemaCheck_WithConflict(t *testing.T) {
	srv, _ := setupTestServer()

	// Create an existing pool first
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools",
		`{"name":"existing","cidr":"10.0.0.0/16"}`, stdhttp.StatusCreated)

	// Check schema that overlaps
	body := `{"pools":[
		{"ref":"root","name":"Root","cidr":"10.0.0.0/8","type":"supernet","parent_ref":""}
	]}`
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/schema/check", body, stdhttp.StatusOK)

	var resp schemaCheckResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ConflictCount != 1 {
		t.Fatalf("expected 1 conflict, got %d", resp.ConflictCount)
	}
	if resp.Conflicts[0].PlannedName != "Root" {
		t.Fatalf("expected planned name 'Root', got %q", resp.Conflicts[0].PlannedName)
	}
	if resp.Conflicts[0].ExistingPoolName != "existing" {
		t.Fatalf("expected existing pool name 'existing', got %q", resp.Conflicts[0].ExistingPoolName)
	}
}

func TestSchemaCheck_InvalidCIDR(t *testing.T) {
	srv, _ := setupTestServer()

	body := `{"pools":[
		{"ref":"bad","name":"Bad","cidr":"999.0.0.0/8","type":"supernet","parent_ref":""}
	]}`
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/schema/check", body, stdhttp.StatusBadRequest)
}

func TestSchemaCheck_EmptyPools(t *testing.T) {
	srv, _ := setupTestServer()
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/schema/check", `{"pools":[]}`, stdhttp.StatusBadRequest)
}

func TestSchemaCheck_MethodNotAllowed(t *testing.T) {
	srv, _ := setupTestServer()
	doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/schema/check", "", stdhttp.StatusMethodNotAllowed)
}

func TestSchemaApply_CreatesHierarchy(t *testing.T) {
	srv, _ := setupTestServer()

	body := `{
		"pools":[
			{"ref":"root","name":"Root","cidr":"10.0.0.0/8","type":"supernet","parent_ref":""},
			{"ref":"r0","name":"us-east-1","cidr":"10.0.0.0/12","type":"region","parent_ref":"root"},
			{"ref":"e0","name":"prod","cidr":"10.0.0.0/16","type":"environment","parent_ref":"r0"}
		],
		"status":"planned",
		"tags":{"test":"true"},
		"skip_conflicts":true
	}`
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/schema/apply", body, stdhttp.StatusOK)

	var resp schemaApplyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Created != 3 {
		t.Fatalf("expected 3 created, got %d", resp.Created)
	}
	if resp.RootPoolID == 0 {
		t.Fatal("expected non-zero root_pool_id")
	}
	if len(resp.PoolMap) != 3 {
		t.Fatalf("expected 3 entries in pool_map, got %d", len(resp.PoolMap))
	}

	// Verify parent relationships via pool detail
	rootID := resp.PoolMap["root"]
	regionID := resp.PoolMap["r0"]
	envID := resp.PoolMap["e0"]

	// Get root pool - should have no parent
	rrRoot := doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/pools/"+itoa(rootID), "", stdhttp.StatusOK)
	var rootPool struct {
		ParentID *int64 `json:"parent_id"`
		Name     string `json:"name"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(rrRoot.Body.Bytes(), &rootPool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rootPool.ParentID != nil {
		t.Fatal("root pool should have no parent")
	}
	if rootPool.Status != "planned" {
		t.Fatalf("expected status 'planned', got %q", rootPool.Status)
	}

	// Get region pool - should have root as parent
	rrRegion := doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/pools/"+itoa(regionID), "", stdhttp.StatusOK)
	var regionPool struct {
		ParentID *int64 `json:"parent_id"`
	}
	if err := json.Unmarshal(rrRegion.Body.Bytes(), &regionPool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if regionPool.ParentID == nil || *regionPool.ParentID != rootID {
		t.Fatalf("region pool should have root as parent, got %v", regionPool.ParentID)
	}

	// Get env pool - should have region as parent
	rrEnv := doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/pools/"+itoa(envID), "", stdhttp.StatusOK)
	var envPool struct {
		ParentID *int64 `json:"parent_id"`
	}
	if err := json.Unmarshal(rrEnv.Body.Bytes(), &envPool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envPool.ParentID == nil || *envPool.ParentID != regionID {
		t.Fatalf("env pool should have region as parent, got %v", envPool.ParentID)
	}
}

func TestSchemaApply_ConflictRejected(t *testing.T) {
	srv, _ := setupTestServer()

	// Create an existing pool
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools",
		`{"name":"existing","cidr":"10.0.0.0/16"}`, stdhttp.StatusCreated)

	// Apply schema that overlaps, with skip_conflicts=false
	body := `{
		"pools":[
			{"ref":"root","name":"Root","cidr":"10.0.0.0/8","type":"supernet","parent_ref":""}
		],
		"status":"planned",
		"skip_conflicts":false
	}`
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/schema/apply", body, stdhttp.StatusConflict)
}

func TestSchemaApply_DuplicateRef(t *testing.T) {
	srv, _ := setupTestServer()

	body := `{
		"pools":[
			{"ref":"dup","name":"A","cidr":"10.0.0.0/8","type":"supernet","parent_ref":""},
			{"ref":"dup","name":"B","cidr":"10.16.0.0/12","type":"region","parent_ref":"dup"}
		],
		"skip_conflicts":true
	}`
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/schema/apply", body, stdhttp.StatusBadRequest)
}

func TestSchemaApply_InvalidParentRef(t *testing.T) {
	srv, _ := setupTestServer()

	body := `{
		"pools":[
			{"ref":"child","name":"Child","cidr":"10.0.0.0/16","type":"subnet","parent_ref":"nonexistent"}
		],
		"skip_conflicts":true
	}`
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/schema/apply", body, stdhttp.StatusBadRequest)
}

func TestSchemaApply_MethodNotAllowed(t *testing.T) {
	srv, _ := setupTestServer()
	doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/schema/apply", "", stdhttp.StatusMethodNotAllowed)
}

func TestSchemaCheck_ForwardAndMissingParentRefs(t *testing.T) {
	cases := []struct {
		name, body, wantDetail string
	}{
		{
			name: "child outside a parent declared later",
			body: `{"pools":[
				{"ref":"child","name":"Child","cidr":"10.60.0.0/24","type":"environment","parent_ref":"root"},
				{"ref":"root","name":"Root","cidr":"10.50.0.0/16","type":"supernet"}
			]}`,
			wantDetail: "pool 0 (child): child not within parent",
		},
		{
			name: "parent_ref that names no entry",
			body: `{"pools":[
				{"ref":"root","name":"Root","cidr":"10.50.0.0/16","type":"supernet"},
				{"ref":"child","name":"Child","cidr":"10.50.1.0/24","type":"environment","parent_ref":"ghost"}
			]}`,
			wantDetail: `pool 1 (child): parent_ref "ghost" not found`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := setupTestServer()
			rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/schema/check", tc.body, stdhttp.StatusBadRequest)
			var got apiError
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Error != "invalid sub-pool cidr" || got.Detail != tc.wantDetail {
				t.Fatalf("got %q / %q, want detail %q", got.Error, got.Detail, tc.wantDetail)
			}
		})
	}
}

func TestSchemaPlan_ChildOutsideParentRefused(t *testing.T) {
	plan := func(child string, skip bool) string {
		return fmt.Sprintf(`{"pools":[
			{"ref":"root","name":"Root","cidr":"10.50.0.0/16","type":"supernet"},
			{"ref":"ok","name":"Inside","cidr":"10.50.1.0/24","type":"environment","parent_ref":"root"},
			{"ref":"child","name":"Child","cidr":%q,"type":"environment","parent_ref":"root"}
		],"skip_conflicts":%t}`, child, skip)
	}
	singleCreate := func(t *testing.T, child string) apiError {
		t.Helper()
		srv, st := setupTestServer()
		parent, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "Root", CIDR: "10.50.0.0/16"})
		if err != nil {
			t.Fatalf("create parent: %v", err)
		}
		rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools",
			fmt.Sprintf(`{"name":"Child","cidr":%q,"parent_id":%d}`, child, parent.ID), stdhttp.StatusBadRequest)
		var e apiError
		if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return e
	}

	for _, child := range []string{"10.60.0.0/24", "10.50.0.0/16", "10.0.0.0/12"} {
		want := singleCreate(t, child)
		for _, req := range []struct {
			path string
			skip bool
		}{
			{"/api/v1/schema/check", false},
			{"/api/v1/schema/apply", false},
			{"/api/v1/schema/apply", true},
		} {
			t.Run(fmt.Sprintf("%s %s skip_conflicts=%t", req.path, child, req.skip), func(t *testing.T) {
				srv, st := setupTestServer()
				rr := doJSON(t, srv.mux, stdhttp.MethodPost, req.path, plan(child, req.skip), stdhttp.StatusBadRequest)
				var got apiError
				if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if got.Error != want.Error {
					t.Fatalf("error = %q, want %q as POST /api/v1/pools returns", got.Error, want.Error)
				}
				if !strings.Contains(got.Detail, "pool 2 (child)") || !strings.Contains(got.Detail, want.Detail) {
					t.Fatalf("detail = %q, want it to name pool 2 (child) and say %q", got.Detail, want.Detail)
				}
				pools, err := st.ListPools(t.Context())
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(pools) != 0 {
					t.Fatalf("refused plan stored %d pools: %+v", len(pools), pools)
				}
			})
		}
	}
}
