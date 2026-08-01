package testutil

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"cloudpam/internal/api"
	"cloudpam/internal/audit"
)

// The helpers in this package are the shared harness for the integration
// tests across the repo. These tests pin the wiring contract each caller
// depends on: which middleware a config flag actually installs, and that the
// returned components are the ones the server is really using.

func TestNewTestServerServesAPIWithDefaultConfig(t *testing.T) {
	c := NewTestServer(t, DefaultTestServerConfig())
	defer c.Cleanup()

	if c.Server == nil || c.Store == nil || c.KeyStore == nil || c.SessionStore == nil || c.UserStore == nil || c.Logger == nil {
		t.Fatalf("NewTestServer left components unset: %+v", c)
	}
	if c.Metrics != nil {
		t.Error("Metrics should be nil when EnableMetrics is false")
	}
	if c.AuditLogger != nil {
		t.Error("AuditLogger should be nil when EnableAudit is false")
	}

	// /healthz is one of the public routes RegisterProtectedRoutes installs.
	resp := DoRequest(t, c.HTTPClient(), MustAuthenticatedRequest(t, http.MethodGet, c.URL("/healthz"), "", nil))
	defer func() { _ = resp.Body.Close() }()

	AssertStatus(t, resp.StatusCode, http.StatusOK)
	// RequestIDMiddleware is always installed.
	AssertHeaderExists(t, resp, "X-Request-ID")

	// Everything under /api/v1 is behind the dual-auth middleware that
	// RegisterProtectedRoutes installs regardless of the harness auth flag.
	protected := DoRequest(t, c.HTTPClient(), MustAuthenticatedRequest(t, http.MethodGet, c.URL("/api/v1/pools"), "", nil))
	defer func() { _ = protected.Body.Close() }()
	AssertStatus(t, protected.StatusCode, http.StatusUnauthorized)
}

func TestNewTestServerURLAndClient(t *testing.T) {
	c := NewTestServer(t, DefaultTestServerConfig())
	defer c.Cleanup()

	if got, want := c.URL("/api/v1/pools"), c.Server.URL+"/api/v1/pools"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
	if c.HTTPClient() != c.Server.Client() {
		t.Error("HTTPClient() should return the test server's own client")
	}
}

func TestNewTestServerRequireAuthRejectsAnonymous(t *testing.T) {
	cfg := DefaultTestServerConfig()
	cfg.EnableAuth = true
	cfg.RequireAuth = true
	c := NewTestServer(t, cfg)
	defer c.Cleanup()

	anon := DoRequest(t, c.HTTPClient(), MustAuthenticatedRequest(t, http.MethodGet, c.URL("/api/v1/pools"), "", nil))
	defer func() { _ = anon.Body.Close() }()
	if anon.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous request status = %d, want 401", anon.StatusCode)
	}

	plaintext, apiKey := CreateTestAPIKey(t, c.KeyStore, "harness", []string{"pools:read", "pools:write"})
	if plaintext == "" || apiKey == nil {
		t.Fatal("CreateTestAPIKey returned an unusable key")
	}
	// The key must be readable from the same store the server authenticates against.
	stored, err := c.KeyStore.GetByPrefix(context.Background(), apiKey.Prefix)
	if err != nil {
		t.Fatalf("GetByPrefix: %v", err)
	}
	if stored == nil || stored.ID != apiKey.ID {
		t.Fatalf("CreateTestAPIKey did not persist the key into the harness key store")
	}

	authed := DoRequest(t, c.HTTPClient(), MustAuthenticatedRequest(t, http.MethodGet, c.URL("/api/v1/pools"), plaintext, nil))
	defer func() { _ = authed.Body.Close() }()
	AssertStatus(t, authed.StatusCode, http.StatusOK)
}

func TestNewTestServerRateLimitReturns429(t *testing.T) {
	cfg := DefaultTestServerConfig()
	cfg.EnableRateLimit = true
	cfg.RateLimitConfig = api.RateLimitConfig{RequestsPerSecond: 1, Burst: 1}
	c := NewTestServer(t, cfg)
	defer c.Cleanup()

	var limited bool
	for i := 0; i < 10; i++ {
		resp := DoRequest(t, c.HTTPClient(), MustAuthenticatedRequest(t, http.MethodGet, c.URL("/healthz"), "", nil))
		status := resp.StatusCode
		_ = resp.Body.Close()
		if status == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("EnableRateLimit did not install a rate limiter: no 429 after 10 rapid requests")
	}
}

func TestNewTestServerAuditCapturesMutations(t *testing.T) {
	cfg := DefaultTestServerConfig()
	cfg.EnableAudit = true
	c := NewTestServer(t, cfg)
	defer c.Cleanup()

	if c.AuditLogger == nil {
		t.Fatal("AuditLogger should be set when EnableAudit is true")
	}

	plaintext, _ := CreateTestAPIKey(t, c.KeyStore, "auditor", []string{"*"})
	resp := DoRequest(t, c.HTTPClient(), MustAuthenticatedRequest(t, http.MethodPost, c.URL("/api/v1/pools"), plaintext,
		JSONBody(t, map[string]any{"name": "audited", "cidr": "10.42.0.0/16"})))
	defer func() { _ = resp.Body.Close() }()
	AssertStatus(t, resp.StatusCode, http.StatusCreated)

	var created map[string]any
	ReadJSONResponse(t, resp, &created)
	if created["cidr"] != "10.42.0.0/16" {
		t.Fatalf("created pool = %v, want cidr 10.42.0.0/16", created)
	}

	mem, ok := c.AuditLogger.(*audit.MemoryAuditLogger)
	if !ok {
		t.Fatalf("AuditLogger is %T, want *audit.MemoryAuditLogger", c.AuditLogger)
	}
	events, _, err := mem.List(context.Background(), audit.ListOptions{Limit: 50})
	if err != nil {
		t.Fatalf("List audit events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("EnableAudit did not install audit middleware: no events recorded for a mutating request")
	}
}

func TestNewTestServerMetricsCollector(t *testing.T) {
	cfg := DefaultTestServerConfig()
	cfg.EnableMetrics = true
	c := NewTestServer(t, cfg)
	defer c.Cleanup()

	if c.Metrics == nil {
		t.Fatal("Metrics should be set when EnableMetrics is true")
	}

	// Enabling metrics registers the Prometheus handler and the middleware
	// that feeds it, so a request must show up in the scrape output.
	warm := DoRequest(t, c.HTTPClient(), MustAuthenticatedRequest(t, http.MethodGet, c.URL("/healthz"), "", nil))
	_ = warm.Body.Close()

	resp := DoRequest(t, c.HTTPClient(), MustAuthenticatedRequest(t, http.MethodGet, c.URL("/metrics"), "", nil))
	defer func() { _ = resp.Body.Close() }()
	AssertStatus(t, resp.StatusCode, http.StatusOK)
	AssertContains(t, resp.Body, "cloudpam_test")
}

func TestNewTestServerCleanupClosesServer(t *testing.T) {
	c := NewTestServer(t, DefaultTestServerConfig())
	url := c.URL("/healthz")
	client := c.HTTPClient()
	c.Cleanup()

	if _, err := client.Get(url); err == nil {
		t.Fatal("Cleanup did not shut the test server down")
	}
}

func TestAuthenticatedRequestHeaders(t *testing.T) {
	tests := []struct {
		name            string
		apiKey          string
		withBody        bool
		wantAuth        string
		wantContentType string
	}{
		{name: "no key no body", apiKey: "", withBody: false},
		{name: "key only", apiKey: "cpam_secret", wantAuth: "Bearer cpam_secret"},
		{name: "body only sets content type", withBody: true, wantContentType: "application/json"},
		{name: "key and body", apiKey: "cpam_secret", withBody: true, wantAuth: "Bearer cpam_secret", wantContentType: "application/json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.withBody {
				body = strings.NewReader(`{"a":1}`)
			}
			var req *http.Request
			var err error
			if body == nil {
				req, err = AuthenticatedRequest(http.MethodGet, "http://example.test/x", tc.apiKey, nil)
			} else {
				req, err = AuthenticatedRequest(http.MethodPost, "http://example.test/x", tc.apiKey, body)
			}
			if err != nil {
				t.Fatalf("AuthenticatedRequest: %v", err)
			}
			if got := req.Header.Get("Authorization"); got != tc.wantAuth {
				t.Errorf("Authorization = %q, want %q", got, tc.wantAuth)
			}
			if got := req.Header.Get("Content-Type"); got != tc.wantContentType {
				t.Errorf("Content-Type = %q, want %q", got, tc.wantContentType)
			}
		})
	}
}

func TestAuthenticatedRequestRejectsBadMethod(t *testing.T) {
	if _, err := AuthenticatedRequest("BAD METHOD", "http://example.test/x", "", nil); err == nil {
		t.Fatal("expected an error for an invalid HTTP method")
	}
}

func TestJSONBodyAndAssertionHelpers(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	want := payload{Name: "pool", Count: 3}

	var decoded payload
	AssertJSON(t, JSONBody(t, want), &decoded)
	if decoded != want {
		t.Errorf("AssertJSON decoded %+v, want %+v", decoded, want)
	}

	// AssertJSONEqual must be insensitive to key ordering.
	AssertJSONEqual(t, strings.NewReader(`{"count":3,"name":"pool"}`), want)

	AssertContains(t, JSONBody(t, want), `"name":"pool"`)
}

func TestAssertHeaderHelpers(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Content-Type", "application/json")

	AssertHeader(t, resp, "Content-Type", "application/json")
	AssertHeaderExists(t, resp, "Content-Type")
}

func TestDoRequestDefaultsToSharedClient(t *testing.T) {
	c := NewTestServer(t, DefaultTestServerConfig())
	defer c.Cleanup()

	// Passing a nil client must fall back to http.DefaultClient rather than panic.
	resp := DoRequest(t, nil, MustAuthenticatedRequest(t, http.MethodGet, c.URL("/healthz"), "", nil))
	defer func() { _ = resp.Body.Close() }()
	AssertStatus(t, resp.StatusCode, http.StatusOK)
}
