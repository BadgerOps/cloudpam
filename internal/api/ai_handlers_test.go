package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

func TestProtectedAIPlanningApplyPlanRequiresPoolsCreate(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	roleStore := auth.NewMemoryRoleStore()
	requireCreateRole(t, roleStore, "ai_update_only", []auth.Permission{
		{Resource: auth.ResourcePools, Action: auth.ActionUpdate},
	})
	requireCreateRole(t, roleStore, "ai_create_only", []auth.Permission{
		{Resource: auth.ResourcePools, Action: auth.ActionCreate},
	})
	auth.SetRoleStoreProvider(roleStore)
	t.Cleanup(func() { auth.SetRoleStoreProvider(nil) })

	t.Run("update only is denied", func(t *testing.T) {
		mux, _ := setupProtectedAIPlanningTestServer(logger)
		req := newApplyPlanRequest(t, auth.Role("ai_update_only"))
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("create only is allowed", func(t *testing.T) {
		mux, st := setupProtectedAIPlanningTestServer(logger)
		req := newApplyPlanRequest(t, auth.Role("ai_create_only"))
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		pools, err := st.ListPools(t.Context())
		if err != nil {
			t.Fatalf("list pools: %v", err)
		}
		if len(pools) != 1 {
			t.Fatalf("expected one pool to be created, got %d", len(pools))
		}
	})
}

func setupProtectedAIPlanningTestServer(logger *slog.Logger) (*http.ServeMux, storage.Store) {
	st := storage.NewMemoryStore()
	mux := http.NewServeMux()
	obsLogger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, st, obsLogger, nil, nil)
	ai := NewAIPlanningServer(srv, nil, nil)
	ai.RegisterProtectedAIPlanningRoutes(func(next http.Handler) http.Handler { return next }, logger)
	return mux, st
}

func requireCreateRole(t *testing.T, store auth.RoleStore, name string, permissions []auth.Permission) {
	t.Helper()
	if err := store.CreateRole(t.Context(), &auth.RoleDefinition{
		Name:        auth.Role(name),
		Description: name,
		Permissions: permissions,
	}); err != nil {
		t.Fatalf("create role %s: %v", name, err)
	}
}

func newApplyPlanRequest(t *testing.T, role auth.Role) *http.Request {
	t.Helper()
	body := `{
  "plan": {
    "pools": [
      {"ref": "root", "name": "AI Root", "cidr": "10.0.0.0/8", "type": "aggregate"}
    ]
  }
}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/sessions/session-1/apply-plan", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(auth.ContextWithRole(req.Context(), role))
}
