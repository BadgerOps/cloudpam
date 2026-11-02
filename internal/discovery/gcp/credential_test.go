package gcp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
)

const testEmail = "sa@proj-1.iam.gserviceaccount.com"

func testServiceAccount(t *testing.T) discovery.GCPServiceAccount {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	b, _ := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "proj-1",
		"private_key_id": "kid-1",
		"private_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email":   testEmail,
		"token_uri":      "http://127.0.0.1:1/never",
	})
	sa, err := discovery.ParseGCPServiceAccountKey(b)
	if err != nil {
		t.Fatal(err)
	}
	return sa
}

type googleStub struct {
	mu            sync.Mutex
	tokenStatus   int
	tokenBody     string
	projectStatus int
	tokenGrants   []string
	assertions    []string
	authHeaders   []string
	projectPaths  []string
}

func newGoogleStub(t *testing.T) (*googleStub, *Collector) {
	t.Helper()
	g := &googleStub{tokenStatus: http.StatusOK, tokenBody: `{"access_token":"tok-1","token_type":"Bearer","expires_in":3600}`, projectStatus: http.StatusOK}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		g.mu.Lock()
		g.tokenGrants = append(g.tokenGrants, r.PostForm.Get("grant_type"))
		g.assertions = append(g.assertions, r.PostForm.Get("assertion"))
		g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(g.tokenStatus)
		_, _ = w.Write([]byte(g.tokenBody))
	})
	mux.HandleFunc("/compute/v1/projects/", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.authHeaders = append(g.authHeaders, r.Header.Get("Authorization"))
		g.projectPaths = append(g.projectPaths, r.URL.Path)
		g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/global/networks"):
			_, _ = w.Write([]byte(`{"items":[{"id":"1","name":"default","IPv4Range":"10.0.0.0/8"}]}`))
		case strings.Contains(r.URL.Path, "/aggregated/"):
			_, _ = w.Write([]byte(`{"items":{}}`))
		default:
			w.WriteHeader(g.projectStatus)
			_, _ = w.Write([]byte(`{"name":"proj-1"}`))
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	collector := NewWithHTTPClient(&http.Client{Transport: &rewriteTransport{baseURL: server.URL}})
	return g, collector
}

func TestWithCredentialIdentityUsesOnlyTheServiceAccount(t *testing.T) {
	g, collector := newGoogleStub(t)
	ctx := context.Background()
	account := domain.Account{ID: 4, Provider: "gcp", Key: "gcp:proj-1"}

	bound, err := collector.WithCredential(ctx, account, testServiceAccount(t))
	if err != nil {
		t.Fatalf("WithCredential() error = %v", err)
	}
	if len(g.tokenGrants) != 0 {
		t.Fatal("WithCredential fetched a token; it must be lazy")
	}

	identity, err := bound.Identity(ctx, account)
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	if identity != testEmail {
		t.Fatalf("Identity() = %q, want %q", identity, testEmail)
	}
	if len(g.tokenGrants) != 1 || g.tokenGrants[0] != "urn:ietf:params:oauth:grant-type:jwt-bearer" || g.assertions[0] == "" {
		t.Fatalf("token request = grants %v assertions %d", g.tokenGrants, len(g.assertions))
	}
	if len(g.projectPaths) != 1 || g.projectPaths[0] != "/compute/v1/projects/proj-1" || g.authHeaders[0] != "Bearer tok-1" {
		t.Fatalf("project request = %v with auth %v", g.projectPaths, g.authHeaders)
	}

	resources, err := bound.Discover(ctx, account)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(resources) != 1 || resources[0].Name != "default" {
		t.Fatalf("Discover() = %+v", resources)
	}
	for _, h := range g.authHeaders {
		if h != "Bearer tok-1" {
			t.Fatalf("a Compute call ran without the service-account token: %v", g.authHeaders)
		}
	}
	if len(g.tokenGrants) != 1 {
		t.Fatalf("token fetched %d times, want the cached token reused", len(g.tokenGrants))
	}
	if bound.Provider() != "gcp" {
		t.Fatalf("Provider() = %q", bound.Provider())
	}
}

func TestIdentityReducesFailuresToProbeErrors(t *testing.T) {
	cases := []struct {
		name     string
		arrange  func(g *googleStub)
		account  domain.Account
		wantStep discovery.ProbeStep
		wantCode string
		wantHTTP int
	}{
		{"token rejected", func(g *googleStub) {
			g.tokenStatus = http.StatusBadRequest
			g.tokenBody = `{"error":"invalid_grant","error_description":"Invalid JWT Signature for sa@proj-1"}`
		}, domain.Account{Key: "gcp:proj-1"}, "token", "invalid_grant", http.StatusBadRequest},
		{"token endpoint error without code", func(g *googleStub) {
			g.tokenStatus = http.StatusInternalServerError
			g.tokenBody = `oops`
		}, domain.Account{Key: "gcp:proj-1"}, "token", "", http.StatusInternalServerError},
		{"project forbidden", func(g *googleStub) { g.projectStatus = http.StatusForbidden }, domain.Account{Key: "gcp:proj-1"}, "get_project", "", http.StatusForbidden},
		{"no project on the account", func(*googleStub) {}, domain.Account{}, "get_project", "no_project_id", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, collector := newGoogleStub(t)
			tc.arrange(g)
			bound, _ := collector.WithCredential(context.Background(), tc.account, testServiceAccount(t))
			identity, err := bound.Identity(context.Background(), tc.account)
			if identity != "" {
				t.Fatalf("Identity() = %q, want empty on failure", identity)
			}
			var pe *discovery.ProbeError
			if !errors.As(err, &pe) {
				t.Fatalf("Identity() error = %v, want *ProbeError", err)
			}
			if pe.Step != tc.wantStep || pe.Code != tc.wantCode || pe.Status != tc.wantHTTP {
				t.Fatalf("ProbeError = %+v, want step %s code %q http %d", *pe, tc.wantStep, tc.wantCode, tc.wantHTTP)
			}
			if strings.Contains(err.Error(), "Signature") {
				t.Fatalf("error leaks the token response: %v", err)
			}
		})
	}
}

func TestIdentityKeepsNetworkFailuresDiagnosable(t *testing.T) {
	account := domain.Account{Key: "gcp:proj-1"}

	t.Run("token endpoint unreachable", func(t *testing.T) {
		closed := httptest.NewServer(http.NotFoundHandler())
		closed.Close()
		collector := NewWithHTTPClient(&http.Client{Transport: &rewriteTransport{baseURL: closed.URL}})
		bound, _ := collector.WithCredential(context.Background(), account, testServiceAccount(t))
		_, err := bound.Identity(context.Background(), account)
		var netErr net.Error
		if !errors.As(err, &netErr) {
			t.Fatalf("Identity() error = %v, want the net.Error preserved through the token fetch", err)
		}
		if !strings.HasPrefix(err.Error(), "token: ") {
			t.Fatalf("Identity() error = %q, want the token step prefix", err.Error())
		}
	})

	t.Run("binding context cancels the token fetch", func(t *testing.T) {
		_, collector := newGoogleStub(t)
		ctx, cancel := context.WithCancel(context.Background())
		bound, _ := collector.WithCredential(ctx, account, testServiceAccount(t))
		cancel()
		_, err := bound.Identity(context.Background(), account)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Identity() error = %v, want context.Canceled from the token request", err)
		}
	})
}

func TestWithCredentialRejectsAForeignPayload(t *testing.T) {
	_, collector := newGoogleStub(t)
	role, err := discovery.NewAWSAssumeRole("arn:aws:iam::123456789012:role/x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collector.WithCredential(context.Background(), domain.Account{}, role); !errors.Is(err, discovery.ErrCredentialProviderMismatch) {
		t.Fatalf("WithCredential() error = %v, want ErrCredentialProviderMismatch", err)
	}
}

func TestInjectedClientStillWinsWithoutATokenSource(t *testing.T) {
	_, collector := newGoogleStub(t)
	client, err := collector.getHTTPClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client != collector.httpClient {
		t.Fatal("getHTTPClient() replaced the injected client although no token source is set")
	}
}
