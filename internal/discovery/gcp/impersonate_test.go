package gcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/discovery"
)

func TestServiceAccountFor(t *testing.T) {
	if got := ServiceAccountFor("cloudpam-discovery@{project}.iam.gserviceaccount.com", "p-1"); got != "cloudpam-discovery@p-1.iam.gserviceaccount.com" {
		t.Fatalf("ServiceAccountFor = %q", got)
	}
	shared := "central@ops.iam.gserviceaccount.com"
	if got := ServiceAccountFor(shared, "p-1"); got != shared {
		t.Fatalf("ServiceAccountFor without placeholder = %q, want %q", got, shared)
	}
}

func TestCheckServiceAccountTemplate(t *testing.T) {
	for _, ok := range []string{"cloudpam-discovery@{project}.iam.gserviceaccount.com", "central@ops.iam.gserviceaccount.com"} {
		if err := CheckServiceAccountTemplate(ok); err != nil {
			t.Errorf("CheckServiceAccountTemplate(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"not-an-email", "a@b", "{project}", "a b@c.d"} {
		if err := CheckServiceAccountTemplate(bad); err == nil {
			t.Errorf("CheckServiceAccountTemplate(%q) = nil, want error", bad)
		}
	}
}

func TestImpersonatedTokenSourceMintsAComputeToken(t *testing.T) {
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	var gotPath string
	var gotBody map[string]any
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken": "minted",
			"expireTime":  expiry.Format(time.RFC3339),
		})
	}))
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: &rewriteTransport{baseURL: srv.URL}}

	ts := ImpersonatedTokenSource(t.Context(), client, "sa@p-1.iam.gserviceaccount.com", discovery.NewLimiter(1000))
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if tok.AccessToken != "minted" || tok.TokenType != "Bearer" || !tok.Expiry.Equal(expiry) {
		t.Fatalf("token = %+v, want minted Bearer expiring %s", tok, expiry)
	}
	if gotPath != "/v1/projects/-/serviceAccounts/sa@p-1.iam.gserviceaccount.com:generateAccessToken" {
		t.Fatalf("path = %q", gotPath)
	}
	scopes, _ := gotBody["scope"].([]any)
	if len(scopes) != 1 || scopes[0] != computeScope || gotBody["lifetime"] != "3600s" {
		t.Fatalf("body = %v, want compute.readonly scope and 3600s lifetime", gotBody)
	}

	// A second call reuses the unexpired token.
	if _, err := ts.Token(); err != nil {
		t.Fatalf("second Token() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("generateAccessToken calls = %d, want 1", calls)
	}
}

func TestImpersonatedTokenSourceSurfacesTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"secret detail"}}`))
	}))
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: &rewriteTransport{baseURL: srv.URL}}

	_, err := ImpersonatedTokenSource(t.Context(), client, "sa@p.iam.gserviceaccount.com", nil).Token()
	var httpErr *discovery.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusForbidden {
		t.Fatalf("error = %v, want a 403 HTTPError", err)
	}
	if !strings.Contains(err.Error(), "sa@p.iam.gserviceaccount.com") {
		t.Fatalf("error = %v, want it to name the service account", err)
	}
	if strings.Contains(err.Error(), "secret detail") {
		t.Fatalf("error = %v, must not echo the provider response body", err)
	}
}
