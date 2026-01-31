package http

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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

func TestRateLimitMiddlewareHeaders(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerSecond: 10,
		Burst:             5,
	}
	handler := RateLimitMiddleware(cfg, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Check X-RateLimit-Limit header
	limitHeader := rr.Header().Get("X-RateLimit-Limit")
	if limitHeader == "" {
		t.Fatal("expected X-RateLimit-Limit header to be set")
	}
	limit, err := strconv.ParseFloat(limitHeader, 64)
	if err != nil {
		t.Fatalf("failed to parse X-RateLimit-Limit: %v", err)
	}
	if limit != 10 {
		t.Fatalf("expected X-RateLimit-Limit to be 10, got %f", limit)
	}

	// Check X-RateLimit-Remaining header
	remainingHeader := rr.Header().Get("X-RateLimit-Remaining")
	if remainingHeader == "" {
		t.Fatal("expected X-RateLimit-Remaining header to be set")
	}
	remaining, err := strconv.Atoi(remainingHeader)
	if err != nil {
		t.Fatalf("failed to parse X-RateLimit-Remaining: %v", err)
	}
	// After one request with burst 5, remaining should be around 4
	if remaining < 0 || remaining > 5 {
		t.Fatalf("expected X-RateLimit-Remaining to be between 0 and 5, got %d", remaining)
	}

	// Check X-RateLimit-Reset header
	resetHeader := rr.Header().Get("X-RateLimit-Reset")
	if resetHeader == "" {
		t.Fatal("expected X-RateLimit-Reset header to be set")
	}
	reset, err := strconv.ParseInt(resetHeader, 10, 64)
	if err != nil {
		t.Fatalf("failed to parse X-RateLimit-Reset: %v", err)
	}
	now := time.Now().Unix()
	if reset < now || reset > now+2 {
		t.Fatalf("expected X-RateLimit-Reset to be within next 2 seconds, got %d (now: %d)", reset, now)
	}
}

func TestRateLimitMiddlewareRetryAfterHeader(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerSecond: 5,
		Burst:             1,
	}
	handler := RateLimitMiddleware(cfg, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust the burst
	first := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(first, req1)

	// This should be rate limited
	second := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(second, req2)

	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", second.Code)
	}

	retryAfter := second.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header to be set")
	}
	retrySeconds, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("failed to parse Retry-After: %v", err)
	}
	if retrySeconds < 1 {
		t.Fatalf("expected Retry-After to be at least 1, got %d", retrySeconds)
	}
}

func TestRateLimitMiddlewarePerIPTracking(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerSecond: 5,
		Burst:             1,
	}
	handler := RateLimitMiddleware(cfg, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request from IP 1.2.3.4 - should succeed
	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "1.2.3.4:12345"
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected first request from IP1 to succeed, got %d", rr1.Code)
	}

	// Second request from IP 1.2.3.4 - should be rate limited
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "1.2.3.4:12345"
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request from IP1 to be rate limited, got %d", rr2.Code)
	}

	// First request from different IP 5.6.7.8 - should succeed
	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.RemoteAddr = "5.6.7.8:54321"
	handler.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("expected first request from IP2 to succeed, got %d", rr3.Code)
	}
}

func TestRateLimitMiddlewareXForwardedFor(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerSecond: 5,
		Burst:             1,
	}
	handler := RateLimitMiddleware(cfg, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request with X-Forwarded-For - should succeed
	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "proxy:80"
	req1.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected first request to succeed, got %d", rr1.Code)
	}

	// Second request with same X-Forwarded-For first IP - should be rate limited
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "proxy:80"
	req2.Header.Set("X-Forwarded-For", "10.0.0.1")
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to be rate limited, got %d", rr2.Code)
	}
}

func TestRateLimitMiddlewareDisabled(t *testing.T) {
	// Disabled config (zero values)
	cfg := RateLimitConfig{
		RequestsPerSecond: 0,
		Burst:             0,
	}
	handler := RateLimitMiddleware(cfg, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Should pass through without rate limiting
	for i := 0; i < 10; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected request %d to succeed with disabled rate limiting, got %d", i, rr.Code)
		}
	}
}

func TestDefaultRateLimitConfig(t *testing.T) {
	// Test default values
	cfg := DefaultRateLimitConfig()
	if cfg.RequestsPerSecond != defaultRateLimitRPS {
		t.Fatalf("expected default RPS %f, got %f", defaultRateLimitRPS, cfg.RequestsPerSecond)
	}
	if cfg.Burst != defaultRateLimitBurst {
		t.Fatalf("expected default burst %d, got %d", defaultRateLimitBurst, cfg.Burst)
	}
}

func TestDefaultRateLimitConfigFromEnv(t *testing.T) {
	// Save original env vars
	origRPS := os.Getenv("RATE_LIMIT_RPS")
	origBurst := os.Getenv("RATE_LIMIT_BURST")
	defer func() {
		os.Setenv("RATE_LIMIT_RPS", origRPS)
		os.Setenv("RATE_LIMIT_BURST", origBurst)
	}()

	// Set custom values
	os.Setenv("RATE_LIMIT_RPS", "50")
	os.Setenv("RATE_LIMIT_BURST", "100")

	cfg := DefaultRateLimitConfig()
	if cfg.RequestsPerSecond != 50 {
		t.Fatalf("expected RPS 50 from env, got %f", cfg.RequestsPerSecond)
	}
	if cfg.Burst != 100 {
		t.Fatalf("expected burst 100 from env, got %d", cfg.Burst)
	}
}

func TestDefaultRateLimitConfigInvalidEnv(t *testing.T) {
	// Save original env vars
	origRPS := os.Getenv("RATE_LIMIT_RPS")
	origBurst := os.Getenv("RATE_LIMIT_BURST")
	defer func() {
		os.Setenv("RATE_LIMIT_RPS", origRPS)
		os.Setenv("RATE_LIMIT_BURST", origBurst)
	}()

	// Set invalid values
	os.Setenv("RATE_LIMIT_RPS", "invalid")
	os.Setenv("RATE_LIMIT_BURST", "notanumber")

	cfg := DefaultRateLimitConfig()
	if cfg.RequestsPerSecond != defaultRateLimitRPS {
		t.Fatalf("expected default RPS on invalid env, got %f", cfg.RequestsPerSecond)
	}
	if cfg.Burst != defaultRateLimitBurst {
		t.Fatalf("expected default burst on invalid env, got %d", cfg.Burst)
	}
}

func TestSanitizeRequestID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "valid alphanumeric",
			input: "abc123",
			want:  "abc123",
		},
		{
			name:  "valid with dashes",
			input: "req-123-abc",
			want:  "req-123-abc",
		},
		{
			name:  "valid with underscores",
			input: "req_123_abc",
			want:  "req_123_abc",
		},
		{
			name:  "valid with dots",
			input: "req.123.abc",
			want:  "req.123.abc",
		},
		{
			name:  "valid uuid format",
			input: "550e8400-e29b-41d4-a716-446655440000",
			want:  "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:  "valid mixed case",
			input: "ReQ-123-AbC",
			want:  "ReQ-123-AbC",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   ",
			want:  "",
		},
		{
			name:  "trimmed whitespace",
			input: "  req-123  ",
			want:  "req-123",
		},
		{
			name:  "too long",
			input: "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789",
			want:  "",
		},
		{
			name:  "exactly 64 chars",
			input: "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz12",
			want:  "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz12",
		},
		{
			name:  "65 chars rejected",
			input: "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz123",
			want:  "",
		},
		{
			name:  "invalid special chars",
			input: "req@123",
			want:  "",
		},
		{
			name:  "invalid spaces",
			input: "req 123",
			want:  "",
		},
		{
			name:  "invalid newline",
			input: "req\n123",
			want:  "",
		},
		{
			name:  "invalid tab",
			input: "req\t123",
			want:  "",
		},
		{
			name:  "invalid unicode",
			input: "req-123\u00e9",
			want:  "",
		},
		{
			name:  "invalid html injection attempt",
			input: "<script>alert(1)</script>",
			want:  "",
		},
		{
			name:  "invalid sql injection attempt",
			input: "'; DROP TABLE users;--",
			want:  "",
		},
		{
			name:  "invalid slash",
			input: "req/123",
			want:  "",
		},
		{
			name:  "invalid backslash",
			input: "req\\123",
			want:  "",
		},
		{
			name:  "invalid colon",
			input: "req:123",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeRequestID(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeRequestID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRequestIDMiddlewareRejectsInvalidID(t *testing.T) {
	tests := []struct {
		name    string
		inputID string
		wantNew bool // whether we expect a new ID to be generated
	}{
		{
			name:    "rejects too long ID",
			inputID: "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789",
			wantNew: true,
		},
		{
			name:    "rejects special chars",
			inputID: "req@123",
			wantNew: true,
		},
		{
			name:    "rejects html injection",
			inputID: "<script>alert(1)</script>",
			wantNew: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured string
			handler := RequestIDMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = RequestIDFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			}))

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(requestIDHeader, tt.inputID)

			handler.ServeHTTP(rr, req)

			responseID := rr.Header().Get(requestIDHeader)
			if tt.wantNew {
				if responseID == tt.inputID {
					t.Errorf("expected invalid ID to be rejected, but got same ID back: %q", responseID)
				}
				if captured == tt.inputID {
					t.Errorf("expected invalid ID to be rejected from context, but got same ID: %q", captured)
				}
				if captured == "" {
					t.Error("expected new ID to be generated, but got empty string")
				}
			}
		})
	}
}

func TestApplyMiddlewares(t *testing.T) {
	var order []string

	middleware1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "m1-before")
			next.ServeHTTP(w, r)
			order = append(order, "m1-after")
		})
	}

	middleware2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "m2-before")
			next.ServeHTTP(w, r)
			order = append(order, "m2-after")
		})
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	})

	wrapped := ApplyMiddlewares(handler, middleware1, middleware2)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	wrapped.ServeHTTP(rr, req)

	// First middleware should be outermost
	expected := []string{"m1-before", "m2-before", "handler", "m2-after", "m1-after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("expected order[%d]=%q, got %q", i, v, order[i])
		}
	}
}

func TestRateLimitConfigEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  RateLimitConfig
		want bool
	}{
		{
			name: "enabled with positive values",
			cfg:  RateLimitConfig{RequestsPerSecond: 10, Burst: 5},
			want: true,
		},
		{
			name: "disabled with zero RPS",
			cfg:  RateLimitConfig{RequestsPerSecond: 0, Burst: 5},
			want: false,
		},
		{
			name: "disabled with zero burst",
			cfg:  RateLimitConfig{RequestsPerSecond: 10, Burst: 0},
			want: false,
		},
		{
			name: "disabled with both zero",
			cfg:  RateLimitConfig{RequestsPerSecond: 0, Burst: 0},
			want: false,
		},
		{
			name: "disabled with negative RPS",
			cfg:  RateLimitConfig{RequestsPerSecond: -1, Burst: 5},
			want: false,
		},
		{
			name: "disabled with negative burst",
			cfg:  RateLimitConfig{RequestsPerSecond: 10, Burst: -1},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.Enabled()
			if got != tt.want {
				t.Errorf("RateLimitConfig.Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
