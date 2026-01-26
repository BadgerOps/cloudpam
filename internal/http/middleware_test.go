package http

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}

func TestRequestIDMiddlewareGeneratesID(t *testing.T) {
	var captured string
	handler := RequestIDMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get(requestIDHeader); got == "" {
		t.Fatalf("expected request id header to be set")
	}
	if captured == "" {
		t.Fatalf("expected request id in context")
	}
}

func TestRequestIDMiddlewarePreservesValidIncoming(t *testing.T) {
	const original = "req-123"
	var captured string
	handler := RequestIDMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(requestIDHeader, original)

	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get(requestIDHeader); got != original {
		t.Fatalf("expected request id header %q, got %q", original, got)
	}
	if captured != original {
		t.Fatalf("expected context request id %q, got %q", original, captured)
	}
}

func TestRateLimitMiddlewareBlocksAfterBurstExhausted(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerSecond: 5,
		Burst:             1,
	}
	handler := RateLimitMiddleware(cfg, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	first := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(first, req1)
	if first.Code != http.StatusOK {
		t.Fatalf("expected first request to succeed, got %d", first.Code)
	}

	second := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(second, req2)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to be rate limited, got %d", second.Code)
	}

	var resp apiError
	if err := json.Unmarshal(second.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected json error response: %v", err)
	}
	if resp.Error != "too many requests" {
		t.Fatalf("expected error message, got %+v", resp)
	}

	// Wait for a token to replenish and try again.
	time.Sleep(300 * time.Millisecond)
	third := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(third, req3)
	if third.Code != http.StatusOK {
		t.Fatalf("expected third request after wait to succeed, got %d", third.Code)
	}
}
