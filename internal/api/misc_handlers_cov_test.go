package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
)

// --- role handlers ---

func TestRoleHandlersReadPathsCov(t *testing.T) {
	srv, roleStore, _ := setupRoleTestServer(t)

	t.Run("list permissions", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/auth/permissions", ""), http.StatusOK)
		var resp struct {
			Permissions []auth.PermissionDefinition `json:"permissions"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp.Permissions) != len(auth.PermissionCatalog()) {
			t.Fatalf("len(permissions) = %d, want the full catalog (%d)", len(resp.Permissions), len(auth.PermissionCatalog()))
		}
	})

	t.Run("list roles includes builtins", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/auth/roles", ""), http.StatusOK)
		var resp struct {
			Roles []*auth.RoleDefinition `json:"roles"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		names := map[auth.Role]bool{}
		for _, r := range resp.Roles {
			names[r.Name] = true
		}
		for _, want := range []auth.Role{auth.RoleAdmin, auth.RoleOperator, auth.RoleViewer} {
			if !names[want] {
				t.Fatalf("builtin role %q missing from listing", want)
			}
		}
	})

	t.Run("get builtin role", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/auth/roles/admin", ""), http.StatusOK)
		var role auth.RoleDefinition
		if err := json.Unmarshal(rr.Body.Bytes(), &role); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if role.Name != auth.RoleAdmin || len(role.Permissions) == 0 {
			t.Fatalf("unexpected role: %+v", role)
		}
	})

	t.Run("get unknown role", func(t *testing.T) {
		assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/auth/roles/nosuchrole", ""), http.StatusNotFound)
	})

	_ = roleStore
}

func TestRoleHandlersRoutingErrorsCov(t *testing.T) {
	srv, _, _ := setupRoleTestServer(t)

	t.Run("collection method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodDelete, "/api/v1/auth/roles", ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != "GET, POST" {
			t.Fatalf("Allow = %q", allow)
		}
	})

	t.Run("empty role name", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/auth/roles/", ""), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("item method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPut, "/api/v1/auth/roles/admin", ""), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != "GET, PATCH, DELETE" {
			t.Fatalf("Allow = %q", allow)
		}
	})
}

func TestRoleHandlersCreateValidationCov(t *testing.T) {
	srv, _, _ := setupRoleTestServer(t)

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantErr  string
	}{
		{"malformed json", `{`, http.StatusBadRequest, "invalid json"},
		{"missing name", `{"permissions":["pools:read"]}`, http.StatusBadRequest, "name is required"},
		{"blank name", `{"name":"   "}`, http.StatusBadRequest, "name is required"},
		{"invalid permission", `{"name":"custom","permissions":["pools:teleport"]}`, http.StatusBadRequest, "invalid permission"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/roles", tc.body), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}

	t.Run("duplicate permissions are de-duplicated", func(t *testing.T) {
		body := `{"name":"dedupe-role","description":"  dedupe  ","permissions":["pools:read","pools:read","pools:list"]}`
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/roles", body), http.StatusCreated)
		var role auth.RoleDefinition
		if err := json.Unmarshal(rr.Body.Bytes(), &role); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(role.Permissions) != 2 {
			t.Fatalf("len(permissions) = %d, want 2 after de-duplication: %+v", len(role.Permissions), role.Permissions)
		}
		if role.Description != "dedupe" {
			t.Fatalf("description = %q, want trimmed", role.Description)
		}
	})

	t.Run("duplicate role name conflicts", func(t *testing.T) {
		body := `{"name":"dedupe-role","permissions":["pools:read"]}`
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/roles", body), http.StatusConflict)
		if e := decodeErrCov(t, rr); e.Error == "" {
			t.Fatal("expected a conflict message")
		}
	})

	t.Run("builtin role cannot be created", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/roles",
			`{"name":"admin","permissions":["pools:read"]}`), http.StatusConflict)
		if e := decodeErrCov(t, rr); e.Error == "" {
			t.Fatal("expected a conflict message")
		}
	})
}

func TestRoleHandlersUpdateDeleteErrorsCov(t *testing.T) {
	srv, _, _ := setupRoleTestServer(t)

	t.Run("update malformed json", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPatch, "/api/v1/auth/roles/admin", `{`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid json" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("update invalid permission", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPatch, "/api/v1/auth/roles/admin",
			`{"permissions":["not-a-permission"]}`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid permission" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("update unknown role", func(t *testing.T) {
		assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPatch, "/api/v1/auth/roles/ghost-role",
			`{"permissions":["pools:read"]}`), http.StatusNotFound)
	})

	t.Run("builtin role cannot be updated or deleted", func(t *testing.T) {
		assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPatch, "/api/v1/auth/roles/admin",
			`{"permissions":["pools:read"]}`), http.StatusConflict)
		assertStatusCov(t, doReqCov(t, srv.mux, http.MethodDelete, "/api/v1/auth/roles/admin", ""), http.StatusConflict)
	})

	t.Run("delete unknown role", func(t *testing.T) {
		assertStatusCov(t, doReqCov(t, srv.mux, http.MethodDelete, "/api/v1/auth/roles/ghost-role", ""), http.StatusNotFound)
	})
}

// --- settings handlers ---

func TestSecuritySettingsValidationCov(t *testing.T) {
	mux := setupSettingsServer()

	base := domain.DefaultSecuritySettings()
	patch := func(mutate func(s *domain.SecuritySettings)) string {
		s := base
		mutate(&s)
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"malformed json", `{`, "invalid request body"},
		{"session duration too small", patch(func(s *domain.SecuritySettings) { s.SessionDurationHours = 0 }), "invalid session_duration_hours"},
		{"session duration too large", patch(func(s *domain.SecuritySettings) { s.SessionDurationHours = 721 }), "invalid session_duration_hours"},
		{"max sessions too small", patch(func(s *domain.SecuritySettings) { s.MaxSessionsPerUser = 0 }), "invalid max_sessions_per_user"},
		{"max sessions too large", patch(func(s *domain.SecuritySettings) { s.MaxSessionsPerUser = 101 }), "invalid max_sessions_per_user"},
		{"password min too small", patch(func(s *domain.SecuritySettings) { s.PasswordMinLength = 7 }), "invalid password_min_length"},
		{"password max below min", patch(func(s *domain.SecuritySettings) { s.PasswordMaxLength = 8; s.PasswordMinLength = 20 }), "invalid password_max_length"},
		{"login rate limit too small", patch(func(s *domain.SecuritySettings) { s.LoginRateLimitPerMin = 0 }), "invalid login_rate_limit_per_minute"},
		{"login rate limit too large", patch(func(s *domain.SecuritySettings) { s.LoginRateLimitPerMin = 61 }), "invalid login_rate_limit_per_minute"},
		{"lockout attempts too large", patch(func(s *domain.SecuritySettings) { s.AccountLockoutAttempts = 101 }), "invalid account_lockout_attempts"},
		{"lockout cooldown too large", patch(func(s *domain.SecuritySettings) { s.AccountLockoutCooldownMinutes = 1441 }), "invalid account_lockout_cooldown_minutes"},
		{"api key expiry too large", patch(func(s *domain.SecuritySettings) { s.APIKeyDefaultExpiryDays = 3651 }), "invalid api_key_default_expiry_days"},
		{"api key lifetime too large", patch(func(s *domain.SecuritySettings) { s.APIKeyMaxLifetimeDays = 3651 }), "invalid api_key_max_lifetime_days"},
		{"expiry beyond lifetime", patch(func(s *domain.SecuritySettings) {
			s.APIKeyMaxLifetimeDays = 30
			s.APIKeyDefaultExpiryDays = 90
		}), "invalid api_key_default_expiry_days"},
		{"rotation reminder too large", patch(func(s *domain.SecuritySettings) { s.APIKeyRotationReminderDays = 366 }), "invalid api_key_rotation_reminder_days"},
		{"unknown role in scope policy", patch(func(s *domain.SecuritySettings) {
			s.APIKeyAllowedScopesByRole = map[string][]string{"wizard": {"pools:read"}}
		}), "invalid api_key_allowed_scopes_by_role"},
		{"invalid scope in scope policy", patch(func(s *domain.SecuritySettings) {
			s.APIKeyAllowedScopesByRole = map[string][]string{"viewer": {"pools:teleport"}}
		}), "invalid api_key_allowed_scopes_by_role"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPatch, "/api/v1/settings/security", tc.body), http.StatusBadRequest)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}

	t.Run("zero cooldown falls back to the default", func(t *testing.T) {
		body := patch(func(s *domain.SecuritySettings) { s.AccountLockoutCooldownMinutes = 0 })
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPatch, "/api/v1/settings/security", body), http.StatusOK)
		var got domain.SecuritySettings
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.AccountLockoutCooldownMinutes != base.AccountLockoutCooldownMinutes {
			t.Fatalf("cooldown = %d, want the default %d", got.AccountLockoutCooldownMinutes, base.AccountLockoutCooldownMinutes)
		}
	})
}

func TestNetworkSchemaPolicySettingsCov(t *testing.T) {
	mux := setupSettingsServer()

	rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/settings/network-schema-policy", ""), http.StatusOK)
	var policy domain.NetworkSchemaPolicy
	if err := json.Unmarshal(rr.Body.Bytes(), &policy); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if policy.Name == "" {
		t.Fatalf("expected a default policy, got %+v", policy)
	}

	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPatch, "/api/v1/settings/network-schema-policy", `{`), http.StatusBadRequest)
	if e := decodeErrCov(t, rr); e.Error != "invalid request body" {
		t.Fatalf("error = %q", e.Error)
	}

	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPatch, "/api/v1/settings/network-schema-policy",
		`{"name":"bogus","ownership_strategy":"chaos","duplicate_scope":"chaos","hierarchy_scope":"chaos"}`), http.StatusBadRequest)
	if e := decodeErrCov(t, rr); e.Error != "invalid network schema policy" {
		t.Fatalf("error = %q", e.Error)
	}

	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPatch, "/api/v1/settings/network-schema-policy",
		`{"name":"global","ownership_strategy":"global","duplicate_scope":"global","hierarchy_scope":"global"}`), http.StatusOK)
	var updated domain.NetworkSchemaPolicy
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if updated.DuplicateScope != "global" {
		t.Fatalf("duplicate_scope = %q, want global", updated.DuplicateScope)
	}

	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/settings/network-schema-policy", ""), http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &policy); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if policy.DuplicateScope != "global" {
		t.Fatalf("policy not persisted: %+v", policy)
	}
}

// --- drift handlers ---

func TestDriftHandlersErrorPathsCov(t *testing.T) {
	mux, _, _, _ := setupDriftTestServer()

	t.Run("detect method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/drift/detect", ""), http.StatusMethodNotAllowed)
		if e := decodeErrCov(t, rr); e.Error != "method not allowed" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("detect tolerates a malformed body", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/drift/detect", `{`), http.StatusOK)
		var resp domain.RunDriftDetectionResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
	})

	t.Run("list method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/drift", `{}`), http.StatusMethodNotAllowed)
		if e := decodeErrCov(t, rr); e.Error != "method not allowed" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("list invalid account_id", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/drift?account_id=abc", ""), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid account_id" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("list pagination defaults", func(t *testing.T) {
		for _, q := range []string{"", "?page=0&page_size=0", "?page=-2&page_size=-5", "?page=abc&page_size=abc"} {
			rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/drift"+q, ""), http.StatusOK)
			var resp domain.DriftListResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp.Page != 1 || resp.PageSize != 50 {
				t.Fatalf("%q: page/page_size = %d/%d, want 1/50", q, resp.Page, resp.PageSize)
			}
			if resp.Summary.BySeverity == nil || resp.Summary.ByType == nil {
				t.Fatalf("%q: summary maps must be initialised", q)
			}
		}
	})

	t.Run("by id routing", func(t *testing.T) {
		cases := []struct {
			method   string
			path     string
			wantCode int
			wantErr  string
		}{
			{http.MethodGet, "/api/v1/drift/", http.StatusBadRequest, "drift item id is required"},
			{http.MethodPut, "/api/v1/drift/some-id", http.StatusMethodNotAllowed, "method not allowed"},
			{http.MethodGet, "/api/v1/drift/some-id/resolve", http.StatusMethodNotAllowed, "method not allowed"},
			{http.MethodPost, "/api/v1/drift/some-id/unknown", http.StatusMethodNotAllowed, "method not allowed"},
		}
		for _, tc := range cases {
			rr := assertStatusCov(t, doReqCov(t, mux, tc.method, tc.path, ""), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("%s %s: error = %q, want %q", tc.method, tc.path, e.Error, tc.wantErr)
			}
		}
	})

	t.Run("unknown drift item", func(t *testing.T) {
		id := uuid.New().String()
		assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/drift/"+id, ""), http.StatusNotFound)
		assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/drift/"+id+"/resolve", ""), http.StatusNotFound)
		assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/drift/"+id+"/ignore", `{`), http.StatusNotFound)
	})
}

func TestDriftResolveAndIgnoreCov(t *testing.T) {
	mux, ms, _, driftStore := setupDriftTestServer()
	acct, err := ms.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "a", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	now := time.Now().UTC()
	seed := func(id string) {
		resourceID := uuid.New()
		if err := driftStore.CreateDriftItem(t.Context(), domain.DriftItem{
			ID:           id,
			AccountID:    acct.ID,
			ResourceID:   &resourceID,
			Type:         domain.DriftTypeUnmanaged,
			Severity:     domain.DriftSeverityWarning,
			Status:       domain.DriftStatusOpen,
			Title:        "unmanaged " + id,
			ResourceCIDR: "10.0.0.0/16",
			DetectedAt:   now,
			UpdatedAt:    now,
		}); err != nil {
			t.Fatalf("seed drift item: %v", err)
		}
	}
	seed("drift-resolve")
	seed("drift-ignore")

	rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/drift/drift-resolve/resolve", ""), http.StatusOK)
	var resolved domain.DriftItem
	if err := json.Unmarshal(rr.Body.Bytes(), &resolved); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resolved.Status != domain.DriftStatusResolved {
		t.Fatalf("status = %q, want resolved", resolved.Status)
	}

	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/drift/drift-ignore/ignore", `{"reason":"known good"}`), http.StatusOK)
	var ignored domain.DriftItem
	if err := json.Unmarshal(rr.Body.Bytes(), &ignored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ignored.Status != domain.DriftStatusIgnored {
		t.Fatalf("status = %q, want ignored", ignored.Status)
	}

	// The single-item GET reflects the stored state.
	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/drift/drift-ignore", ""), http.StatusOK)
	var fetched domain.DriftItem
	if err := json.Unmarshal(rr.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fetched.Status != domain.DriftStatusIgnored {
		t.Fatalf("status = %q, want ignored", fetched.Status)
	}

	// Filtering by status narrows the listing.
	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/drift?status=resolved", ""), http.StatusOK)
	var list domain.DriftListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if list.Total != 1 || list.Items[0].ID != "drift-resolve" {
		t.Fatalf("status filter returned %+v", list)
	}
}

// --- recommendation handlers ---

func TestRecommendationHandlersErrorPathsCov(t *testing.T) {
	mux, _ := setupRecommendationServer()

	t.Run("generate method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/recommendations/generate", ""), http.StatusMethodNotAllowed)
		if e := decodeErrCov(t, rr); e.Error != "method not allowed" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("generate malformed json", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/recommendations/generate", `{`), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid request body" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("list method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodDelete, "/api/v1/recommendations", ""), http.StatusMethodNotAllowed)
		if e := decodeErrCov(t, rr); e.Error != "method not allowed" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("list invalid pool_id", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/recommendations?pool_id=abc", ""), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid pool_id" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("list pagination defaults", func(t *testing.T) {
		for _, q := range []string{"", "?page=0&page_size=0", "?page=-1&page_size=-1", "?page=xyz&page_size=xyz"} {
			rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/recommendations"+q, ""), http.StatusOK)
			var resp domain.RecommendationsListResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp.Page != 1 || resp.PageSize != 50 {
				t.Fatalf("%q: page/page_size = %d/%d, want 1/50", q, resp.Page, resp.PageSize)
			}
		}
	})

	t.Run("by id routing", func(t *testing.T) {
		cases := []struct {
			method   string
			path     string
			wantCode int
			wantErr  string
		}{
			{http.MethodGet, "/api/v1/recommendations/", http.StatusBadRequest, "recommendation id is required"},
			{http.MethodPut, "/api/v1/recommendations/rec-1", http.StatusMethodNotAllowed, "method not allowed"},
			{http.MethodGet, "/api/v1/recommendations/rec-1/apply", http.StatusMethodNotAllowed, "method not allowed"},
			{http.MethodPost, "/api/v1/recommendations/rec-1/unknown", http.StatusMethodNotAllowed, "method not allowed"},
		}
		for _, tc := range cases {
			rr := assertStatusCov(t, doReqCov(t, mux, tc.method, tc.path, ""), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("%s %s: error = %q, want %q", tc.method, tc.path, e.Error, tc.wantErr)
			}
		}
	})

	t.Run("unknown recommendation", func(t *testing.T) {
		assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/recommendations/missing", ""), http.StatusNotFound)
		assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/recommendations/missing/apply", `{`), http.StatusNotFound)
		assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/recommendations/missing/dismiss", `{`), http.StatusNotFound)
	})
}

func TestRecommendationApplyAndDismissCov(t *testing.T) {
	mux, st := setupRecommendationServer()
	pool, err := st.CreatePool(t.Context(), domain.CreatePool{
		Name: "Net", CIDR: "10.120.0.0/16", Type: domain.PoolTypeSupernet, Description: "test",
	})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/recommendations/generate",
		`{"pool_ids":[`+itoa(pool.ID)+`]}`), http.StatusOK)
	var gen domain.GenerateRecommendationsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &gen); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gen.Total == 0 {
		t.Fatal("expected at least one recommendation to be generated")
	}

	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/recommendations", ""), http.StatusOK)
	var list domain.RecommendationsListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list.Items) == 0 {
		t.Fatal("expected recommendations to be listed")
	}
	target := list.Items[0]

	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/recommendations/"+target.ID, ""), http.StatusOK)
	var got domain.Recommendation
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != target.ID {
		t.Fatalf("id = %q, want %q", got.ID, target.ID)
	}

	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/recommendations/"+target.ID+"/dismiss",
		`{"reason":"not needed"}`), http.StatusOK)
	var dismissed domain.Recommendation
	if err := json.Unmarshal(rr.Body.Bytes(), &dismissed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dismissed.Status != domain.RecommendationStatusDismissed {
		t.Fatalf("status = %q, want dismissed", dismissed.Status)
	}

	// Filtering by status reflects the dismissal.
	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/recommendations?status=dismissed", ""), http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if list.Total != 1 || list.Items[0].ID != target.ID {
		t.Fatalf("status filter returned %+v", list)
	}
}

// --- shared helper sanity ---

func TestKeysOfCovIsStableForEmptyMapCov(t *testing.T) {
	if got := keysOfCov(map[string][][]string{}); len(got) != 0 {
		t.Fatalf("keysOfCov(empty) = %v", got)
	}
	if got := keysOfCov(map[string][][]string{"a.csv": nil}); len(got) != 1 || !strings.HasSuffix(got[0], ".csv") {
		t.Fatalf("keysOfCov = %v", got)
	}
}
