package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"cloudpam/internal/auth"
)

// exportPermTestHandler reports whether the wrapped handler was reached.
func exportPermTestHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireExportPermissionsMiddleware(t *testing.T) {
	poolsOnly := &auth.APIKey{ID: "pools-only", Scopes: []string{"pools:read"}}
	accountsOnly := &auth.APIKey{ID: "accounts-only", Scopes: []string{"accounts:read"}}
	both := &auth.APIKey{ID: "both", Scopes: []string{"pools:read", "accounts:read"}}

	tests := []struct {
		name        string
		key         *auth.APIKey
		datasets    string
		wantStatus  int
		wantHandler bool
	}{
		{"pools key exports pools", poolsOnly, "pools", http.StatusOK, true},
		{"pools key cannot export accounts", poolsOnly, "accounts", http.StatusForbidden, false},
		{"pools key cannot export blocks", poolsOnly, "blocks", http.StatusForbidden, false},
		{"pools key cannot export accounts alongside pools", poolsOnly, "pools,accounts", http.StatusForbidden, false},
		{"accounts key exports accounts", accountsOnly, "accounts", http.StatusOK, true},
		{"accounts key cannot export pools", accountsOnly, "pools", http.StatusForbidden, false},
		{"key with both scopes exports everything", both, "accounts,pools,blocks", http.StatusOK, true},
		{"unknown datasets reach the handler for a 400", poolsOnly, "bogus", http.StatusOK, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			wrapped := RequireExportPermissionsMiddleware(newTestLogger())(exportPermTestHandler(&called))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/export?datasets="+tt.datasets, nil)
			req = req.WithContext(auth.ContextWithAPIKey(req.Context(), tt.key))

			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
			if called != tt.wantHandler {
				t.Errorf("handler called = %v, want %v", called, tt.wantHandler)
			}
		})
	}
}

func TestRequireExportPermissionsMiddlewareAllowsAdminRole(t *testing.T) {
	var called bool
	wrapped := RequireExportPermissionsMiddleware(newTestLogger())(exportPermTestHandler(&called))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/export?datasets=accounts,pools,blocks", nil)
	req = req.WithContext(auth.ContextWithRole(req.Context(), auth.RoleAdmin))

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !called {
		t.Error("handler should have been called")
	}
}

func TestRequireExportPermissionsMiddlewareDeniesUnauthenticated(t *testing.T) {
	var called bool
	wrapped := RequireExportPermissionsMiddleware(newTestLogger())(exportPermTestHandler(&called))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/export?datasets=pools", nil)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
	if called {
		t.Error("handler should not have been called")
	}
}

func TestExportRequiredResources(t *testing.T) {
	tests := []struct {
		datasets string
		want     []string
	}{
		{"", nil},
		{"pools", []string{auth.ResourcePools}},
		{"accounts", []string{auth.ResourceAccounts}},
		{"blocks", []string{auth.ResourceAccounts, auth.ResourcePools}},
		{"accounts,pools", []string{auth.ResourceAccounts, auth.ResourcePools}},
	}

	for _, tt := range tests {
		t.Run(tt.datasets, func(t *testing.T) {
			got := exportRequiredResources(parseExportDatasets(tt.datasets))
			if len(got) != len(tt.want) {
				t.Fatalf("resources = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("resources = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
