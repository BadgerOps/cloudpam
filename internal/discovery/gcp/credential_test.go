package gcp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

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

func TestDiscoverReducesARejectedTokenToAProbeError(t *testing.T) {
	g, collector := newGoogleStub(t)
	g.tokenStatus = http.StatusBadRequest
	g.tokenBody = `{"error":"invalid_grant","error_description":"LEAK-MARKER-9f3a"}`
	account := domain.Account{Provider: "gcp", Key: "gcp:proj-1"}
	bound, err := collector.WithCredential(context.Background(), account, testServiceAccount(t))
	if err != nil {
		t.Fatal(err)
	}

	_, err = bound.Discover(context.Background(), account)
	var pe *discovery.ProbeError
	if !errors.As(err, &pe) || pe.Step != discovery.ProbeStepToken || pe.Code != "invalid_grant" || pe.Status != http.StatusBadRequest {
		t.Fatalf("Discover() error = %v, want a token ProbeError", err)
	}
	if !strings.HasPrefix(err.Error(), "token: ") || strings.Contains(err.Error(), "LEAK-MARKER-9f3a") {
		t.Fatalf("Discover() error = %q, want the step without the token response body", err.Error())
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

	t.Run("client timeout still bounds the token fetch", func(t *testing.T) {
		untilDone := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		})
		collector := NewWithHTTPClient(&http.Client{Transport: untilDone, Timeout: 20 * time.Millisecond})
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		bound, _ := collector.WithCredential(ctx, account, testServiceAccount(t))
		start := time.Now()
		_, err := bound.Identity(context.Background(), account)
		if elapsed := time.Since(start); err == nil || elapsed > 100*time.Millisecond {
			t.Fatalf("Identity() = %v after %s, want the 20ms client timeout to end the token fetch", err, elapsed)
		}
	})

	t.Run("token response is read before its request context ends", func(t *testing.T) {
		bodyTiedToRequest := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := strings.NewReader(`{"access_token":"tok-1","token_type":"Bearer","expires_in":3600}`)
			return &http.Response{StatusCode: http.StatusOK, Body: ctxBody{ctx: req.Context(), Reader: body}}, nil
		})
		collector := NewWithHTTPClient(&http.Client{Transport: bodyTiedToRequest})
		bound, _ := collector.WithCredential(context.Background(), account, testServiceAccount(t))
		if _, err := bound.Identity(context.Background(), account); err != nil {
			t.Fatalf("Identity() error = %v, want the token read before its request context ends", err)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type ctxBody struct {
	ctx context.Context
	io.Reader
}

func (b ctxBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.Reader.Read(p)
}

func (ctxBody) Close() error { return nil }

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

func TestHostIdentityGetsTheProjectWithoutAServiceAccount(t *testing.T) {
	g, collector := newGoogleStub(t)
	account := domain.Account{Key: "gcp:proj-1"}

	identity, err := collector.Identity(context.Background(), account)
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	if identity != hostIdentity {
		t.Fatalf("Identity() = %q, want %q", identity, hostIdentity)
	}
	if len(g.tokenGrants) != 0 || len(g.projectPaths) != 1 || g.projectPaths[0] != "/compute/v1/projects/proj-1" {
		t.Fatalf("token grants = %v, project paths = %v; want only the project get", g.tokenGrants, g.projectPaths)
	}

	g.projectStatus = http.StatusForbidden
	for _, tc := range []struct {
		name     string
		account  domain.Account
		wantCode string
		wantHTTP int
	}{
		{"project forbidden", account, "", http.StatusForbidden},
		{"no project on the account", domain.Account{}, "no_project_id", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := collector.Identity(context.Background(), tc.account)
			var pe *discovery.ProbeError
			if !errors.As(err, &pe) || pe.Step != discovery.ProbeStepGetProject || pe.Code != tc.wantCode || pe.Status != tc.wantHTTP {
				t.Fatalf("Identity() error = %v, want get_project code %q http %d", err, tc.wantCode, tc.wantHTTP)
			}
		})
	}
}

func TestHostIdentityDeadlineBoundsTheADCTokenFetch(t *testing.T) {
	release := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); stalled.Close() })

	sa := testServiceAccount(t)
	key, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "proj-1",
		"private_key_id": sa.PrivateKeyID(),
		"private_key":    string(sa.PrivateKey()),
		"client_email":   sa.ClientEmail(),
		"token_uri":      stalled.URL + "/token",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/adc.json"
	if err := os.WriteFile(path, key, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := New().Identity(ctx, domain.Account{Key: "gcp:proj-1"})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Identity() error = %v, want context.DeadlineExceeded from the stalled token endpoint", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Identity() kept waiting on the stalled ADC token endpoint past the probe deadline")
	}
}

// listError is an error whose dynamic type cannot be compared with ==.
type listError []string

func (e listError) Error() string { return strings.Join(e, "; ") }

type failingTokenSource struct{ err error }

func (s failingTokenSource) Token() (*oauth2.Token, error) { return nil, s.err }

func TestIdentityReportsAnIncomparableTokenError(t *testing.T) {
	c := &serviceAccountCollector{
		Collector: &Collector{tokenSource: failingTokenSource{err: listError{"metadata unreachable"}}},
		token:     &tokenTransport{},
	}

	_, err := c.Identity(context.Background(), domain.Account{Provider: "gcp", Key: "gcp:proj-1"})

	if err == nil || err.Error() != "token: metadata unreachable" {
		t.Fatalf("Identity() error = %v, want the token step with the source's error", err)
	}
}
