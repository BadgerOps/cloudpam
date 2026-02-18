package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"cloudpam/internal/auth"
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
		_ = os.Setenv("RATE_LIMIT_RPS", origRPS)
		_ = os.Setenv("RATE_LIMIT_BURST", origBurst)
	}()

	// Set custom values
	_ = os.Setenv("RATE_LIMIT_RPS", "50")
	_ = os.Setenv("RATE_LIMIT_BURST", "100")

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
		_ = os.Setenv("RATE_LIMIT_RPS", origRPS)
		_ = os.Setenv("RATE_LIMIT_BURST", origBurst)
	}()

	// Set invalid values
	_ = os.Setenv("RATE_LIMIT_RPS", "invalid")
	_ = os.Setenv("RATE_LIMIT_BURST", "notanumber")

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

// Auth middleware tests

func createTestKey(t *testing.T, store auth.KeyStore, name string, scopes []string) (string, *auth.APIKey) {
	t.Helper()
	plaintext, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
		Name:   name,
		Scopes: scopes,
	})
	if err != nil {
		t.Fatalf("failed to generate API key: %v", err)
	}
	if err := store.Create(context.Background(), apiKey); err != nil {
		t.Fatalf("failed to store API key: %v", err)
	}
	return plaintext, apiKey
}

func TestAuthMiddlewareValidKey(t *testing.T) {
	store := auth.NewMemoryKeyStore()
	plaintext, apiKey := createTestKey(t, store, "Test Key", []string{"pools:read"})

	var capturedKey *auth.APIKey
	handler := AuthMiddleware(store, true, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedKey = auth.APIKeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if capturedKey == nil {
		t.Fatal("expected API key in context")
	}
	if capturedKey.ID != apiKey.ID {
		t.Errorf("expected key ID %q, got %q", apiKey.ID, capturedKey.ID)
	}
}

func TestAuthMiddlewareMissingAuthRequired(t *testing.T) {
	store := auth.NewMemoryKeyStore()

	handler := AuthMiddleware(store, true, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	var resp apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Error != "unauthorized" {
		t.Errorf("expected error 'unauthorized', got %q", resp.Error)
	}
}

func TestAuthMiddlewareMissingAuthOptional(t *testing.T) {
	store := auth.NewMemoryKeyStore()

	var called bool
	handler := AuthMiddleware(store, false, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if key := auth.APIKeyFromContext(r.Context()); key != nil {
			t.Error("expected no key in context for unauthenticated request")
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Error("handler was not called")
	}
}

func TestAuthMiddlewareInvalidFormat(t *testing.T) {
	store := auth.NewMemoryKeyStore()

	handler := AuthMiddleware(store, true, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name   string
		header string
	}{
		{"Basic auth", "Basic dXNlcjpwYXNz"},
		{"No Bearer prefix", "cpam_abc12345678901234567890123456789012"},
		{"Invalid key format", "Bearer not-a-valid-key"},
		{"Short key", "Bearer cpam_abc"},
		{"Invalid chars", "Bearer cpam_abc!@#$%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tt.header)

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", rr.Code)
			}
		})
	}
}

func TestAuthMiddlewareKeyNotFound(t *testing.T) {
	store := auth.NewMemoryKeyStore()

	handler := AuthMiddleware(store, true, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Valid format but key doesn't exist
	req.Header.Set("Authorization", "Bearer cpam_abcdefgh12345678901234567890123456789012")

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddlewareRevokedKey(t *testing.T) {
	store := auth.NewMemoryKeyStore()
	plaintext, apiKey := createTestKey(t, store, "Test Key", []string{"pools:read"})

	// Revoke the key
	if err := store.Revoke(context.Background(), apiKey.ID); err != nil {
		t.Fatalf("failed to revoke key: %v", err)
	}

	handler := AuthMiddleware(store, true, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	var resp apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Detail != "API key has been revoked" {
		t.Errorf("expected revoked message, got %q", resp.Detail)
	}
}

func TestAuthMiddlewareExpiredKey(t *testing.T) {
	store := auth.NewMemoryKeyStore()

	// Create an expired key
	expired := time.Now().Add(-1 * time.Hour)
	plaintext, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
		Name:      "Expired Key",
		Scopes:    []string{"pools:read"},
		ExpiresAt: &expired,
	})
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	if err := store.Create(context.Background(), apiKey); err != nil {
		t.Fatalf("failed to store key: %v", err)
	}

	handler := AuthMiddleware(store, true, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	var resp apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Detail != "API key has expired" {
		t.Errorf("expected expired message, got %q", resp.Detail)
	}
}

func TestAuthMiddlewareWrongKey(t *testing.T) {
	store := auth.NewMemoryKeyStore()
	_, apiKey := createTestKey(t, store, "Test Key", []string{"pools:read"})

	// Create a different key with the same prefix (simulating a brute force attempt)
	// The prefix will match but the hash won't
	wrongKey := "cpam_" + apiKey.Prefix + "wrongkeywrongkeywrongkeywrongkeyXX"

	handler := AuthMiddleware(store, true, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+wrongKey)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestRequireScopeMiddleware(t *testing.T) {
	store := auth.NewMemoryKeyStore()
	plaintext, _ := createTestKey(t, store, "Test Key", []string{"pools:read", "accounts:read"})

	tests := []struct {
		name           string
		requiredScope  string
		expectedStatus int
	}{
		{"has scope", "pools:read", http.StatusOK},
		{"missing scope", "pools:write", http.StatusForbidden},
		{"different resource", "accounts:read", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := ApplyMiddlewares(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
				AuthMiddleware(store, true, newTestLogger()),
				RequireScopeMiddleware(tt.requiredScope, newTestLogger()),
			)

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+plaintext)

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected %d, got %d: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestRequireScopeMiddlewareWildcard(t *testing.T) {
	store := auth.NewMemoryKeyStore()
	plaintext, _ := createTestKey(t, store, "Admin Key", []string{"*"})

	handler := ApplyMiddlewares(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		AuthMiddleware(store, true, newTestLogger()),
		RequireScopeMiddleware("anything:really", newTestLogger()),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for wildcard scope, got %d", rr.Code)
	}
}

func TestRequireScopeMiddlewareResourceWildcard(t *testing.T) {
	store := auth.NewMemoryKeyStore()
	plaintext, _ := createTestKey(t, store, "Pools Admin", []string{"pools:*"})

	tests := []struct {
		name           string
		requiredScope  string
		expectedStatus int
	}{
		{"pools:read", "pools:read", http.StatusOK},
		{"pools:write", "pools:write", http.StatusOK},
		{"pools:delete", "pools:delete", http.StatusOK},
		{"accounts:read", "accounts:read", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := ApplyMiddlewares(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
				AuthMiddleware(store, true, newTestLogger()),
				RequireScopeMiddleware(tt.requiredScope, newTestLogger()),
			)

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+plaintext)

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestRequireAnyScopeMiddleware(t *testing.T) {
	store := auth.NewMemoryKeyStore()
	plaintext, _ := createTestKey(t, store, "Test Key", []string{"pools:read"})

	tests := []struct {
		name           string
		requiredScopes []string
		expectedStatus int
	}{
		{"has one of required", []string{"pools:read", "pools:write"}, http.StatusOK},
		{"has none of required", []string{"accounts:write", "pools:delete"}, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := ApplyMiddlewares(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
				AuthMiddleware(store, true, newTestLogger()),
				RequireAnyScopeMiddleware(tt.requiredScopes, newTestLogger()),
			)

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+plaintext)

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected %d, got %d: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestRequireScopeMiddlewareNoAuth(t *testing.T) {
	handler := RequireScopeMiddleware("pools:read", newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", rr.Code)
	}
}

func TestAuthMiddlewareUpdatesLastUsed(t *testing.T) {
	store := auth.NewMemoryKeyStore()
	plaintext, apiKey := createTestKey(t, store, "Test Key", []string{"pools:read"})

	// Verify initial state
	initial, _ := store.GetByID(context.Background(), apiKey.ID)
	if initial.LastUsedAt != nil {
		t.Fatal("LastUsedAt should initially be nil")
	}

	handler := AuthMiddleware(store, true, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Give the async update time to complete
	time.Sleep(50 * time.Millisecond)

	updated, _ := store.GetByID(context.Background(), apiKey.ID)
	if updated.LastUsedAt == nil {
		t.Error("LastUsedAt should be updated after successful auth")
	}
}

// Audit middleware tests

// mockAuditLogger captures audit events for testing
type mockAuditLogger struct {
	events []*AuditEvent
}

func (m *mockAuditLogger) Log(ctx context.Context, event *AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}

func TestAuditMiddlewareSkipsGETRequests(t *testing.T) {
	auditLog := &mockAuditLogger{}
	handler := AuditMiddleware(auditLog, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	handler.ServeHTTP(rr, req)

	if len(auditLog.events) != 0 {
		t.Errorf("expected no audit events for GET, got %d", len(auditLog.events))
	}
}

func TestAuditMiddlewareLogsPostRequests(t *testing.T) {
	auditLog := &mockAuditLogger{}
	handler := AuditMiddleware(auditLog, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	handler.ServeHTTP(rr, req)

	if len(auditLog.events) != 1 {
		t.Fatalf("expected 1 audit event for POST, got %d", len(auditLog.events))
	}

	event := auditLog.events[0]
	if event.Action != "create" {
		t.Errorf("expected action 'create', got %q", event.Action)
	}
	if event.ResourceType != "pool" {
		t.Errorf("expected resource_type 'pool', got %q", event.ResourceType)
	}
	if event.StatusCode != http.StatusCreated {
		t.Errorf("expected status code %d, got %d", http.StatusCreated, event.StatusCode)
	}
}

func TestAuditMiddlewareLogsPatchRequests(t *testing.T) {
	auditLog := &mockAuditLogger{}
	handler := AuditMiddleware(auditLog, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/accounts/123", nil)
	handler.ServeHTTP(rr, req)

	if len(auditLog.events) != 1 {
		t.Fatalf("expected 1 audit event for PATCH, got %d", len(auditLog.events))
	}

	event := auditLog.events[0]
	if event.Action != "update" {
		t.Errorf("expected action 'update', got %q", event.Action)
	}
	if event.ResourceType != "account" {
		t.Errorf("expected resource_type 'account', got %q", event.ResourceType)
	}
	if event.ResourceID != "123" {
		t.Errorf("expected resource_id '123', got %q", event.ResourceID)
	}
}

func TestAuditMiddlewareLogsDeleteRequests(t *testing.T) {
	auditLog := &mockAuditLogger{}
	handler := AuditMiddleware(auditLog, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pools/456", nil)
	handler.ServeHTTP(rr, req)

	if len(auditLog.events) != 1 {
		t.Fatalf("expected 1 audit event for DELETE, got %d", len(auditLog.events))
	}

	event := auditLog.events[0]
	if event.Action != "delete" {
		t.Errorf("expected action 'delete', got %q", event.Action)
	}
	if event.ResourceID != "456" {
		t.Errorf("expected resource_id '456', got %q", event.ResourceID)
	}
}

func TestAuditMiddlewareSkipsHealthEndpoints(t *testing.T) {
	auditLog := &mockAuditLogger{}
	handler := AuditMiddleware(auditLog, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	endpoints := []string{"/healthz", "/readyz", "/metrics", "/openapi.yaml"}
	for _, ep := range endpoints {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, ep, nil)
		handler.ServeHTTP(rr, req)
	}

	if len(auditLog.events) != 0 {
		t.Errorf("expected no audit events for health endpoints, got %d", len(auditLog.events))
	}
}

func TestAuditMiddlewareCapturesActorFromContext(t *testing.T) {
	auditLog := &mockAuditLogger{}
	keyStore := auth.NewMemoryKeyStore()
	plaintext, _ := createTestKey(t, keyStore, "Test Key", []string{"*"})

	// Chain auth and audit middleware
	handler := ApplyMiddlewares(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}),
		AuthMiddleware(keyStore, false, newTestLogger()),
		AuditMiddleware(auditLog, newTestLogger()),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	handler.ServeHTTP(rr, req)

	if len(auditLog.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(auditLog.events))
	}

	event := auditLog.events[0]
	if event.Actor == "anonymous" {
		t.Error("expected actor to be API key prefix, got 'anonymous'")
	}
	if event.ActorType != "api_key" {
		t.Errorf("expected actor_type 'api_key', got %q", event.ActorType)
	}
}

func TestAuditMiddlewareAnonymousActor(t *testing.T) {
	auditLog := &mockAuditLogger{}
	handler := AuditMiddleware(auditLog, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", nil)
	handler.ServeHTTP(rr, req)

	if len(auditLog.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(auditLog.events))
	}

	event := auditLog.events[0]
	if event.Actor != "anonymous" {
		t.Errorf("expected actor 'anonymous', got %q", event.Actor)
	}
	if event.ActorType != "anonymous" {
		t.Errorf("expected actor_type 'anonymous', got %q", event.ActorType)
	}
}

func TestAuditMiddlewareNilLogger(t *testing.T) {
	// Should not panic with nil audit logger
	handler := AuditMiddleware(nil, newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rr.Code)
	}
}

func TestParseResourceFromPath(t *testing.T) {
	tests := []struct {
		path     string
		wantType string
		wantID   string
	}{
		{"/api/v1/pools", "pool", ""},
		{"/api/v1/pools/123", "pool", "123"},
		{"/api/v1/pools/123/blocks", "", ""}, // Skip blocks subroute
		{"/api/v1/accounts", "account", ""},
		{"/api/v1/accounts/456", "account", "456"},
		{"/api/v1/auth/keys", "api_key", ""},
		{"/api/v1/auth/keys/abc-123", "api_key", "abc-123"},
		{"/healthz", "", ""},
		{"/api/v1/unknown", "", ""},
		{"/other/path", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			gotType, gotID := parseResourceFromPath(tt.path)
			if gotType != tt.wantType {
				t.Errorf("parseResourceFromPath(%q) type = %q, want %q", tt.path, gotType, tt.wantType)
			}
			if gotID != tt.wantID {
				t.Errorf("parseResourceFromPath(%q) id = %q, want %q", tt.path, gotID, tt.wantID)
			}
		})
	}
}

func TestMethodToAction(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{http.MethodPost, "create"},
		{http.MethodPatch, "update"},
		{http.MethodPut, "update"},
		{http.MethodDelete, "delete"},
		{http.MethodGet, ""},
		{http.MethodHead, ""},
		{http.MethodOptions, ""},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := methodToAction(tt.method)
			if got != tt.want {
				t.Errorf("methodToAction(%q) = %q, want %q", tt.method, got, tt.want)
			}
		})
	}
}

// Tests for RBAC Authorization Middleware

func TestRequirePermissionMiddleware_AllowsAuthorized(t *testing.T) {
	logger := newTestLogger()

	// Handler that should be called
	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Apply middleware requiring pools:read permission
	mw := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionRead, logger)
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	// Set role as admin (has all permissions)
	ctx := auth.ContextWithRole(req.Context(), auth.RoleAdmin)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !handlerCalled {
		t.Error("handler should have been called")
	}
}

func TestRequirePermissionMiddleware_DeniesUnauthorized(t *testing.T) {
	logger := newTestLogger()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Require pools:create (viewer doesn't have this)
	mw := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionCreate, logger)
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	// Set role as viewer (read-only)
	ctx := auth.ContextWithRole(req.Context(), auth.RoleViewer)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
	if handlerCalled {
		t.Error("handler should not have been called")
	}

	// Check response body
	var resp apiError
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != "forbidden" {
		t.Errorf("expected error 'forbidden', got '%s'", resp.Error)
	}
}

func TestRequirePermissionMiddleware_DeniesNoRole(t *testing.T) {
	logger := newTestLogger()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionRead, logger)
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	// No role set in context

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	if handlerCalled {
		t.Error("handler should not have been called")
	}
}

func TestRequirePermissionMiddleware_UsesAPIKeyScopes(t *testing.T) {
	logger := newTestLogger()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionRead, logger)
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	// Set API key with read scope (should map to viewer role)
	apiKey := &auth.APIKey{
		ID:     "test-key",
		Scopes: []string{"pools:read"},
	}
	ctx := auth.ContextWithAPIKey(req.Context(), apiKey)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !handlerCalled {
		t.Error("handler should have been called")
	}
}

func TestRequireAnyPermissionMiddleware_AllowsWithOneMatch(t *testing.T) {
	logger := newTestLogger()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Require any of these permissions
	perms := []auth.Permission{
		{Resource: auth.ResourcePools, Action: auth.ActionCreate},
		{Resource: auth.ResourceAccounts, Action: auth.ActionRead},
	}
	mw := RequireAnyPermissionMiddleware(perms, logger)
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Viewer can read accounts but not create pools
	ctx := auth.ContextWithRole(req.Context(), auth.RoleViewer)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !handlerCalled {
		t.Error("handler should have been called")
	}
}

func TestRequireAnyPermissionMiddleware_DeniesWithNoMatch(t *testing.T) {
	logger := newTestLogger()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Require permissions that auditor doesn't have
	perms := []auth.Permission{
		{Resource: auth.ResourcePools, Action: auth.ActionCreate},
		{Resource: auth.ResourceAccounts, Action: auth.ActionCreate},
	}
	mw := RequireAnyPermissionMiddleware(perms, logger)
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	// Auditor can only access audit logs
	ctx := auth.ContextWithRole(req.Context(), auth.RoleAuditor)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
	if handlerCalled {
		t.Error("handler should not have been called")
	}
}

func TestRequireRoleMiddleware_AllowsEqualRole(t *testing.T) {
	logger := newTestLogger()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Require operator role
	mw := RequireRoleMiddleware(auth.RoleOperator, logger)
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// User has operator role (equal to required)
	ctx := auth.ContextWithRole(req.Context(), auth.RoleOperator)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !handlerCalled {
		t.Error("handler should have been called")
	}
}

func TestRequireRoleMiddleware_AllowsHigherRole(t *testing.T) {
	logger := newTestLogger()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Require viewer role
	mw := RequireRoleMiddleware(auth.RoleViewer, logger)
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// User has admin role (higher than viewer)
	ctx := auth.ContextWithRole(req.Context(), auth.RoleAdmin)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !handlerCalled {
		t.Error("handler should have been called")
	}
}

func TestRequireRoleMiddleware_DeniesLowerRole(t *testing.T) {
	logger := newTestLogger()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Require admin role
	mw := RequireRoleMiddleware(auth.RoleAdmin, logger)
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// User has viewer role (lower than admin)
	ctx := auth.ContextWithRole(req.Context(), auth.RoleViewer)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
	if handlerCalled {
		t.Error("handler should not have been called")
	}
}

func TestRequireRoleMiddleware_DeniesNoRole(t *testing.T) {
	logger := newTestLogger()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireRoleMiddleware(auth.RoleViewer, logger)
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No role set

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	if handlerCalled {
		t.Error("handler should not have been called")
	}
}

func TestRequirePermissionMiddleware_NilLoggerUsesDefault(t *testing.T) {
	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Pass nil logger
	mw := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionRead, nil)
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	ctx := auth.ContextWithRole(req.Context(), auth.RoleAdmin)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !handlerCalled {
		t.Error("handler should have been called")
	}
}
