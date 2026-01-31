// Package observability provides structured logging, metrics, and tracing.
package observability

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MetricsConfig holds configuration for the metrics subsystem.
type MetricsConfig struct {
	// Enabled controls whether metrics collection is active.
	Enabled bool
	// Namespace prefix for all metrics (default: cloudpam).
	Namespace string
	// Version is the application version for the info metric.
	Version string
}

// DefaultMetricsConfig returns the default metrics configuration.
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Enabled:   true,
		Namespace: "cloudpam",
		Version:   "dev",
	}
}

// MetricsConfigFromEnv creates a MetricsConfig from environment variables.
// CLOUDPAM_METRICS_ENABLED: true/false (default: true)
// APP_VERSION: version string (default: dev)
func MetricsConfigFromEnv() MetricsConfig {
	cfg := DefaultMetricsConfig()

	if v := os.Getenv("CLOUDPAM_METRICS_ENABLED"); v != "" {
		cfg.Enabled = strings.ToLower(v) == "true" || v == "1"
	}
	if v := os.Getenv("APP_VERSION"); v != "" {
		cfg.Version = v
	}
	return cfg
}

// Metrics provides application metrics collection.
// Thread-safe for concurrent use.
type Metrics struct {
	mu        sync.RWMutex
	namespace string
	version   string

	// HTTP request counters: key = "method:path:status"
	httpRequestCounts map[string]*atomic.Int64

	// HTTP request durations: key = "method:path"
	// We store durations to compute quantiles on-demand.
	httpDurations  map[string]*durationCollector
	httpDurationMu sync.RWMutex

	// Rate limiter counters
	rateLimitAllowed  atomic.Int64
	rateLimitRejected atomic.Int64

	// Active connections gauge
	activeConnections atomic.Int64
}

// durationCollector collects duration samples for quantile computation.
// It keeps a sliding window of samples.
type durationCollector struct {
	mu      sync.Mutex
	samples []float64
	maxSize int
}

func newDurationCollector(maxSize int) *durationCollector {
	return &durationCollector{
		samples: make([]float64, 0, maxSize),
		maxSize: maxSize,
	}
}

func (d *durationCollector) add(duration time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	seconds := duration.Seconds()
	if len(d.samples) >= d.maxSize {
		// Remove oldest sample (simple ring buffer behavior)
		copy(d.samples, d.samples[1:])
		d.samples = d.samples[:len(d.samples)-1]
	}
	d.samples = append(d.samples, seconds)
}

func (d *durationCollector) quantile(q float64) float64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.samples) == 0 {
		return 0
	}

	// Make a copy and sort
	sorted := make([]float64, len(d.samples))
	copy(sorted, d.samples)
	sort.Float64s(sorted)

	// Calculate the index for the quantile
	idx := q * float64(len(sorted)-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}

	// Linear interpolation
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

func (d *durationCollector) sum() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	var total float64
	for _, s := range d.samples {
		total += s
	}
	return total
}

func (d *durationCollector) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.samples)
}

// NewMetrics creates a new Metrics collector.
func NewMetrics(cfg MetricsConfig) *Metrics {
	return &Metrics{
		namespace:         cfg.Namespace,
		version:           cfg.Version,
		httpRequestCounts: make(map[string]*atomic.Int64),
		httpDurations:     make(map[string]*durationCollector),
	}
}

// RecordHTTPRequest records an HTTP request with its method, path, status code, and duration.
func (m *Metrics) RecordHTTPRequest(method, path string, statusCode int, duration time.Duration) {
	// Normalize path to avoid high cardinality
	normalizedPath := normalizePath(path)

	// Increment request counter
	countKey := fmt.Sprintf("%s:%s:%d", method, normalizedPath, statusCode)
	m.mu.Lock()
	counter, ok := m.httpRequestCounts[countKey]
	if !ok {
		counter = &atomic.Int64{}
		m.httpRequestCounts[countKey] = counter
	}
	m.mu.Unlock()
	counter.Add(1)

	// Record duration
	durationKey := fmt.Sprintf("%s:%s", method, normalizedPath)
	m.httpDurationMu.Lock()
	collector, ok := m.httpDurations[durationKey]
	if !ok {
		collector = newDurationCollector(1000) // Keep last 1000 samples
		m.httpDurations[durationKey] = collector
	}
	m.httpDurationMu.Unlock()
	collector.add(duration)
}

// RecordRateLimitAllowed increments the count of allowed requests.
func (m *Metrics) RecordRateLimitAllowed() {
	m.rateLimitAllowed.Add(1)
}

// RecordRateLimitRejected increments the count of rejected requests.
func (m *Metrics) RecordRateLimitRejected() {
	m.rateLimitRejected.Add(1)
}

// IncrementActiveConnections increments the active connection gauge.
func (m *Metrics) IncrementActiveConnections() {
	m.activeConnections.Add(1)
}

// DecrementActiveConnections decrements the active connection gauge.
func (m *Metrics) DecrementActiveConnections() {
	m.activeConnections.Add(-1)
}

// normalizePath normalizes URL paths to reduce cardinality.
// It replaces numeric IDs with {id} placeholders.
func normalizePath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		// Replace numeric IDs with {id}
		if _, err := strconv.ParseInt(part, 10, 64); err == nil {
			parts[i] = "{id}"
		}
		// Replace UUIDs with {id}
		if len(part) == 36 && strings.Count(part, "-") == 4 {
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}

// Handler returns an http.Handler that serves Prometheus-format metrics.
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		m.writePrometheusMetrics(w)
	})
}

// writePrometheusMetrics writes all metrics in Prometheus text format.
func (m *Metrics) writePrometheusMetrics(w http.ResponseWriter) {
	// App info metric
	_, _ = fmt.Fprintf(w, "# HELP %s_info Application information\n", m.namespace)
	_, _ = fmt.Fprintf(w, "# TYPE %s_info gauge\n", m.namespace)
	_, _ = fmt.Fprintf(w, "%s_info{version=%q} 1\n\n", m.namespace, m.version)

	// HTTP request total
	fmt.Fprintf(w, "# HELP %s_http_requests_total Total number of HTTP requests\n", m.namespace)
	fmt.Fprintf(w, "# TYPE %s_http_requests_total counter\n", m.namespace)
	m.mu.RLock()
	// Sort keys for deterministic output
	keys := make([]string, 0, len(m.httpRequestCounts))
	for k := range m.httpRequestCounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		counter := m.httpRequestCounts[key]
		parts := strings.SplitN(key, ":", 3)
		if len(parts) == 3 {
			fmt.Fprintf(w, "%s_http_requests_total{method=%q,path=%q,status=%q} %d\n",
				m.namespace, parts[0], parts[1], parts[2], counter.Load())
		}
	}
	m.mu.RUnlock()
	_, _ = fmt.Fprintln(w)

	// HTTP request duration quantiles
	fmt.Fprintf(w, "# HELP %s_http_request_duration_seconds HTTP request duration in seconds\n", m.namespace)
	fmt.Fprintf(w, "# TYPE %s_http_request_duration_seconds summary\n", m.namespace)
	m.httpDurationMu.RLock()
	durationKeys := make([]string, 0, len(m.httpDurations))
	for k := range m.httpDurations {
		durationKeys = append(durationKeys, k)
	}
	sort.Strings(durationKeys)
	for _, key := range durationKeys {
		collector := m.httpDurations[key]
		parts := strings.SplitN(key, ":", 2)
		if len(parts) == 2 {
			method, path := parts[0], parts[1]
			// Output quantiles
			for _, q := range []float64{0.5, 0.9, 0.99} {
				val := collector.quantile(q)
				fmt.Fprintf(w, "%s_http_request_duration_seconds{method=%q,path=%q,quantile=\"%.2f\"} %.6f\n",
					m.namespace, method, path, q, val)
			}
			// Output sum and count
			fmt.Fprintf(w, "%s_http_request_duration_seconds_sum{method=%q,path=%q} %.6f\n",
				m.namespace, method, path, collector.sum())
			fmt.Fprintf(w, "%s_http_request_duration_seconds_count{method=%q,path=%q} %d\n",
				m.namespace, method, path, collector.count())
		}
	}
	m.httpDurationMu.RUnlock()
	_, _ = fmt.Fprintln(w)

	// Rate limiter metrics
	fmt.Fprintf(w, "# HELP %s_rate_limit_requests_total Total rate limit decisions\n", m.namespace)
	fmt.Fprintf(w, "# TYPE %s_rate_limit_requests_total counter\n", m.namespace)
	fmt.Fprintf(w, "%s_rate_limit_requests_total{status=\"allowed\"} %d\n", m.namespace, m.rateLimitAllowed.Load())
	fmt.Fprintf(w, "%s_rate_limit_requests_total{status=\"rejected\"} %d\n\n", m.namespace, m.rateLimitRejected.Load())

	// Active connections gauge
	fmt.Fprintf(w, "# HELP %s_active_connections Current number of active HTTP connections\n", m.namespace)
	fmt.Fprintf(w, "# TYPE %s_active_connections gauge\n", m.namespace)
	fmt.Fprintf(w, "%s_active_connections %d\n", m.namespace, m.activeConnections.Load())
}

// MetricsMiddleware returns an HTTP middleware that records request metrics.
func MetricsMiddleware(m *Metrics) func(http.Handler) http.Handler {
	if m == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip metrics endpoint itself to avoid recursion
			if r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			m.IncrementActiveConnections()
			defer m.DecrementActiveConnections()

			start := time.Now()

			// Wrap response writer to capture status code
			wrapped := &metricsResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)
			m.RecordHTTPRequest(r.Method, r.URL.Path, wrapped.statusCode, duration)
		})
	}
}

// metricsResponseWriter wraps http.ResponseWriter to capture the status code.
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *metricsResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap returns the underlying ResponseWriter for compatibility with
// http.ResponseController and other wrapping utilities.
func (w *metricsResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// RateLimitMetricsMiddleware returns middleware that records rate limit metrics.
// It should wrap the rate limiting middleware to capture allow/reject decisions.
func RateLimitMetricsMiddleware(m *Metrics, rateLimitEnabled bool) func(http.Handler) http.Handler {
	if m == nil || !rateLimitEnabled {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if this request was rate limited by looking at response status
			wrapped := &metricsResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			// After the request, check if it was rate limited
			if wrapped.statusCode == http.StatusTooManyRequests {
				m.RecordRateLimitRejected()
			} else {
				m.RecordRateLimitAllowed()
			}
		})
	}
}

// NoopMetrics returns a Metrics instance that doesn't collect anything.
// Useful for testing or when metrics are disabled.
func NoopMetrics() *Metrics {
	return nil
}

// GetMetrics extracts Metrics from context if present.
func GetMetrics(ctx context.Context) *Metrics {
	if m, ok := ctx.Value(metricsContextKey).(*Metrics); ok {
		return m
	}
	return nil
}

// WithMetrics adds Metrics to the context.
func WithMetrics(ctx context.Context, m *Metrics) context.Context {
	return context.WithValue(ctx, metricsContextKey, m)
}

// metricsContextKey is the context key for metrics.
type metricsContextKeyType string

const metricsContextKey metricsContextKeyType = "metrics"
