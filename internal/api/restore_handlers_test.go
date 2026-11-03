package api

import (
	"encoding/json"
	stdhttp "net/http"
	"testing"

	"cloudpam/internal/audit"
	"cloudpam/internal/domain"
)

func TestRestorePool_ListsAndRestoresWithAudit(t *testing.T) {
	mux, st, key, auditLogger := setupProtectedTestServerWithAudit(t)
	p, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "prod", CIDR: "10.20.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	if rr := doProtected(t, mux, stdhttp.MethodDelete, "/api/v1/pools/"+itoa(p.ID), "", key); rr.Code != stdhttp.StatusNoContent {
		t.Fatalf("delete: %d %s", rr.Code, rr.Body.String())
	}

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools/deleted", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("list deleted: %d %s", rr.Code, rr.Body.String())
	}
	var deleted []domain.Pool
	if err := json.Unmarshal(rr.Body.Bytes(), &deleted); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].ID != p.ID || deleted[0].DeletedAt == nil {
		t.Fatalf("deleted pools = %+v, want pool %d with deleted_at", deleted, p.ID)
	}

	rr = doProtected(t, mux, stdhttp.MethodPost, "/api/v1/pools/"+itoa(p.ID)+"/restore", "", key)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("restore: %d %s", rr.Code, rr.Body.String())
	}
	if rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools/"+itoa(p.ID), "", key); rr.Code != stdhttp.StatusOK {
		t.Fatalf("get restored pool: %d %s", rr.Code, rr.Body.String())
	}
	events, total, err := auditLogger.List(t.Context(), audit.ListOptions{Action: audit.ActionRestore, ResourceType: audit.ResourcePool, ResourceID: itoa(p.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || events[0].ResourceName != "prod" {
		t.Fatalf("restore audit events = %d %+v, want one for prod", total, events)
	}

	if rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/pools/"+itoa(p.ID)+"/restore", "", key); rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("restore live pool: %d, want 404", rr.Code)
	}
	if rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/pools/999/restore", "", key); rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("restore unknown pool: %d, want 404", rr.Code)
	}
	if rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools/"+itoa(p.ID)+"/restore", "", key); rr.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("GET restore: %d, want 405", rr.Code)
	}
}

func TestRestorePool_ConflictsAreRejected(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, mux *stdhttp.ServeMux, key string) int64
	}{
		{"cidr overlaps a live sibling", func(t *testing.T, mux *stdhttp.ServeMux, key string) int64 {
			small := createPoolVia(t, mux, key, `{"name":"small","cidr":"10.30.1.0/24"}`)
			deletePoolVia(t, mux, key, small, "")
			createPoolVia(t, mux, key, `{"name":"big","cidr":"10.30.0.0/16"}`)
			return small
		}},
		{"same cidr is live again", func(t *testing.T, mux *stdhttp.ServeMux, key string) int64 {
			first := createPoolVia(t, mux, key, `{"name":"first","cidr":"10.31.0.0/16"}`)
			deletePoolVia(t, mux, key, first, "")
			createPoolVia(t, mux, key, `{"name":"second","cidr":"10.31.0.0/16"}`)
			return first
		}},
		{"parent is still deleted", func(t *testing.T, mux *stdhttp.ServeMux, key string) int64 {
			root := createPoolVia(t, mux, key, `{"name":"root","cidr":"10.32.0.0/16"}`)
			child := createPoolVia(t, mux, key, `{"name":"child","cidr":"10.32.1.0/24","parent_id":`+itoa(root)+`}`)
			deletePoolVia(t, mux, key, root, "?force=true")
			return child
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux, _, key := setupProtectedTestServer(t)
			id := tc.setup(t, mux, key)
			rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/pools/"+itoa(id)+"/restore", "", key)
			if rr.Code != stdhttp.StatusConflict {
				t.Fatalf("restore: %d %s, want 409", rr.Code, rr.Body.String())
			}
			if rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/pools/"+itoa(id), "", key); rr.Code != stdhttp.StatusNotFound {
				t.Fatalf("pool %d is live after a rejected restore: %d", id, rr.Code)
			}
		})
	}
}

func TestRestoreAccount_RestoresWithAuditAndRejectsTakenKey(t *testing.T) {
	mux, st, key, auditLogger := setupProtectedTestServerWithAudit(t)
	a, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:222222222222", Name: "Prod"})
	if err != nil {
		t.Fatal(err)
	}
	if rr := doProtected(t, mux, stdhttp.MethodDelete, "/api/v1/accounts/"+itoa(a.ID), "", key); rr.Code != stdhttp.StatusNoContent {
		t.Fatalf("delete: %d %s", rr.Code, rr.Body.String())
	}

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/accounts/deleted", "", key)
	var deleted []domain.Account
	if err := json.Unmarshal(rr.Body.Bytes(), &deleted); err != nil || rr.Code != stdhttp.StatusOK {
		t.Fatalf("list deleted: %d %s", rr.Code, rr.Body.String())
	}
	if len(deleted) != 1 || deleted[0].ID != a.ID || deleted[0].DeletedAt == nil {
		t.Fatalf("deleted accounts = %+v, want account %d with deleted_at", deleted, a.ID)
	}

	if rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/accounts/"+itoa(a.ID)+"/restore", "", key); rr.Code != stdhttp.StatusOK {
		t.Fatalf("restore: %d %s", rr.Code, rr.Body.String())
	}
	if rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/accounts/"+itoa(a.ID), "", key); rr.Code != stdhttp.StatusOK {
		t.Fatalf("get restored account: %d", rr.Code)
	}
	if _, total, _ := auditLogger.List(t.Context(), audit.ListOptions{Action: audit.ActionRestore, ResourceType: audit.ResourceAccount, ResourceID: itoa(a.ID)}); total != 1 {
		t.Fatalf("restore audit events = %d, want 1", total)
	}
	if rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/accounts/"+itoa(a.ID)+"/restore", "", key); rr.Code != stdhttp.StatusNotFound {
		t.Fatalf("restore live account: %d, want 404", rr.Code)
	}

	if rr := doProtected(t, mux, stdhttp.MethodDelete, "/api/v1/accounts/"+itoa(a.ID), "", key); rr.Code != stdhttp.StatusNoContent {
		t.Fatalf("delete again: %d", rr.Code)
	}
	if rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/accounts", `{"key":"aws:222222222222","name":"Replacement"}`, key); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("create replacement: %d %s", rr.Code, rr.Body.String())
	}
	if rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/accounts/"+itoa(a.ID)+"/restore", "", key); rr.Code != stdhttp.StatusConflict {
		t.Fatalf("restore over a taken key: %d %s, want 409", rr.Code, rr.Body.String())
	}
}

func TestRestore_RequiresDeletePermission(t *testing.T) {
	mux, _, key := setupViewerProtectedTestServer(t)
	for _, req := range []struct{ method, path string }{
		{stdhttp.MethodGet, "/api/v1/pools/deleted"},
		{stdhttp.MethodPost, "/api/v1/pools/1/restore"},
		{stdhttp.MethodGet, "/api/v1/accounts/deleted"},
		{stdhttp.MethodPost, "/api/v1/accounts/1/restore"},
	} {
		if rr := doProtected(t, mux, req.method, req.path, "", key); rr.Code != stdhttp.StatusForbidden {
			t.Errorf("%s %s as viewer: %d, want 403", req.method, req.path, rr.Code)
		}
	}
}

func createPoolVia(t *testing.T, mux *stdhttp.ServeMux, key, body string) int64 {
	t.Helper()
	rr := doProtected(t, mux, stdhttp.MethodPost, "/api/v1/pools", body, key)
	if rr.Code != stdhttp.StatusCreated {
		t.Fatalf("create %s: %d %s", body, rr.Code, rr.Body.String())
	}
	var p domain.Pool
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func deletePoolVia(t *testing.T, mux *stdhttp.ServeMux, key string, id int64, query string) {
	t.Helper()
	if rr := doProtected(t, mux, stdhttp.MethodDelete, "/api/v1/pools/"+itoa(id)+query, "", key); rr.Code != stdhttp.StatusNoContent {
		t.Fatalf("delete pool %d: %d %s", id, rr.Code, rr.Body.String())
	}
}
