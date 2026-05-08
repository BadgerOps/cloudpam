package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

func setupRoleTestServer(t *testing.T) (*Server, auth.RoleStore, auth.UserStore) {
	t.Helper()
	mux := http.NewServeMux()
	logger := observability.NewLogger(observability.Config{
		Level:  "info",
		Format: "json",
		Output: io.Discard,
	})
	srv := NewServer(mux, storage.NewMemoryStore(), logger, nil, nil)
	userStore := auth.NewMemoryUserStore()
	roleStore := auth.NewMemoryRoleStore(userStore)
	auth.SetRoleStoreProvider(roleStore)
	t.Cleanup(func() { auth.SetRoleStoreProvider(nil) })

	roleSrv := NewRoleServer(srv, roleStore)
	authMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.ContextWithRole(r.Context(), auth.RoleAdmin)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	roleSrv.RegisterProtectedRoleRoutes(authMW, logger.Slog())
	return srv, roleStore, userStore
}

func TestRoleHandlers_CreateUpdateAndDeleteCustomRole(t *testing.T) {
	srv, roleStore, _ := setupRoleTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/roles", strings.NewReader(`{
		"name": "network-operator",
		"description": "Network operator",
		"permissions": ["pools:read", "pools:list", "accounts:read"]
	}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var created auth.RoleDefinition
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal role: %v", err)
	}
	if created.Name != "network-operator" || created.IsBuiltin {
		t.Fatalf("unexpected role response: %+v", created)
	}
	if !auth.HasPermissionContext(context.Background(), created.Name, auth.ResourcePools, auth.ActionRead) {
		t.Fatalf("created role should resolve through dynamic permission provider")
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/auth/roles/network-operator", strings.NewReader(`{
		"description": "Read only",
		"permissions": ["pools:read"]
	}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	role, err := roleStore.GetRole(context.Background(), "network-operator")
	if err != nil {
		t.Fatalf("get role: %v", err)
	}
	if len(role.Permissions) != 1 || role.Permissions[0].String() != "pools:read" {
		t.Fatalf("unexpected permissions after update: %+v", role.Permissions)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/auth/roles/network-operator", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRoleHandlers_PreventBuiltinAndAssignedRoleDeletion(t *testing.T) {
	srv, roleStore, userStore := setupRoleTestServer(t)
	if err := roleStore.CreateRole(context.Background(), &auth.RoleDefinition{
		Name:        "assigned-role",
		Description: "Assigned",
		Permissions: []auth.Permission{
			{Resource: auth.ResourcePools, Action: auth.ActionRead},
		},
	}); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := userStore.Create(context.Background(), &auth.User{
		ID:           "user-1",
		Username:     "assigned-user",
		Role:         "assigned-role",
		PasswordHash: []byte("hash"),
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/roles/admin", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for built-in role delete, got %d: %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/auth/roles/assigned-role", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for assigned role delete, got %d: %s", rr.Code, rr.Body.String())
	}
}
