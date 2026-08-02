package observability

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// recordingExporter captures exported spans in memory.
type recordingExporter struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func (r *recordingExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, spans...)
	return nil
}

func (r *recordingExporter) Shutdown(context.Context) error { return nil }

func (r *recordingExporter) recorded() []sdktrace.ReadOnlySpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]sdktrace.ReadOnlySpan, len(r.spans))
	copy(out, r.spans)
	return out
}

// clearTracingEnv neutralises tracing environment variables for a test.
func clearTracingEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"CLOUDPAM_TRACING_ENABLED",
		"CLOUDPAM_TRACING_ENDPOINT",
		"CLOUDPAM_TRACING_SAMPLE_RATE",
		"APP_VERSION",
	} {
		t.Setenv(key, "")
	}
}

func TestDefaultTracingConfigIsOptIn(t *testing.T) {
	cfg := DefaultTracingConfig()

	if cfg.Enabled {
		t.Fatalf("tracing must default to disabled, got Enabled=true")
	}
	// The zero value must agree with the documented default, otherwise a
	// literal that omits Enabled is indistinguishable from an explicit "off".
	var zero TracingConfig
	if zero.Enabled != cfg.Enabled {
		t.Fatalf("zero value Enabled=%v disagrees with default Enabled=%v", zero.Enabled, cfg.Enabled)
	}
	if cfg.SampleRate != 1.0 {
		t.Errorf("SampleRate = %v, want 1.0", cfg.SampleRate)
	}
	if cfg.Endpoint != defaultTracingEndpoint {
		t.Errorf("Endpoint = %q, want %q", cfg.Endpoint, defaultTracingEndpoint)
	}
	if cfg.ServiceName != "cloudpam" {
		t.Errorf("ServiceName = %q, want cloudpam", cfg.ServiceName)
	}
	if cfg.ExportTimeout <= 0 {
		t.Errorf("ExportTimeout = %v, want a positive duration", cfg.ExportTimeout)
	}
}

func TestTracingConfigFromEnvDefaultsToDisabled(t *testing.T) {
	clearTracingEnv(t)

	cfg, err := TracingConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Enabled {
		t.Errorf("Enabled = true, want false when no env is set")
	}
	if cfg != DefaultTracingConfig() {
		t.Errorf("config = %+v, want defaults %+v", cfg, DefaultTracingConfig())
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("disabled config must validate, got %v", err)
	}
}

func TestTracingConfigFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantEnabled bool
		wantRate    float64
		wantEndpt   string
		wantVersion string
		wantErr     bool
	}{
		{
			name:        "enabled true",
			env:         map[string]string{"CLOUDPAM_TRACING_ENABLED": "true"},
			wantEnabled: true,
			wantRate:    1.0,
			wantEndpt:   defaultTracingEndpoint,
			wantVersion: "dev",
		},
		{
			name:        "enabled 1",
			env:         map[string]string{"CLOUDPAM_TRACING_ENABLED": "1"},
			wantEnabled: true,
			wantRate:    1.0,
			wantEndpt:   defaultTracingEndpoint,
			wantVersion: "dev",
		},
		{
			name:        "enabled mixed case",
			env:         map[string]string{"CLOUDPAM_TRACING_ENABLED": "TRUE"},
			wantEnabled: true,
			wantRate:    1.0,
			wantEndpt:   defaultTracingEndpoint,
			wantVersion: "dev",
		},
		{
			name:        "explicitly disabled",
			env:         map[string]string{"CLOUDPAM_TRACING_ENABLED": "false"},
			wantEnabled: false,
			wantRate:    1.0,
			wantEndpt:   defaultTracingEndpoint,
			wantVersion: "dev",
		},
		{
			name: "endpoint sample rate and version",
			env: map[string]string{
				"CLOUDPAM_TRACING_ENABLED":     "true",
				"CLOUDPAM_TRACING_ENDPOINT":    "http://collector:4318",
				"CLOUDPAM_TRACING_SAMPLE_RATE": "0.25",
				"APP_VERSION":                  "v1.2.3",
			},
			wantEnabled: true,
			wantRate:    0.25,
			wantEndpt:   "http://collector:4318",
			wantVersion: "v1.2.3",
		},
		{
			name: "unparsable sample rate reports an error and keeps the default",
			env: map[string]string{
				"CLOUDPAM_TRACING_ENABLED":     "true",
				"CLOUDPAM_TRACING_SAMPLE_RATE": "half",
			},
			wantEnabled: true,
			wantRate:    1.0,
			wantEndpt:   defaultTracingEndpoint,
			wantVersion: "dev",
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearTracingEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, err := TracingConfigFromEnv()
			if tc.wantErr && err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Enabled != tc.wantEnabled {
				t.Errorf("Enabled = %v, want %v", cfg.Enabled, tc.wantEnabled)
			}
			if cfg.SampleRate != tc.wantRate {
				t.Errorf("SampleRate = %v, want %v", cfg.SampleRate, tc.wantRate)
			}
			if cfg.Endpoint != tc.wantEndpt {
				t.Errorf("Endpoint = %q, want %q", cfg.Endpoint, tc.wantEndpt)
			}
			if cfg.ServiceVersion != tc.wantVersion {
				t.Errorf("ServiceVersion = %q, want %q", cfg.ServiceVersion, tc.wantVersion)
			}
		})
	}
}

func TestTracingConfigValidate(t *testing.T) {
	base := DefaultTracingConfig()
	base.Enabled = true

	tests := []struct {
		name    string
		mutate  func(*TracingConfig)
		wantErr string
	}{
		{name: "valid", mutate: func(*TracingConfig) {}},
		{
			name:    "missing endpoint",
			mutate:  func(c *TracingConfig) { c.Endpoint = "  " },
			wantErr: "tracing endpoint is required",
		},
		{
			name:    "bad scheme",
			mutate:  func(c *TracingConfig) { c.Endpoint = "grpc://collector:4317" },
			wantErr: "scheme must be http or https",
		},
		{
			// The zero value of SampleRate would silently sample nothing, so
			// it is rejected rather than treated as a valid configuration.
			name:    "zero sample rate",
			mutate:  func(c *TracingConfig) { c.SampleRate = 0 },
			wantErr: "sample rate must be greater than 0",
		},
		{
			name:    "sample rate above one",
			mutate:  func(c *TracingConfig) { c.SampleRate = 1.5 },
			wantErr: "sample rate must be greater than 0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)

			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestTracingConfigValidateIgnoresFieldsWhenDisabled(t *testing.T) {
	// A disabled config is never inspected, so garbage in the other fields
	// cannot make startup fail.
	cfg := TracingConfig{Endpoint: "not a url", SampleRate: -3}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled config must validate, got %v", err)
	}
}

func TestNewTracerProviderDisabledReturnsNil(t *testing.T) {
	cfg := DefaultTracingConfig()

	tp, err := NewTracerProvider(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp != nil {
		t.Fatalf("expected a nil provider when tracing is disabled, got %#v", tp)
	}

	// Every method on the nil provider must be safe so callers never need a
	// tracing-enabled branch.
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Errorf("nil Shutdown returned %v", err)
	}
	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Errorf("nil ForceFlush returned %v", err)
	}
	_, span := tp.Tracer().Start(context.Background(), "noop")
	if span.SpanContext().IsValid() {
		t.Errorf("nil provider produced a recording span")
	}
	span.End()
}

func TestNewTracerProviderRejectsInvalidConfig(t *testing.T) {
	cfg := DefaultTracingConfig()
	cfg.Enabled = true
	cfg.Endpoint = ""

	tp, err := NewTracerProvider(cfg, nil)
	if err == nil {
		t.Fatalf("expected an error for an enabled config without an endpoint")
	}
	if tp != nil {
		t.Errorf("expected a nil provider on error, got %#v", tp)
	}
}

func TestTracingMiddlewareDisabledIsPassthrough(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	wrapped := TracingMiddleware(nil)(handler)

	// The disabled middleware must return the very same handler: nothing is
	// installed in the chain, so a request costs exactly what it did before.
	if reflect.ValueOf(wrapped).Pointer() != reflect.ValueOf(handler).Pointer() {
		t.Fatalf("disabled middleware wrapped the handler instead of returning it unchanged")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if got := rec.Header().Get("X-Trace-Id"); got != "" {
		t.Errorf("X-Trace-Id = %q, want empty when tracing is disabled", got)
	}
}

func TestTracingMiddlewareEmitsSpan(t *testing.T) {
	exporter := &recordingExporter{}
	cfg := DefaultTracingConfig()
	cfg.Enabled = true
	tp := newTracerProvider(cfg, exporter)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	var seenTraceID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenTraceID = TraceIDFromContext(r.Context())
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools/42", nil)
	req.Header.Set("User-Agent", "cloudpam-test")
	rec := httptest.NewRecorder()
	TracingMiddleware(tp)(handler).ServeHTTP(rec, req)

	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush: %v", err)
	}

	spans := exporter.recorded()
	if len(spans) != 1 {
		t.Fatalf("exported %d spans, want 1", len(spans))
	}
	span := spans[0]

	if got, want := span.Name(), "POST /api/v1/pools/{id}"; got != want {
		t.Errorf("span name = %q, want %q", got, want)
	}
	attrs := map[string]string{}
	ints := map[string]int64{}
	for _, attr := range span.Attributes() {
		switch attr.Value.Type().String() {
		case "INT64":
			ints[string(attr.Key)] = attr.Value.AsInt64()
		default:
			attrs[string(attr.Key)] = attr.Value.String()
		}
	}
	if attrs["http.request.method"] != http.MethodPost {
		t.Errorf("http.request.method = %q, want POST", attrs["http.request.method"])
	}
	if attrs["http.route"] != "/api/v1/pools/{id}" {
		t.Errorf("http.route = %q, want /api/v1/pools/{id}", attrs["http.route"])
	}
	if attrs["url.path"] != "/api/v1/pools/42" {
		t.Errorf("url.path = %q, want /api/v1/pools/42", attrs["url.path"])
	}
	if attrs["user_agent.original"] != "cloudpam-test" {
		t.Errorf("user_agent.original = %q, want cloudpam-test", attrs["user_agent.original"])
	}
	if ints["http.response.status_code"] != int64(http.StatusCreated) {
		t.Errorf("http.response.status_code = %d, want %d", ints["http.response.status_code"], http.StatusCreated)
	}

	// The trace ID must reach both the handler context (for log correlation)
	// and the response header (for client-side correlation).
	wantTraceID := span.SpanContext().TraceID().String()
	if seenTraceID != wantTraceID {
		t.Errorf("context trace id = %q, want %q", seenTraceID, wantTraceID)
	}
	if got := rec.Header().Get("X-Trace-Id"); got != wantTraceID {
		t.Errorf("X-Trace-Id = %q, want %q", got, wantTraceID)
	}
}

func TestTracingMiddlewareMarksServerErrors(t *testing.T) {
	exporter := &recordingExporter{}
	cfg := DefaultTracingConfig()
	cfg.Enabled = true
	tp := newTracerProvider(cfg, exporter)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	TracingMiddleware(tp)(handler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil))

	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush: %v", err)
	}
	spans := exporter.recorded()
	if len(spans) != 1 {
		t.Fatalf("exported %d spans, want 1", len(spans))
	}
	if got := spans[0].Status().Code.String(); got != "Error" {
		t.Errorf("status = %s, want Error", got)
	}
}

func TestTracingMiddlewareContinuesIncomingTrace(t *testing.T) {
	exporter := &recordingExporter{}
	cfg := DefaultTracingConfig()
	cfg.Enabled = true
	tp := newTracerProvider(cfg, exporter)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	const parentTrace = "0af7651916cd43dd8448eb211c80319c"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	req.Header.Set("traceparent", "00-"+parentTrace+"-b7ad6b7169203331-01")

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	TracingMiddleware(tp)(handler).ServeHTTP(httptest.NewRecorder(), req)

	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush: %v", err)
	}
	spans := exporter.recorded()
	if len(spans) != 1 {
		t.Fatalf("exported %d spans, want 1", len(spans))
	}
	if got := spans[0].SpanContext().TraceID().String(); got != parentTrace {
		t.Errorf("trace id = %q, want the incoming %q", got, parentTrace)
	}
}

func TestTracingMiddlewareSkipsOperationalEndpoints(t *testing.T) {
	exporter := &recordingExporter{}
	cfg := DefaultTracingConfig()
	cfg.Enabled = true
	tp := newTracerProvider(cfg, exporter)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		TracingMiddleware(tp)(handler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush: %v", err)
	}
	if spans := exporter.recorded(); len(spans) != 0 {
		t.Errorf("exported %d spans for operational endpoints, want 0", len(spans))
	}
}

// plainWriter implements only http.ResponseWriter.
type plainWriter struct{ header http.Header }

func (p *plainWriter) Header() http.Header {
	if p.header == nil {
		p.header = http.Header{}
	}
	return p.header
}
func (p *plainWriter) Write(b []byte) (int, error) { return len(b), nil }
func (p *plainWriter) WriteHeader(int)             {}

// fullWriter implements every optional interface the wrapper forwards.
type fullWriter struct {
	plainWriter
	flushed  bool
	hijacked bool
	readFrom bool
}

func (f *fullWriter) Flush() { f.flushed = true }
func (f *fullWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	f.hijacked = true
	return nil, nil, errors.New("hijack not supported in test")
}
func (f *fullWriter) ReadFrom(r io.Reader) (int64, error) {
	f.readFrom = true
	return io.Copy(io.Discard, r)
}

func TestWrapTracingResponseWriterForwardsOptionalInterfaces(t *testing.T) {
	full := &fullWriter{}
	wrapped, recorder := wrapTracingResponseWriter(full)

	flusher, ok := wrapped.(http.Flusher)
	if !ok {
		t.Fatalf("wrapper dropped http.Flusher; SSE streaming would break")
	}
	flusher.Flush()
	if !full.flushed {
		t.Errorf("Flush did not reach the underlying writer")
	}

	hijacker, ok := wrapped.(http.Hijacker)
	if !ok {
		t.Fatalf("wrapper dropped http.Hijacker")
	}
	if _, _, err := hijacker.Hijack(); err == nil {
		t.Errorf("expected the test hijacker error")
	}
	if !full.hijacked {
		t.Errorf("Hijack did not reach the underlying writer")
	}

	readerFrom, ok := wrapped.(io.ReaderFrom)
	if !ok {
		t.Fatalf("wrapper dropped io.ReaderFrom")
	}
	if _, err := readerFrom.ReadFrom(strings.NewReader("hello")); err != nil {
		t.Errorf("ReadFrom: %v", err)
	}
	if !full.readFrom {
		t.Errorf("ReadFrom did not reach the underlying writer")
	}

	// Status capture still works through the wrapper.
	wrapped.WriteHeader(http.StatusNotFound)
	if recorder.status != http.StatusNotFound {
		t.Errorf("recorded status = %d, want %d", recorder.status, http.StatusNotFound)
	}
	if unwrapped, ok := wrapped.(interface{ Unwrap() http.ResponseWriter }); !ok {
		t.Errorf("wrapper does not support Unwrap for http.ResponseController")
	} else if unwrapped.Unwrap() != http.ResponseWriter(full) {
		t.Errorf("Unwrap returned the wrong writer")
	}
}

func TestWrapTracingResponseWriterDoesNotInventInterfaces(t *testing.T) {
	// Feature detection must stay honest: a writer that cannot flush must not
	// look like it can.
	wrapped, _ := wrapTracingResponseWriter(&plainWriter{})

	if _, ok := wrapped.(http.Flusher); ok {
		t.Errorf("wrapper claims http.Flusher for a writer that has none")
	}
	if _, ok := wrapped.(http.Hijacker); ok {
		t.Errorf("wrapper claims http.Hijacker for a writer that has none")
	}
	if _, ok := wrapped.(io.ReaderFrom); ok {
		t.Errorf("wrapper claims io.ReaderFrom for a writer that has none")
	}
}

func TestTracingMiddlewarePreservesFlusher(t *testing.T) {
	exporter := &recordingExporter{}
	cfg := DefaultTracingConfig()
	cfg.Enabled = true
	tp := newTracerProvider(cfg, exporter)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	var sawFlusher bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		sawFlusher = ok
		if ok {
			_, _ = w.Write([]byte("data: hi\n\n"))
			flusher.Flush()
		}
	})

	// httptest.ResponseRecorder implements http.Flusher, matching the real
	// net/http writer used by the SSE endpoints.
	TracingMiddleware(tp)(handler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/ai/chat", nil))

	if !sawFlusher {
		t.Fatalf("handler could not type-assert http.Flusher through the tracing middleware")
	}
}

func TestOTLPExporterPostsJSON(t *testing.T) {
	type capture struct {
		contentType string
		body        []byte
	}
	received := make(chan capture, 1)

	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/traces" {
			t.Errorf("collector path = %q, want /v1/traces", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("collector method = %q, want POST", r.Method)
		}
		received <- capture{contentType: r.Header.Get("Content-Type"), body: body}
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	cfg := DefaultTracingConfig()
	cfg.Enabled = true
	cfg.Endpoint = collector.URL
	cfg.ServiceVersion = "v9.9.9"

	exporter, err := newOTLPHTTPExporter(cfg)
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}
	tp := newTracerProvider(cfg, exporter)

	_, span := tp.Tracer().Start(context.Background(), "unit.span")
	span.End()

	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush: %v", err)
	}
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	var got capture
	select {
	case got = <-received:
	case <-time.After(5 * time.Second):
		t.Fatalf("collector never received an export")
	}

	if !strings.HasPrefix(got.contentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got.contentType)
	}

	var payload otlpPayload
	if err := json.Unmarshal(got.body, &payload); err != nil {
		t.Fatalf("payload is not valid JSON: %v\n%s", err, got.body)
	}
	if len(payload.ResourceSpans) != 1 {
		t.Fatalf("resourceSpans = %d, want 1", len(payload.ResourceSpans))
	}
	rs := payload.ResourceSpans[0]

	var serviceName, serviceVersion string
	for _, attr := range rs.Resource.Attributes {
		if attr.Value.StringValue == nil {
			continue
		}
		switch attr.Key {
		case "service.name":
			serviceName = *attr.Value.StringValue
		case "service.version":
			serviceVersion = *attr.Value.StringValue
		}
	}
	if serviceName != "cloudpam" {
		t.Errorf("service.name = %q, want cloudpam", serviceName)
	}
	if serviceVersion != "v9.9.9" {
		t.Errorf("service.version = %q, want v9.9.9", serviceVersion)
	}

	if len(rs.ScopeSpans) != 1 || len(rs.ScopeSpans[0].Spans) != 1 {
		t.Fatalf("expected exactly one scope with one span, got %+v", rs.ScopeSpans)
	}
	if got, want := rs.ScopeSpans[0].Scope.Name, tracerName; got != want {
		t.Errorf("scope name = %q, want %q", got, want)
	}

	otlpSpan := rs.ScopeSpans[0].Spans[0]
	if otlpSpan.Name != "unit.span" {
		t.Errorf("span name = %q, want unit.span", otlpSpan.Name)
	}
	// OTLP JSON encodes IDs as hex and 64-bit values as strings.
	if len(otlpSpan.TraceID) != 32 {
		t.Errorf("traceId = %q, want 32 hex characters", otlpSpan.TraceID)
	}
	if len(otlpSpan.SpanID) != 16 {
		t.Errorf("spanId = %q, want 16 hex characters", otlpSpan.SpanID)
	}
	if otlpSpan.StartTimeUnixNano == "" || otlpSpan.EndTimeUnixNano == "" {
		t.Errorf("timestamps missing: start=%q end=%q", otlpSpan.StartTimeUnixNano, otlpSpan.EndTimeUnixNano)
	}
}

func TestOTLPExporterTolerantOfUnreachableCollector(t *testing.T) {
	// Bind and immediately release a port so nothing is listening on it.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadEndpoint := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	cfg := DefaultTracingConfig()
	cfg.Enabled = true
	cfg.Endpoint = deadEndpoint
	cfg.ExportTimeout = 2 * time.Second

	// Construction must succeed: nothing is dialled until spans are exported,
	// so a collector that is down cannot stop the server from starting.
	tp, err := NewTracerProvider(cfg, nil)
	if err != nil {
		t.Fatalf("provider construction failed for an unreachable collector: %v", err)
	}
	if tp == nil {
		t.Fatalf("expected a provider")
	}

	// Serving a request must still work end to end.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	TracingMiddleware(tp)(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 despite the dead collector", rec.Code)
	}

	// The export fails, but only the export: flush reports it and shutdown
	// still completes.
	if err := tp.ForceFlush(context.Background()); err == nil {
		t.Logf("force flush succeeded; batch may not have been drained yet")
	}
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Errorf("shutdown returned %v, want nil", err)
	}
}

func TestOTLPExporterStopsAfterShutdown(t *testing.T) {
	var calls int
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	cfg := DefaultTracingConfig()
	cfg.Enabled = true
	cfg.Endpoint = collector.URL

	exporter, err := newOTLPHTTPExporter(cfg)
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}
	if err := exporter.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	// Recorded spans are dropped rather than posted once the exporter is shut
	// down, and doing so is not an error.
	recorder := &recordingExporter{}
	tp := newTracerProvider(cfg, recorder)
	_, span := tp.Tracer().Start(context.Background(), "unit.span")
	span.End()
	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush: %v", err)
	}
	spans := recorder.recorded()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}

	if err := exporter.ExportSpans(context.Background(), spans); err != nil {
		t.Errorf("export after shutdown returned %v, want nil", err)
	}
	if calls != 0 {
		t.Errorf("collector received %d requests after shutdown, want 0", calls)
	}
	_ = tp.Shutdown(context.Background())
}

func TestOtlpTracesURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
		wantErr  bool
	}{
		{name: "base url", endpoint: "http://localhost:4318", want: "http://localhost:4318/v1/traces"},
		{name: "trailing slash", endpoint: "http://localhost:4318/", want: "http://localhost:4318/v1/traces"},
		{name: "bare host port defaults to http", endpoint: "collector:4318", want: "http://collector:4318/v1/traces"},
		{name: "https preserved", endpoint: "https://otel.example.com", want: "https://otel.example.com/v1/traces"},
		{name: "explicit path kept", endpoint: "https://otel.example.com/ingest/v1/traces", want: "https://otel.example.com/ingest/v1/traces"},
		{name: "whitespace trimmed", endpoint: "  http://localhost:4318  ", want: "http://localhost:4318/v1/traces"},
		{name: "empty", endpoint: "", wantErr: true},
		{name: "unsupported scheme", endpoint: "grpc://localhost:4317", wantErr: true},
		{name: "missing host", endpoint: "http://", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := otlpTracesURL(tc.endpoint)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got %q", tc.endpoint, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("otlpTracesURL(%q) = %q, want %q", tc.endpoint, got, tc.want)
			}
		})
	}
}

func TestTraceContextHelpers(t *testing.T) {
	ctx := context.Background()
	if got := TraceIDFromContext(ctx); got != "" {
		t.Errorf("TraceIDFromContext on a bare context = %q, want empty", got)
	}
	if got := SpanIDFromContext(ctx); got != "" {
		t.Errorf("SpanIDFromContext on a bare context = %q, want empty", got)
	}

	// An empty trace ID must not put anything in the context.
	if WithTraceContext(ctx, "", "span") != ctx {
		t.Errorf("WithTraceContext stored an empty trace id")
	}

	traced := WithTraceContext(ctx, "trace-abc", "span-def")
	if got := TraceIDFromContext(traced); got != "trace-abc" {
		t.Errorf("TraceIDFromContext = %q, want trace-abc", got)
	}
	if got := SpanIDFromContext(traced); got != "span-def" {
		t.Errorf("SpanIDFromContext = %q, want span-def", got)
	}
}

func TestLoggerIncludesTraceFieldsOnlyWhenTraced(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(Config{Level: "info", Format: "json", Output: &buf})

	logger.InfoContext(context.Background(), "untraced")
	if strings.Contains(buf.String(), "trace_id") {
		t.Errorf("untraced log record carries a trace_id: %s", buf.String())
	}

	buf.Reset()
	logger.InfoContext(WithTraceContext(context.Background(), "trace-abc", "span-def"), "traced")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("log record is not JSON: %v\n%s", err, buf.String())
	}
	if record["trace_id"] != "trace-abc" {
		t.Errorf("trace_id = %v, want trace-abc", record["trace_id"])
	}
	if record["span_id"] != "span-def" {
		t.Errorf("span_id = %v, want span-def", record["span_id"])
	}
}

func TestPackageTracerIsUsableWithoutAProvider(t *testing.T) {
	// Outbound instrumentation (LLM, AWS) uses the package tracer directly.
	// With tracing disabled it must be a harmless no-op rather than a panic.
	ctx, span := Tracer().Start(context.Background(), "provider.call")
	span.End()
	if ctx == nil {
		t.Fatalf("Tracer().Start returned a nil context")
	}
}
