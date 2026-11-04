package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"cloudpam/internal/discovery"
)

func fastRetries(t *testing.T, base time.Duration) {
	t.Helper()
	old := retryBaseDelay
	retryBaseDelay = base
	t.Cleanup(func() { retryBaseDelay = old })
}

// flakyRM fails the first `failures` requests with status (forever when
// failures < 0), then serves an empty organization.
func flakyRM(t *testing.T, status, failures int, header http.Header) (*http.Client, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(calls.Add(1))
		if failures < 0 || n <= failures {
			for k, v := range header {
				w.Header()[k] = v
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"busy"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(rmProjectList{})
	}))
	t.Cleanup(srv.Close)
	return &http.Client{Transport: &rewriteTransport{baseURL: srv.URL}}, &calls
}

func TestListProjectsRetriesTransientFailures(t *testing.T) {
	fastRetries(t, time.Millisecond)
	client, calls := flakyRM(t, http.StatusServiceUnavailable, 2, nil)
	if _, err := ListProjects(t.Context(), client, "organizations/1", nil); err != nil {
		t.Fatalf("ListProjects() error = %v, want success after two 503s", err)
	}
	// Two failures and a success on the projects page, then one folders page.
	if got := calls.Load(); got != 4 {
		t.Fatalf("requests = %d, want 4", got)
	}
}

func TestListProjectsGivesUpAfterFourAttempts(t *testing.T) {
	fastRetries(t, time.Millisecond)
	client, calls := flakyRM(t, http.StatusServiceUnavailable, -1, nil)
	_, err := ListProjects(t.Context(), client, "organizations/1", nil)
	var httpErr *discovery.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("error = %v, want a 503 HTTPError", err)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("requests = %d, want 4", got)
	}
}

func TestListProjectsDoesNotRetryOtherClientErrors(t *testing.T) {
	fastRetries(t, time.Millisecond)
	client, calls := flakyRM(t, http.StatusNotFound, -1, nil)
	_, err := ListProjects(t.Context(), client, "organizations/1", nil)
	var httpErr *discovery.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusNotFound {
		t.Fatalf("error = %v, want a 404 HTTPError", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestListProjectsHonoursRetryAfter(t *testing.T) {
	fastRetries(t, time.Millisecond)
	client, _ := flakyRM(t, http.StatusTooManyRequests, 1, http.Header{"Retry-After": {"1"}})
	start := time.Now()
	if _, err := ListProjects(t.Context(), client, "organizations/1", nil); err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond || elapsed > 1900*time.Millisecond {
		t.Fatalf("elapsed = %s, want about the 1s Retry-After", elapsed)
	}
}

func TestListProjectsStopsBackingOffWhenTheContextEnds(t *testing.T) {
	fastRetries(t, 10*time.Second)
	client, calls := flakyRM(t, http.StatusServiceUnavailable, -1, nil)
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := ListProjects(ctx, client, "organizations/1", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("elapsed = %s, want a prompt return", elapsed)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestListProjectsRetriesWaitOnTheLimiter(t *testing.T) {
	fastRetries(t, time.Millisecond)
	client, calls := flakyRM(t, http.StatusServiceUnavailable, -1, nil)
	_, err := ListProjects(shortDeadline(t), client, "organizations/1", limiterWithTokens(t, 2))
	if err == nil {
		t.Fatal("ListProjects() = nil error, want the limiter to fail the third attempt")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestImpersonatedTokenSourceRetriesRateLimiting(t *testing.T) {
	fastRetries(t, time.Millisecond)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["lifetime"] != "3600s" {
			t.Errorf("attempt %d body = %v (%v), want the full request body", calls.Load()+1, body, err)
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken": "minted",
			"expireTime":  time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: &rewriteTransport{baseURL: srv.URL}}

	tok, err := ImpersonatedTokenSource(t.Context(), client, "sa@p.iam.gserviceaccount.com", nil).Token()
	if err != nil || tok.AccessToken != "minted" {
		t.Fatalf("Token() = %v, %v, want the minted token after one 429", tok, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("generateAccessToken calls = %d, want 2", got)
	}
}
