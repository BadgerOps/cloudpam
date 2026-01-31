package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewMetrics(t *testing.T) {
	cfg := MetricsConfig{
		Enabled:   true,
		Namespace: "test",
		Version:   "1.0.0",
	}
	m := NewMetrics(cfg)

	if m == nil {
		t.Fatal("expected non-nil Metrics")
	}
	if m.namespace != "test" {
		t.Errorf("expected namespace 'test', got %q", m.namespace)
	}
	if m.version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", m.version)
	}
}

func TestDefaultMetricsConfig(t *testing.T) {
	cfg := DefaultMetricsConfig()

	if !cfg.Enabled {
		t.Error("expected Enabled=true by default")
	}
	if cfg.Namespace != "cloudpam" {
		t.Errorf("expected namespace 'cloudpam', got %q", cfg.Namespace)
	}
	if cfg.Version != "dev" {
		t.Errorf("expected version 'dev', got %q", cfg.Version)
	}
}

func TestMetricsConfigFromEnv(t *testing.T) {
	// Save original env
	origEnabled := os.Getenv("CLOUDPAM_METRICS_ENABLED")
	origVersion := os.Getenv("APP_VERSION")
	defer func() {
		os.Setenv("CLOUDPAM_METRICS_ENABLED", origEnabled)
		os.Setenv("APP_VERSION", origVersion)
	}()

	// Test with custom values
	os.Setenv("CLOUDPAM_METRICS_ENABLED", "false")
	os.Setenv("APP_VERSION", "v2.0.0")

	cfg := MetricsConfigFromEnv()

	if cfg.Enabled {
		t.Error("expected Enabled=false from env")
	}
	if cfg.Version != "v2.0.0" {
		t.Errorf("expected version 'v2.0.0', got %q", cfg.Version)
	}
}

func TestMetricsConfigFromEnvEnabled(t *testing.T) {
	origEnabled := os.Getenv("CLOUDPAM_METRICS_ENABLED")
	defer os.Setenv("CLOUDPAM_METRICS_ENABLED", origEnabled)

	tests := []struct {
		envValue string
		want     bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"false", false},
		{"FALSE", false},
		{"0", false},
		{"", true}, // default
	}

	for _, tt := range tests {
		t.Run(tt.envValue, func(t *testing.T) {
			os.Setenv("CLOUDPAM_METRICS_ENABLED", tt.envValue)
			cfg := MetricsConfigFromEnv()
			if cfg.Enabled != tt.want {
				t.Errorf("expected Enabled=%v for env=%q, got %v", tt.want, tt.envValue, cfg.Enabled)
			}
		})
	}
}

func TestRecordHTTPRequest(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})

	m.RecordHTTPRequest("GET", "/api/v1/pools", 200, 100*time.Millisecond)
	m.RecordHTTPRequest("GET", "/api/v1/pools", 200, 200*time.Millisecond)
	m.RecordHTTPRequest("GET", "/api/v1/pools", 500, 50*time.Millisecond)
	m.RecordHTTPRequest("POST", "/api/v1/pools", 201, 150*time.Millisecond)

	// Verify counters
	m.mu.RLock()
	defer m.mu.RUnlock()

	get200Key := "GET:/api/v1/pools:200"
	if counter, ok := m.httpRequestCounts[get200Key]; !ok {
		t.Errorf("expected counter for %s", get200Key)
	} else if counter.Load() != 2 {
		t.Errorf("expected count 2, got %d", counter.Load())
	}

	get500Key := "GET:/api/v1/pools:500"
	if counter, ok := m.httpRequestCounts[get500Key]; !ok {
		t.Errorf("expected counter for %s", get500Key)
	} else if counter.Load() != 1 {
		t.Errorf("expected count 1, got %d", counter.Load())
	}
}

func TestRecordHTTPRequestPathNormalization(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})

	// Numeric IDs should be normalized
	m.RecordHTTPRequest("GET", "/api/v1/pools/123", 200, 100*time.Millisecond)
	m.RecordHTTPRequest("GET", "/api/v1/pools/456", 200, 100*time.Millisecond)

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Both should be counted under the same normalized path
	key := "GET:/api/v1/pools/{id}:200"
	if counter, ok := m.httpRequestCounts[key]; !ok {
		t.Errorf("expected counter for normalized path %s", key)
	} else if counter.Load() != 2 {
		t.Errorf("expected count 2 for normalized path, got %d", counter.Load())
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/api/v1/pools", "/api/v1/pools"},
		{"/api/v1/pools/123", "/api/v1/pools/{id}"},
		{"/api/v1/pools/123/blocks", "/api/v1/pools/{id}/blocks"},
		{"/api/v1/accounts/456", "/api/v1/accounts/{id}"},
		{"/healthz", "/healthz"},
		{"/", "/"},
		// UUID normalization
		{"/api/v1/pools/550e8400-e29b-41d4-a716-446655440000", "/api/v1/pools/{id}"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizePath(tt.input)
			if got != tt.want {
				t.Errorf("normalizePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRateLimitMetrics(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})

	m.RecordRateLimitAllowed()
	m.RecordRateLimitAllowed()
	m.RecordRateLimitAllowed()
	m.RecordRateLimitRejected()

	if m.rateLimitAllowed.Load() != 3 {
		t.Errorf("expected 3 allowed, got %d", m.rateLimitAllowed.Load())
	}
	if m.rateLimitRejected.Load() != 1 {
		t.Errorf("expected 1 rejected, got %d", m.rateLimitRejected.Load())
	}
}

func TestActiveConnections(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})

	m.IncrementActiveConnections()
	m.IncrementActiveConnections()
	m.IncrementActiveConnections()

	if m.activeConnections.Load() != 3 {
		t.Errorf("expected 3 active connections, got %d", m.activeConnections.Load())
	}

	m.DecrementActiveConnections()

	if m.activeConnections.Load() != 2 {
		t.Errorf("expected 2 active connections, got %d", m.activeConnections.Load())
	}
}

func TestMetricsHandler(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "cloudpam", Version: "1.0.0"})

	// Record some metrics
	m.RecordHTTPRequest("GET", "/api/v1/pools", 200, 100*time.Millisecond)
	m.RecordHTTPRequest("GET", "/api/v1/pools", 200, 200*time.Millisecond)
	m.RecordRateLimitAllowed()
	m.RecordRateLimitRejected()

	// Test the handler
	handler := m.Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Check for expected metrics in output
	expectedMetrics := []string{
		"cloudpam_info{version=\"1.0.0\"} 1",
		"cloudpam_http_requests_total{method=\"GET\",path=\"/api/v1/pools\",status=\"200\"} 2",
		"cloudpam_http_request_duration_seconds{method=\"GET\",path=\"/api/v1/pools\"",
		"cloudpam_rate_limit_requests_total{status=\"allowed\"} 1",
		"cloudpam_rate_limit_requests_total{status=\"rejected\"} 1",
		"cloudpam_active_connections 0",
	}

	for _, expected := range expectedMetrics {
		if !strings.Contains(body, expected) {
			t.Errorf("expected metric %q in output, body:\n%s", expected, body)
		}
	}

	// Check content type
	contentType := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/plain") {
		t.Errorf("expected Content-Type text/plain, got %q", contentType)
	}
}

func TestMetricsHandlerMethodNotAllowed(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})
	handler := m.Handler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}

func TestMetricsMiddleware(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := MetricsMiddleware(m)(innerHandler)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Verify metrics were recorded
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := "GET:/api/v1/pools:200"
	if counter, ok := m.httpRequestCounts[key]; !ok {
		t.Error("expected request to be recorded")
	} else if counter.Load() != 1 {
		t.Errorf("expected count 1, got %d", counter.Load())
	}
}

func TestMetricsMiddlewareSkipsMetricsEndpoint(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := MetricsMiddleware(m)(innerHandler)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	handler.ServeHTTP(rr, req)

	// Verify /metrics was NOT recorded to avoid recursion
	m.mu.RLock()
	defer m.mu.RUnlock()

	for key := range m.httpRequestCounts {
		if strings.Contains(key, "/metrics") {
			t.Error("metrics endpoint should not be recorded")
		}
	}
}

func TestMetricsMiddlewareNil(t *testing.T) {
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := MetricsMiddleware(nil)(innerHandler)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
}

func TestMetricsMiddlewareActiveConnections(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})

	var activeCount int64
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture active connections during request
		activeCount = m.activeConnections.Load()
		w.WriteHeader(http.StatusOK)
	})

	handler := MetricsMiddleware(m)(innerHandler)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	handler.ServeHTTP(rr, req)

	// During request, active connections should be 1
	if activeCount != 1 {
		t.Errorf("expected 1 active connection during request, got %d", activeCount)
	}

	// After request, should be 0
	if m.activeConnections.Load() != 0 {
		t.Errorf("expected 0 active connections after request, got %d", m.activeConnections.Load())
	}
}

func TestDurationCollector(t *testing.T) {
	d := newDurationCollector(5)

	// Add some durations
	d.add(100 * time.Millisecond)
	d.add(200 * time.Millisecond)
	d.add(300 * time.Millisecond)
	d.add(400 * time.Millisecond)
	d.add(500 * time.Millisecond)

	// Test count
	if d.count() != 5 {
		t.Errorf("expected count 5, got %d", d.count())
	}

	// Test sum (should be 1.5 seconds)
	sum := d.sum()
	if sum < 1.4 || sum > 1.6 {
		t.Errorf("expected sum around 1.5s, got %f", sum)
	}

	// Test quantiles
	p50 := d.quantile(0.5)
	if p50 < 0.25 || p50 > 0.35 {
		t.Errorf("expected p50 around 0.3s, got %f", p50)
	}

	p99 := d.quantile(0.99)
	if p99 < 0.45 || p99 > 0.55 {
		t.Errorf("expected p99 around 0.5s, got %f", p99)
	}
}

func TestDurationCollectorMaxSize(t *testing.T) {
	d := newDurationCollector(3)

	// Add more than max size
	d.add(100 * time.Millisecond)
	d.add(200 * time.Millisecond)
	d.add(300 * time.Millisecond)
	d.add(400 * time.Millisecond) // Should push out 100ms

	// Count should be capped at max size
	if d.count() != 3 {
		t.Errorf("expected count 3, got %d", d.count())
	}

	// First sample should have been removed
	// So samples should be [200ms, 300ms, 400ms]
	sum := d.sum()
	if sum < 0.85 || sum > 0.95 {
		t.Errorf("expected sum around 0.9s (200+300+400ms), got %f", sum)
	}
}

func TestDurationCollectorEmpty(t *testing.T) {
	d := newDurationCollector(5)

	if d.count() != 0 {
		t.Errorf("expected count 0, got %d", d.count())
	}
	if d.sum() != 0 {
		t.Errorf("expected sum 0, got %f", d.sum())
	}
	if d.quantile(0.5) != 0 {
		t.Errorf("expected quantile 0, got %f", d.quantile(0.5))
	}
}

func TestMetricsContext(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})

	ctx := context.Background()
	ctx = WithMetrics(ctx, m)

	got := GetMetrics(ctx)
	if got != m {
		t.Error("expected to get metrics from context")
	}
}

func TestGetMetricsNilContext(t *testing.T) {
	ctx := context.Background()
	got := GetMetrics(ctx)
	if got != nil {
		t.Error("expected nil metrics from empty context")
	}
}

func TestNoopMetrics(t *testing.T) {
	m := NoopMetrics()
	if m != nil {
		t.Error("expected NoopMetrics to return nil")
	}
}

func TestRateLimitMetricsMiddleware(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})

	// Simulate a handler that returns 429 for rate limiting
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rate-limited" {
			w.WriteHeader(http.StatusTooManyRequests)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	})

	handler := RateLimitMetricsMiddleware(m, true)(innerHandler)

	// Normal request
	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	handler.ServeHTTP(rr1, req1)

	// Rate limited request
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/rate-limited", nil)
	handler.ServeHTTP(rr2, req2)

	if m.rateLimitAllowed.Load() != 1 {
		t.Errorf("expected 1 allowed, got %d", m.rateLimitAllowed.Load())
	}
	if m.rateLimitRejected.Load() != 1 {
		t.Errorf("expected 1 rejected, got %d", m.rateLimitRejected.Load())
	}
}

func TestRateLimitMetricsMiddlewareDisabled(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Rate limiting disabled
	handler := RateLimitMetricsMiddleware(m, false)(innerHandler)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	handler.ServeHTTP(rr, req)

	// Metrics should not be recorded when disabled
	if m.rateLimitAllowed.Load() != 0 {
		t.Errorf("expected 0 allowed when disabled, got %d", m.rateLimitAllowed.Load())
	}
}

func TestRateLimitMetricsMiddlewareNilMetrics(t *testing.T) {
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := RateLimitMetricsMiddleware(nil, true)(innerHandler)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	handler.ServeHTTP(rr, req)

	// Should pass through without panic
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
}

func TestMetricsResponseWriterUnwrap(t *testing.T) {
	inner := httptest.NewRecorder()
	wrapped := &metricsResponseWriter{ResponseWriter: inner, statusCode: http.StatusOK}

	unwrapped := wrapped.Unwrap()
	if unwrapped != inner {
		t.Error("Unwrap() should return the inner ResponseWriter")
	}
}

func TestMetricsConcurrentAccess(t *testing.T) {
	m := NewMetrics(MetricsConfig{Namespace: "test", Version: "1.0.0"})

	// Simulate concurrent requests
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func(i int) {
			m.RecordHTTPRequest("GET", "/api/v1/pools", 200, time.Duration(i)*time.Millisecond)
			m.RecordRateLimitAllowed()
			m.IncrementActiveConnections()
			m.DecrementActiveConnections()
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	// Verify no race conditions and counts are correct
	m.mu.RLock()
	counter := m.httpRequestCounts["GET:/api/v1/pools:200"]
	m.mu.RUnlock()

	if counter.Load() != 100 {
		t.Errorf("expected 100 requests recorded, got %d", counter.Load())
	}
	if m.rateLimitAllowed.Load() != 100 {
		t.Errorf("expected 100 allowed, got %d", m.rateLimitAllowed.Load())
	}
	if m.activeConnections.Load() != 0 {
		t.Errorf("expected 0 active connections, got %d", m.activeConnections.Load())
	}
}
