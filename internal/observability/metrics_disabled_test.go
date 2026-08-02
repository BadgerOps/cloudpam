package observability

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// buildMetrics mirrors how cmd/cloudpam wires metrics: a disabled config yields
// no collector at all.
func buildMetrics(cfg MetricsConfig) *Metrics {
	if !cfg.Enabled {
		return nil
	}
	return NewMetrics(cfg)
}

func TestDisabledMetricsConfigCollectsNothing(t *testing.T) {
	orig, had := os.LookupEnv("CLOUDPAM_METRICS_ENABLED")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("CLOUDPAM_METRICS_ENABLED", orig)
			return
		}
		_ = os.Unsetenv("CLOUDPAM_METRICS_ENABLED")
	})
	_ = os.Setenv("CLOUDPAM_METRICS_ENABLED", "false")

	cfg := MetricsConfigFromEnv()
	if cfg.Enabled {
		t.Fatal("expected Enabled=false from env")
	}

	m := buildMetrics(cfg)
	if m != nil {
		t.Fatal("expected no collector when metrics are disabled")
	}

	// Every recording path must be inert rather than panicking.
	m.RecordHTTPRequest(http.MethodGet, "/api/v1/pools", 200, time.Second)
	m.RecordRateLimitAllowed()
	m.RecordRateLimitRejected()
	m.IncrementActiveConnections()
	m.DecrementActiveConnections()

	// The middleware stack must pass requests through untouched.
	var handlerCalled bool
	handler := MetricsMiddleware(m)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil))
	if !handlerCalled || rr.Code != http.StatusOK {
		t.Errorf("handler called = %v, status = %d; want true, 200", handlerCalled, rr.Code)
	}

	rr = httptest.NewRecorder()
	RateLimitMetricsMiddleware(m, true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("rate limit middleware status = %d, want 200", rr.Code)
	}
}

func TestDisabledMetricsHandlerDoesNotServe(t *testing.T) {
	m := NoopMetrics()

	rr := httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "http_requests_total") {
		t.Errorf("disabled handler exposed metrics: %q", rr.Body.String())
	}
}
