package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/discovery"
	"cloudpam/internal/observability"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
)

// roleHeaderMiddlewareCov stands in for the dual-auth middleware: it promotes
// the X-Test-Role header into the request context so the RBAC middleware under
// test sees an authenticated principal.
func roleHeaderMiddlewareCov(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if role := r.Header.Get("X-Test-Role"); role != "" {
			r = r.WithContext(auth.ContextWithRole(r.Context(), auth.Role(role)))
		}
		next.ServeHTTP(w, r)
	})
}

// setupProtectedFeatureRoutesCov registers every RBAC-protected feature router
// on a single mux so authorization can be asserted end to end.
func setupProtectedFeatureRoutesCov(t *testing.T) *http.ServeMux {
	t.Helper()

	st := storage.NewMemoryStore()
	mux := http.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	slogger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := NewServer(mux, st, logger, nil, audit.NewMemoryAuditLogger())

	ds := storage.NewMemoryDiscoveryStore(st)
	driftStore := storage.NewMemoryDriftStore(st)
	networkStore := storage.NewMemoryNetworkStore(st)

	discSrv := NewDiscoveryServer(srv, ds, discovery.NewSyncService(ds), auth.NewMemoryKeyStore())
	discSrv.SetNetworkStore(networkStore)
	discSrv.RegisterProtectedDiscoveryRoutes(roleHeaderMiddlewareCov, slogger)

	networkSrv := NewNetworkServer(srv, st, ds, driftStore)
	networkSrv.SetNetworkStore(networkStore)
	networkSrv.RegisterProtectedNetworkRoutes(roleHeaderMiddlewareCov, slogger)

	driftSrv := NewDriftServer(srv, discovery.NewDriftDetector(st, ds, driftStore), driftStore)
	driftSrv.RegisterProtectedDriftRoutes(roleHeaderMiddlewareCov, slogger)

	recStore := storage.NewMemoryRecommendationStore(st)
	analysisSvc := planning.NewAnalysisService(st)
	recSrv := NewRecommendationServer(srv, planning.NewRecommendationService(analysisSvc, recStore, st), recStore)
	recSrv.RegisterProtectedRecommendationRoutes(roleHeaderMiddlewareCov, slogger)

	// Keep the update endpoints hermetic: a local release API and a temp
	// control directory instead of GitHub and /var/lib.
	releaseAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"tag_name":     "v9.9.9",
			"body":         "test release",
			"html_url":     "https://example.test/releases/v9.9.9",
			"published_at": "2026-01-01T00:00:00Z",
			"draft":        false,
			"prerelease":   false,
		}})
	}))
	t.Cleanup(releaseAPI.Close)

	updateSrv := NewUpdateServer(srv)
	updateSrv.controlDir = t.TempDir()
	updateSrv.client = releaseAPI.Client()
	updateSrv.releasesURL = releaseAPI.URL
	updateSrv.RegisterProtectedUpdateRoutes(roleHeaderMiddlewareCov, slogger)

	return mux
}

func doRoleReqCov(t *testing.T, mux *http.ServeMux, method, path, body, role string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if role != "" {
		req.Header.Set("X-Test-Role", role)
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestProtectedFeatureRoutesAuthzCov(t *testing.T) {
	mux := setupProtectedFeatureRoutesCov(t)

	cases := []struct {
		name string
		// method/path/body identify the endpoint.
		method string
		path   string
		body   string
		// deniedRole must receive 403; allowedRole must not be rejected by RBAC.
		deniedRole  string
		allowedRole string
	}{
		{"discovery resources list", http.MethodGet, "/api/v1/discovery/resources?account_id=1", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"discovery resource by id", http.MethodGet, "/api/v1/discovery/resources/not-a-uuid", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"discovery resource link", http.MethodPost, "/api/v1/discovery/resources/not-a-uuid/link", `{}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"discovery import", http.MethodPost, "/api/v1/discovery/import", `{"account_id":1}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"discovery import preview", http.MethodPost, "/api/v1/discovery/import/preview", `{}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"discovery sync list", http.MethodGet, "/api/v1/discovery/sync?account_id=1", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"discovery sync trigger", http.MethodPost, "/api/v1/discovery/sync", `{}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"discovery sync job", http.MethodGet, "/api/v1/discovery/sync/not-a-uuid", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"discovery ingest", http.MethodPost, "/api/v1/discovery/ingest", `{}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"discovery org ingest", http.MethodPost, "/api/v1/discovery/ingest/org", `{}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"discovery agents list", http.MethodGet, "/api/v1/discovery/agents", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"discovery agent get", http.MethodGet, "/api/v1/discovery/agents/not-a-uuid", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"discovery agent provision", http.MethodPost, "/api/v1/discovery/agents/provision", `{}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"discovery agent register", http.MethodPost, "/api/v1/discovery/agents/register", `{}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"discovery agent heartbeat", http.MethodPost, "/api/v1/discovery/agents/heartbeat", `{}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"discovery agent approve", http.MethodPost, "/api/v1/discovery/agents/not-a-uuid/approve", `{}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"discovery agent delete", http.MethodDelete, "/api/v1/discovery/agents/not-a-uuid", "", string(auth.RoleViewer), string(auth.RoleAdmin)},

		{"network flat", http.MethodGet, "/api/v1/network/flat", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"network hierarchy", http.MethodGet, "/api/v1/network/hierarchy", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"network merged", http.MethodGet, "/api/v1/network/merged", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"network conflicts", http.MethodGet, "/api/v1/network/conflicts", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"network conflict resolve", http.MethodPost, "/api/v1/network/conflicts/c1/resolve", `{}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"network objects list", http.MethodGet, "/api/v1/network/objects", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"network objects create", http.MethodPost, "/api/v1/network/objects", `{`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"network object get", http.MethodGet, "/api/v1/network/objects/1", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"network object patch", http.MethodPatch, "/api/v1/network/objects/1", `{`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"network relationships list", http.MethodGet, "/api/v1/network/relationships", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"network relationships create", http.MethodPost, "/api/v1/network/relationships", `{`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"network relationship resolve", http.MethodPost, "/api/v1/network/relationships/resolve", `{`, string(auth.RoleViewer), string(auth.RoleOperator)},

		{"drift detect", http.MethodPost, "/api/v1/drift/detect", `{}`, string(auth.RoleViewer), string(auth.RoleOperator)},
		{"drift list", http.MethodGet, "/api/v1/drift", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"drift by id", http.MethodGet, "/api/v1/drift/missing", "", string(auth.RoleViewer), string(auth.RoleOperator)},

		{"recommendations generate", http.MethodPost, "/api/v1/recommendations/generate", `{}`, string(auth.RoleAuditor), string(auth.RoleOperator)},
		{"recommendations list", http.MethodGet, "/api/v1/recommendations", "", string(auth.RoleAuditor), string(auth.RoleViewer)},
		{"recommendation by id", http.MethodGet, "/api/v1/recommendations/missing", "", string(auth.RoleViewer), string(auth.RoleOperator)},

		{"updates check", http.MethodGet, "/api/v1/updates", "", string(auth.RoleOperator), string(auth.RoleAdmin)},
		{"updates status", http.MethodGet, "/api/v1/updates/status", "", string(auth.RoleOperator), string(auth.RoleAdmin)},
		{"updates status ack", http.MethodPost, "/api/v1/updates/status/ack", `{}`, string(auth.RoleOperator), string(auth.RoleAdmin)},
		{"updates upgrade", http.MethodPost, "/api/v1/updates/upgrade", `{}`, string(auth.RoleOperator), string(auth.RoleAdmin)},
	}

	for _, tc := range cases {
		t.Run(tc.name+" unauthenticated", func(t *testing.T) {
			rr := doRoleReqCov(t, mux, tc.method, tc.path, tc.body, "")
			assertStatusCov(t, rr, http.StatusUnauthorized)
			if e := decodeErrCov(t, rr); e.Error != "unauthorized" {
				t.Fatalf("error = %q, want unauthorized", e.Error)
			}
		})

		t.Run(tc.name+" under-privileged", func(t *testing.T) {
			rr := doRoleReqCov(t, mux, tc.method, tc.path, tc.body, tc.deniedRole)
			assertStatusCov(t, rr, http.StatusForbidden)
			if e := decodeErrCov(t, rr); e.Error != "forbidden" {
				t.Fatalf("error = %q, want forbidden", e.Error)
			}
		})

		t.Run(tc.name+" authorized", func(t *testing.T) {
			rr := doRoleReqCov(t, mux, tc.method, tc.path, tc.body, tc.allowedRole)
			if rr.Code == http.StatusUnauthorized || rr.Code == http.StatusForbidden {
				t.Fatalf("role %q should pass RBAC, got %d: %s", tc.allowedRole, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestProtectedOIDCAdminRoutesAuthzCov(t *testing.T) {
	env := setupOIDCTestEnv(t)
	env.oidcServer.RegisterOIDCAdminRoutes(roleHeaderMiddlewareCov, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	mux := env.oidcServer.mux

	cases := []struct {
		name        string
		method      string
		path        string
		body        string
		deniedRole  string
		allowedRole string
	}{
		{"list", http.MethodGet, "/api/v1/settings/oidc/providers", "", string(auth.RoleOperator), string(auth.RoleAdmin)},
		{"create", http.MethodPost, "/api/v1/settings/oidc/providers", `{}`, string(auth.RoleOperator), string(auth.RoleAdmin)},
		{"get", http.MethodGet, "/api/v1/settings/oidc/providers/" + env.providerID, "", string(auth.RoleOperator), string(auth.RoleAdmin)},
		{"update", http.MethodPatch, "/api/v1/settings/oidc/providers/" + env.providerID, `{}`, string(auth.RoleOperator), string(auth.RoleAdmin)},
		{"delete", http.MethodDelete, "/api/v1/settings/oidc/providers/missing", "", string(auth.RoleOperator), string(auth.RoleAdmin)},
		{"test", http.MethodPost, "/api/v1/settings/oidc/providers/missing/test", "", string(auth.RoleOperator), string(auth.RoleAdmin)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := doRoleReqCov(t, mux, tc.method, tc.path, tc.body, "")
			assertStatusCov(t, rr, http.StatusUnauthorized)

			rr = doRoleReqCov(t, mux, tc.method, tc.path, tc.body, tc.deniedRole)
			assertStatusCov(t, rr, http.StatusForbidden)

			rr = doRoleReqCov(t, mux, tc.method, tc.path, tc.body, tc.allowedRole)
			if rr.Code == http.StatusUnauthorized || rr.Code == http.StatusForbidden {
				t.Fatalf("admin should pass RBAC, got %d: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestUnprotectedAuthAndAIRouteRegistrationCov(t *testing.T) {
	st := storage.NewMemoryStore()
	mux := http.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, st, logger, nil, audit.NewMemoryAuditLogger())

	keyStore := auth.NewMemoryKeyStore()
	authSrv := NewAuthServerWithStores(srv, keyStore, auth.NewMemorySessionStore(), auth.NewMemoryUserStore(), audit.NewMemoryAuditLogger())
	authSrv.SetSettingsStore(storage.NewMemorySettingsStore())
	authSrv.RegisterAuthRoutes()

	convStore := storage.NewMemoryConversationStore(st)
	aiSvc := planning.NewAIPlanningService(planning.NewAnalysisService(st), convStore, st, &stubLLMProviderCov{available: true})
	NewAIPlanningServer(srv, aiSvc, convStore).RegisterAIPlanningRoutes()

	t.Run("api keys are reachable without rbac", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/auth/keys", ""), http.StatusOK)
		if !strings.Contains(rr.Body.String(), "[") {
			t.Fatalf("expected a JSON list, got %s", rr.Body.String())
		}

		rr = assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/auth/keys", `{"name":"ci","scopes":["pools:read"]}`), http.StatusCreated)
		var created struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if created.ID == "" || !strings.HasPrefix(created.Key, "cpam_") {
			t.Fatalf("unexpected key: %+v", created)
		}

		assertStatusCov(t, doReqCov(t, mux, http.MethodDelete, "/api/v1/auth/keys/"+created.ID, ""), http.StatusNoContent)
	})

	t.Run("ai sessions are reachable without rbac", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/ai/sessions", `{"title":"t"}`), http.StatusCreated)
		var conv struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &conv); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if conv.ID == "" {
			t.Fatal("expected a conversation id")
		}
		assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/ai/sessions/"+conv.ID, ""), http.StatusOK)
		assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/ai/chat", `{"session_id":"","message":"hi"}`), http.StatusBadRequest)
	})
}
