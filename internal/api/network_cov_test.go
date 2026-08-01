package api

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

// setupNetworkServerWithoutStoreCov builds a NetworkServer that has no durable
// network store, so object/relationship endpoints must report 501.
func setupNetworkServerWithoutStoreCov(t *testing.T) *http.ServeMux {
	t.Helper()
	st := storage.NewMemoryStore()
	mux := http.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, st, logger, nil, nil)
	ds := storage.NewMemoryDiscoveryStore(st)
	ns := NewNetworkServer(srv, st, ds, storage.NewMemoryDriftStore(st))
	ns.RegisterNetworkRoutes()
	return mux
}

func TestNetworkViewsMethodNotAllowedCov(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	for _, path := range []string{
		"/api/v1/network/flat",
		"/api/v1/network/hierarchy",
		"/api/v1/network/merged",
		"/api/v1/network/conflicts",
	} {
		t.Run(path, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path, `{}`), http.StatusMethodNotAllowed)
			if allow := rr.Header().Get("Allow"); allow != http.MethodGet {
				t.Fatalf("Allow = %q, want GET", allow)
			}
			if e := decodeErrCov(t, rr); e.Error != "method not allowed" {
				t.Fatalf("error = %q", e.Error)
			}
		})
	}
}

func TestNetworkMergedMatchesHierarchyCov(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	parent, err := st.CreatePool(t.Context(), domain.CreatePool{
		Name: "root", CIDR: "10.80.0.0/16", Type: domain.PoolTypeVPC,
		Status: domain.PoolStatusActive, AccountID: &acct.ID,
	})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	if _, err := st.CreatePool(t.Context(), domain.CreatePool{
		Name: "child", CIDR: "10.80.1.0/24", ParentID: &parent.ID, AccountID: &acct.ID,
	}); err != nil {
		t.Fatalf("create child pool: %v", err)
	}

	var merged, hierarchy domain.NetworkViewResponse
	rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/merged", ""), http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &merged); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, "/api/v1/network/hierarchy", ""), http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &hierarchy); err != nil {
		t.Fatalf("unmarshal hierarchy: %v", err)
	}

	if merged.Total != hierarchy.Total || len(merged.Items) != len(hierarchy.Items) {
		t.Fatalf("merged (%d/%d) should mirror hierarchy (%d/%d)",
			merged.Total, len(merged.Items), hierarchy.Total, len(hierarchy.Items))
	}
	if merged.Total != 2 {
		t.Fatalf("total = %d, want 2 pools", merged.Total)
	}
	if len(merged.Items) != 1 || len(merged.Items[0].Children) != 1 {
		t.Fatalf("expected one root with one child, got %+v", merged.Items)
	}
}

func TestNetworkConflictSubroutesNotFoundCov(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	paths := []string{
		"/api/v1/network/conflicts/",
		"/api/v1/network/conflicts/abc",
		"/api/v1/network/conflicts/abc/unknown",
		"/api/v1/network/conflicts/abc/actions/teleport",
		"/api/v1/network/conflicts/abc/actions/link/extra",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path, `{}`), http.StatusNotFound)
			if e := decodeErrCov(t, rr); e.Error != "not found" {
				t.Fatalf("error = %q", e.Error)
			}
		})
	}
}

func TestNetworkResolveConflictValidationCov(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()
	path := "/api/v1/network/conflicts/some-conflict/resolve"

	t.Run("method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, path, ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != http.MethodPost {
			t.Fatalf("Allow = %q", allow)
		}
	})

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"malformed json", `{`, "invalid request body"},
		{"missing decision", `{}`, "decision is required"},
		{"blank decision", `{"decision":"   "}`, "decision is required"},
		{"unknown decision", `{"decision":"nuke"}`, "decision must be one of skip, ignore, or defer"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path, tc.body), http.StatusBadRequest)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}

	t.Run("unknown conflict", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path, `{"decision":"skip"}`), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "conflict not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})
}

func TestNetworkConflictActionValidationCov(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	t.Run("link action", func(t *testing.T) {
		path := "/api/v1/network/conflicts/c1/actions/link"
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, path, ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != http.MethodPost {
			t.Fatalf("Allow = %q", allow)
		}

		tests := []struct {
			name    string
			body    string
			wantErr string
		}{
			{"malformed json", `{`, "invalid request body"},
			{"missing discovered_id", `{"pool_id":1}`, "discovered_id is required"},
			{"missing pool_id", `{"discovered_id":"` + uuid.New().String() + `"}`, "pool_id is required"},
			{"negative pool_id", `{"discovered_id":"` + uuid.New().String() + `","pool_id":-1}`, "pool_id is required"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path, tc.body), http.StatusBadRequest)
				if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
					t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
				}
			})
		}

		rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path,
			`{"discovered_id":"`+uuid.New().String()+`","pool_id":1}`), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "conflict not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("import action", func(t *testing.T) {
		path := "/api/v1/network/conflicts/c1/actions/import"
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, path, ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != http.MethodPost {
			t.Fatalf("Allow = %q", allow)
		}

		rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path, `{`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid request body" {
			t.Fatalf("error = %q", e.Error)
		}

		rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path, `{"resource_ids":[]}`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "resource_ids must include at least one discovered resource" {
			t.Fatalf("error = %q", e.Error)
		}

		rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path,
			`{"resource_ids":["`+uuid.New().String()+`"]}`), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "conflict not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("placeholder parent action", func(t *testing.T) {
		path := "/api/v1/network/conflicts/c1/actions/create_placeholder_parent"
		rr := assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodGet, path, ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != http.MethodPost {
			t.Fatalf("Allow = %q", allow)
		}

		rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path, `{`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid request body" {
			t.Fatalf("error = %q", e.Error)
		}

		rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path, `{}`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "discovered_id is required" {
			t.Fatalf("error = %q", e.Error)
		}

		rr = assertStatusCov(t, doReqCov(t, discSrv.srv.mux, http.MethodPost, path,
			`{"discovered_id":"`+uuid.New().String()+`"}`), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "conflict not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})
}

func TestNetworkStoreUnavailableCov(t *testing.T) {
	mux := setupNetworkServerWithoutStoreCov(t)

	cases := []struct {
		method  string
		path    string
		wantErr string
	}{
		{http.MethodGet, "/api/v1/network/objects", "network object storage is not available"},
		{http.MethodPost, "/api/v1/network/objects", "network object storage is not available"},
		{http.MethodGet, "/api/v1/network/objects/1", "network object storage is not available"},
		{http.MethodGet, "/api/v1/network/relationships", "network relationship storage is not available"},
		{http.MethodPost, "/api/v1/network/relationships/resolve", "network relationship storage is not available"},
		{http.MethodPost, "/api/v1/network/conflicts/c1/actions/create_placeholder_parent", "network object storage is not available"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, mux, tc.method, tc.path, `{}`), http.StatusNotImplemented)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}
}

func TestNetworkObjectsErrorPathsCov(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()
	mux := discSrv.srv.mux

	t.Run("collection method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodDelete, "/api/v1/network/objects", ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != "GET, POST" {
			t.Fatalf("Allow = %q", allow)
		}
	})

	t.Run("create with malformed json", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/network/objects", `{`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid request body" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("invalid object id", func(t *testing.T) {
		for _, path := range []string{"/api/v1/network/objects/abc", "/api/v1/network/objects/0", "/api/v1/network/objects/-3", "/api/v1/network/objects/"} {
			rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, path, ""), http.StatusNotFound)
			if e := decodeErrCov(t, rr); e.Error != "not found" {
				t.Fatalf("%s: error = %q", path, e.Error)
			}
		}
	})

	t.Run("unknown object", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/network/objects/9999", ""), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "network object not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("patch malformed json", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPatch, "/api/v1/network/objects/1", `{`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid request body" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("patch unknown object", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPatch, "/api/v1/network/objects/9999", `{"name":"x"}`), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "network object not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("subroute method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodDelete, "/api/v1/network/objects/1", ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != "GET, PATCH" {
			t.Fatalf("Allow = %q", allow)
		}
	})
}

func TestNetworkObjectsFilteringCov(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()
	mux := discSrv.srv.mux
	acctA, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "a", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	acctB, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:2", Name: "b", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	mk := func(acctID int64, name, cidr, region string) domain.NetworkObject {
		body, err := json.Marshal(domain.CreateNetworkObject{
			ObjectType: domain.NetworkObjectTypeVPC, Provider: "aws", AccountID: acctID,
			Region: region, Name: name, CIDR: cidr, ProviderResourceID: name,
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/network/objects", string(body)), http.StatusCreated)
		var obj domain.NetworkObject
		if err := json.Unmarshal(rr.Body.Bytes(), &obj); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return obj
	}
	objA := mk(acctA.ID, "vpc-a", "10.90.0.0/16", "us-east-1")
	mk(acctB.ID, "vpc-b", "10.91.0.0/16", "eu-west-1")

	list := func(query string) domain.NetworkObjectListResponse {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/network/objects"+query, ""), http.StatusOK)
		var resp domain.NetworkObjectListResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return resp
	}

	if got := list(""); got.Total != 2 {
		t.Fatalf("unfiltered total = %d, want 2", got.Total)
	}
	byAccount := list("?account_id=" + itoa(acctA.ID))
	if byAccount.Total != 1 || byAccount.Items[0].ID != objA.ID {
		t.Fatalf("account filter returned %+v", byAccount)
	}
	if got := list("?region=eu-west-1"); got.Total != 1 || got.Items[0].Name != "vpc-b" {
		t.Fatalf("region filter returned %+v", got)
	}
	if got := list("?object_type=subnet"); got.Total != 0 {
		t.Fatalf("object_type filter returned %+v", got)
	}

	// Fetch and patch the object we created.
	rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/network/objects/"+itoa(objA.ID), ""), http.StatusOK)
	var fetched domain.NetworkObject
	if err := json.Unmarshal(rr.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fetched.Name != "vpc-a" || fetched.CIDR != "10.90.0.0/16" {
		t.Fatalf("unexpected object: %+v", fetched)
	}

	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPatch, "/api/v1/network/objects/"+itoa(objA.ID), `{"name":"vpc-a-renamed"}`), http.StatusOK)
	var patched domain.NetworkObject
	if err := json.Unmarshal(rr.Body.Bytes(), &patched); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if patched.Name != "vpc-a-renamed" {
		t.Fatalf("name = %q, want vpc-a-renamed", patched.Name)
	}
	if !patched.UpdatedAt.After(time.Time{}) {
		t.Fatal("updated_at should be set")
	}
}

func TestNetworkRelationshipsCov(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()
	mux := discSrv.srv.mux

	t.Run("method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodDelete, "/api/v1/network/relationships", ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != "GET, POST" {
			t.Fatalf("Allow = %q", allow)
		}
	})

	t.Run("create malformed json", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/network/relationships", `{`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid request body" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	var relID string
	t.Run("create and list", func(t *testing.T) {
		body := `{"id":"rel-1","type":"matches","source_kind":"discovered","source_id":"d-1","target_kind":"pool","target_id":"7","confidence":1,"resolution_state":"open"}`
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/network/relationships", body), http.StatusCreated)
		var rel domain.NetworkRelationship
		if err := json.Unmarshal(rr.Body.Bytes(), &rel); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if rel.ID != "rel-1" || rel.Type != domain.NetworkRelationshipMatches {
			t.Fatalf("unexpected relationship: %+v", rel)
		}
		relID = rel.ID

		rr = assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/network/relationships", ""), http.StatusOK)
		var resp domain.NetworkRelationshipListResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Total != 1 || resp.Items[0].ID != "rel-1" {
			t.Fatalf("unexpected list: %+v", resp)
		}

		rr = assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/network/relationships?type=conflicts", ""), http.StatusOK)
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Total != 0 {
			t.Fatalf("type filter returned %+v", resp)
		}
	})

	t.Run("subroutes not found", func(t *testing.T) {
		for _, path := range []string{
			"/api/v1/network/relationships/",
			"/api/v1/network/relationships/rel-1",
			"/api/v1/network/relationships/rel-1/other",
			"/api/v1/network/relationships/rel-1/resolve/extra",
		} {
			rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, path, `{}`), http.StatusNotFound)
			if e := decodeErrCov(t, rr); e.Error != "not found" {
				t.Fatalf("%s: error = %q", path, e.Error)
			}
		}
	})

	t.Run("resolve by id", func(t *testing.T) {
		path := "/api/v1/network/relationships/" + relID + "/resolve"

		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, path, ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != http.MethodPost {
			t.Fatalf("Allow = %q", allow)
		}

		rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPost, path, `{`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid request body" {
			t.Fatalf("error = %q", e.Error)
		}

		rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPost, path, `{"resolution_state":"resolved","reason":"linked"}`), http.StatusOK)
		var rel domain.NetworkRelationship
		if err := json.Unmarshal(rr.Body.Bytes(), &rel); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if rel.ResolutionState != "resolved" || rel.Reason != "linked" {
			t.Fatalf("unexpected resolved relationship: %+v", rel)
		}
	})

	t.Run("resolve by body", func(t *testing.T) {
		path := "/api/v1/network/relationships/resolve"

		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, path, ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != http.MethodPost {
			t.Fatalf("Allow = %q", allow)
		}

		rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPost, path, `{`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid request body" {
			t.Fatalf("error = %q", e.Error)
		}

		rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPost, path, `{"resolution_state":"resolved"}`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "id is required" {
			t.Fatalf("error = %q", e.Error)
		}

		rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPost, path, `{"id":"missing","resolution_state":"resolved"}`), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "network relationship not found" {
			t.Fatalf("error = %q", e.Error)
		}

		rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPost, path, `{"id":"`+relID+`","resolution_state":"ignored"}`), http.StatusOK)
		var rel domain.NetworkRelationship
		if err := json.Unmarshal(rr.Body.Bytes(), &rel); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if rel.ResolutionState != "ignored" {
			t.Fatalf("resolution_state = %q, want ignored", rel.ResolutionState)
		}
	})
}
