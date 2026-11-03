package gcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
)

// limiterWithTokens returns a limiter holding exactly n tokens and refilling
// one per second, so a test can count the calls that get through before a
// short deadline fails the next wait.
func limiterWithTokens(t *testing.T, n int) *discovery.Limiter {
	t.Helper()
	if n > 0 {
		return discovery.NewLimiter(float64(n))
	}
	l := discovery.NewLimiter(1)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	return l
}

func shortDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

// twoPageCompute serves networks as two pages and every other listing as one.
func twoPageCompute(t *testing.T, requests *atomic.Int32) *Collector {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/compute/v1/projects/test-project/global/networks", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body := `{"items":[{"id":"1","name":"a"}],"nextPageToken":"p2"}`
		if r.URL.Query().Get("pageToken") == "p2" {
			body = `{"items":[{"id":"2","name":"b"}]}`
		}
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return NewWithHTTPClient(&http.Client{
		Transport: &rewriteTransport{base: server.Client().Transport, baseURL: server.URL},
	})
}

func TestDiscoverCountsEveryComputePageAgainstTheLimiter(t *testing.T) {
	cases := []struct {
		name         string
		tokens       int
		wantRequests int32
	}{
		{name: "no token stops before the first call", tokens: 0, wantRequests: 0},
		{name: "two tokens allow two pages", tokens: 2, wantRequests: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			collector := twoPageCompute(t, &requests)
			collector.SetLimiter(limiterWithTokens(t, tc.tokens))

			_, err := collector.Discover(shortDeadline(t), domain.Account{ID: 1, ExternalID: "test-project"})
			if err == nil {
				t.Fatal("Discover() = nil error, want the limiter to fail the next page before the deadline")
			}
			if got := requests.Load(); got != tc.wantRequests {
				t.Fatalf("requests = %d, want %d", got, tc.wantRequests)
			}
		})
	}
}

func TestDiscoverWithoutALimiterIsNotPaced(t *testing.T) {
	var requests atomic.Int32
	collector := twoPageCompute(t, &requests)
	collector.SetLimiter(nil)

	resources, err := collector.Discover(shortDeadline(t), domain.Account{ID: 1, ExternalID: "test-project"})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(resources) != 2 || requests.Load() != 4 {
		t.Fatalf("resources = %d, requests = %d; want 2 networks over 4 requests", len(resources), requests.Load())
	}
}

func TestWithCredentialKeepsTheHostLimiter(t *testing.T) {
	var requests atomic.Int32
	host := twoPageCompute(t, &requests)
	host.SetLimiter(limiterWithTokens(t, 0))

	account := domain.Account{ID: 1, Provider: "gcp", ExternalID: "test-project"}
	bound, err := host.WithCredential(context.Background(), account, testServiceAccount(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Discover(shortDeadline(t), account); err == nil {
		t.Fatal("bound Discover() = nil error, want the host limiter to hold the call")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests = %d, want 0 while the host limiter is empty", got)
	}
}
