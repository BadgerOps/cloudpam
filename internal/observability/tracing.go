// Package observability provides structured logging, metrics, and tracing.
package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// tracerName is the instrumentation scope name used for every CloudPAM span.
const tracerName = "cloudpam"

// defaultTracingEndpoint is the conventional OTLP/HTTP receiver address.
const defaultTracingEndpoint = "http://localhost:4318"

// TracingConfig holds configuration for the tracing subsystem.
//
// Tracing is opt-in: the zero value of Enabled (false) is the same as the
// documented default, so an unset field and an explicit "off" always mean the
// same thing. Every other field is only consulted when Enabled is true, and
// NewTracerProvider rejects a config that is enabled but incomplete rather than
// silently falling back — see Validate.
type TracingConfig struct {
	// Enabled controls whether spans are recorded and exported.
	// Default and zero value: false (tracing is opt-in).
	Enabled bool
	// Endpoint is the OTLP/HTTP base URL of the collector, for example
	// "http://localhost:4318". A bare "host:port" is treated as http. A URL
	// with a path is used verbatim; otherwise "/v1/traces" is appended.
	Endpoint string
	// SampleRate is the head sampling probability for root spans.
	// Must be greater than 0 and at most 1; disable tracing instead of
	// sampling nothing.
	SampleRate float64
	// ServiceName is reported as the service.name resource attribute.
	ServiceName string
	// ServiceVersion is reported as the service.version resource attribute.
	ServiceVersion string
	// ExportTimeout bounds a single export request to the collector.
	ExportTimeout time.Duration
}

// DefaultTracingConfig returns the default tracing configuration.
// Enabled is set explicitly to false so the default is stated in code rather
// than inherited from the zero value.
func DefaultTracingConfig() TracingConfig {
	return TracingConfig{
		Enabled:        false,
		Endpoint:       defaultTracingEndpoint,
		SampleRate:     1.0,
		ServiceName:    tracerName,
		ServiceVersion: "dev",
		ExportTimeout:  10 * time.Second,
	}
}

// TracingConfigFromEnv creates a TracingConfig from environment variables.
// CLOUDPAM_TRACING_ENABLED: true/1 to enable (default: false)
// CLOUDPAM_TRACING_ENDPOINT: OTLP/HTTP collector base URL (default: http://localhost:4318)
// CLOUDPAM_TRACING_SAMPLE_RATE: sampling probability in (0, 1] (default: 1.0)
// APP_VERSION: reported as service.version (default: dev)
//
// A non-nil error reports a malformed value; the returned config still holds
// the default for that field so the caller can warn and carry on.
func TracingConfigFromEnv() (TracingConfig, error) {
	cfg := DefaultTracingConfig()

	if v := os.Getenv("CLOUDPAM_TRACING_ENABLED"); v != "" {
		cfg.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := strings.TrimSpace(os.Getenv("CLOUDPAM_TRACING_ENDPOINT")); v != "" {
		cfg.Endpoint = v
	}
	if v := strings.TrimSpace(os.Getenv("APP_VERSION")); v != "" {
		cfg.ServiceVersion = v
	}
	if v := strings.TrimSpace(os.Getenv("CLOUDPAM_TRACING_SAMPLE_RATE")); v != "" {
		rate, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return cfg, fmt.Errorf("invalid CLOUDPAM_TRACING_SAMPLE_RATE %q: want a number in (0, 1]", v)
		}
		cfg.SampleRate = rate
	}
	return cfg, nil
}

// Validate reports whether the config can start a tracer provider.
// A disabled config is always valid; nothing else is inspected.
func (c TracingConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		return errors.New("tracing endpoint is required; set CLOUDPAM_TRACING_ENDPOINT")
	}
	if _, err := otlpTracesURL(c.Endpoint); err != nil {
		return err
	}
	if c.SampleRate <= 0 || c.SampleRate > 1 {
		return fmt.Errorf("tracing sample rate must be greater than 0 and at most 1, got %v; set CLOUDPAM_TRACING_ENABLED=false to disable tracing", c.SampleRate)
	}
	return nil
}

// TracerProvider owns the tracing pipeline. A nil *TracerProvider means tracing
// is disabled: every method is a no-op and TracingMiddleware installs nothing.
type TracerProvider struct {
	provider   *sdktrace.TracerProvider
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

// NewTracerProvider builds a tracer provider from cfg and installs it as the
// OpenTelemetry global provider.
//
// It returns (nil, nil) when cfg.Enabled is false so the caller can keep the
// tracing plumbing out of the request path entirely rather than checking a flag
// per request. No network connection is made here: an unreachable collector
// surfaces later as a failed export, never as a startup failure.
func NewTracerProvider(cfg TracingConfig, logger Logger) (*TracerProvider, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	exporter, err := newOTLPHTTPExporter(cfg)
	if err != nil {
		return nil, err
	}

	tp := newTracerProvider(cfg, exporter)

	otel.SetTracerProvider(tp.provider)
	otel.SetTextMapPropagator(tp.propagator)
	if logger != nil {
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
			logger.Warn("tracing error", "error", err)
		}))
	}
	return tp, nil
}

// newTracerProvider wires a provider around an arbitrary exporter. Tests use it
// to capture spans without installing anything globally.
func newTracerProvider(cfg TracingConfig, exporter sdktrace.SpanExporter) *TracerProvider {
	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = tracerName
	}
	res := resource.NewSchemaless(
		attribute.String("service.name", serviceName),
		attribute.String("service.version", cfg.ServiceVersion),
	)

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
	)

	return &TracerProvider{
		provider: provider,
		tracer:   provider.Tracer(tracerName),
		propagator: propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	}
}

// Tracer returns the provider's tracer, or a no-op tracer when tracing is off.
func (t *TracerProvider) Tracer() trace.Tracer {
	if t == nil {
		return noop.NewTracerProvider().Tracer(tracerName)
	}
	return t.tracer
}

// ForceFlush exports any spans still queued. A nil receiver is a no-op.
func (t *TracerProvider) ForceFlush(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return t.provider.ForceFlush(ctx)
}

// Shutdown flushes queued spans and releases exporter resources.
// A nil receiver is a no-op, so callers never need a tracing-enabled branch.
func (t *TracerProvider) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
}

// TracingMiddleware returns an HTTP middleware that records a server span per
// request. A nil provider returns the identity middleware, so with tracing
// disabled no wrapper is installed and the request path is untouched.
func TracingMiddleware(t *TracerProvider) func(http.Handler) http.Handler {
	if t == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	tracer := t.tracer
	propagator := t.propagator

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isTracingExemptPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			route := normalizePath(r.URL.Path)

			ctx, span := tracer.Start(ctx, r.Method+" "+route,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.request.method", r.Method),
					attribute.String("http.route", route),
					attribute.String("url.path", r.URL.Path),
					attribute.String("server.address", r.Host),
					attribute.String("user_agent.original", r.UserAgent()),
				),
			)
			defer span.End()

			if sc := span.SpanContext(); sc.IsValid() {
				traceID := sc.TraceID().String()
				ctx = WithTraceContext(ctx, traceID, sc.SpanID().String())
				w.Header().Set("X-Trace-Id", traceID)
			}
			if reqID := RequestIDFromContext(ctx); reqID != "" {
				span.SetAttributes(attribute.String("request.id", reqID))
			}

			wrapped, recorder := wrapTracingResponseWriter(w)
			next.ServeHTTP(wrapped, r.WithContext(ctx))

			span.SetAttributes(attribute.Int("http.response.status_code", recorder.status))
			if recorder.status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(recorder.status))
			}
		})
	}
}

// isTracingExemptPath reports whether a path is excluded from tracing.
// Health, readiness and metrics scrapes are high frequency and carry no
// diagnostic value as traces.
func isTracingExemptPath(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/metrics":
		return true
	default:
		return false
	}
}

// tracingResponseWriter captures the response status code.
type tracingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *tracingResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (w *tracingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// wrapTracingResponseWriter wraps w so the status code can be observed while
// preserving the optional interfaces the wrapped writer actually implements.
// Handlers commonly feature-detect with w.(http.Flusher) — SSE streaming on
// /api/v1/ai/chat does — so the wrapper must implement Flusher exactly when the
// underlying writer does, and must not claim interfaces it cannot honour.
func wrapTracingResponseWriter(w http.ResponseWriter) (http.ResponseWriter, *tracingResponseWriter) {
	base := &tracingResponseWriter{ResponseWriter: w, status: http.StatusOK}

	flusher, hasFlusher := w.(http.Flusher)
	hijacker, hasHijacker := w.(http.Hijacker)
	readerFrom, hasReaderFrom := w.(io.ReaderFrom)

	switch {
	case hasFlusher && hasHijacker && hasReaderFrom:
		return struct {
			*tracingResponseWriter
			http.Flusher
			http.Hijacker
			io.ReaderFrom
		}{base, flusher, hijacker, readerFrom}, base
	case hasFlusher && hasHijacker:
		return struct {
			*tracingResponseWriter
			http.Flusher
			http.Hijacker
		}{base, flusher, hijacker}, base
	case hasFlusher && hasReaderFrom:
		return struct {
			*tracingResponseWriter
			http.Flusher
			io.ReaderFrom
		}{base, flusher, readerFrom}, base
	case hasHijacker && hasReaderFrom:
		return struct {
			*tracingResponseWriter
			http.Hijacker
			io.ReaderFrom
		}{base, hijacker, readerFrom}, base
	case hasFlusher:
		return struct {
			*tracingResponseWriter
			http.Flusher
		}{base, flusher}, base
	case hasHijacker:
		return struct {
			*tracingResponseWriter
			http.Hijacker
		}{base, hijacker}, base
	case hasReaderFrom:
		return struct {
			*tracingResponseWriter
			io.ReaderFrom
		}{base, readerFrom}, base
	default:
		return base, base
	}
}

// traceIDs carries the identifiers of the active span so log records can be
// correlated with traces. It is only ever placed in the context by
// TracingMiddleware, which is not installed when tracing is disabled.
type traceIDs struct {
	traceID string
	spanID  string
}

// WithTraceContext stores trace and span identifiers in the context.
func WithTraceContext(ctx context.Context, traceID, spanID string) context.Context {
	if traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, traceContextKey, traceIDs{traceID: traceID, spanID: spanID})
}

// TraceIDFromContext returns the active trace ID, or "" when untraced.
func TraceIDFromContext(ctx context.Context) string {
	return traceIDsFromContext(ctx).traceID
}

// SpanIDFromContext returns the active span ID, or "" when untraced.
func SpanIDFromContext(ctx context.Context) string {
	return traceIDsFromContext(ctx).spanID
}

func traceIDsFromContext(ctx context.Context) traceIDs {
	if ctx == nil {
		return traceIDs{}
	}
	if v, ok := ctx.Value(traceContextKey).(traceIDs); ok {
		return v
	}
	return traceIDs{}
}

// globalTracer is resolved once at package init. The OpenTelemetry global
// provider hands back a delegating tracer that starts as a no-op and is
// upgraded in place when NewTracerProvider installs a real provider, so callers
// can hold on to it without paying for a global lookup per span.
var globalTracer = otel.Tracer(tracerName)

// Tracer returns the shared CloudPAM tracer for instrumenting outbound calls.
// With tracing disabled it is the OpenTelemetry no-op tracer, so Start costs an
// interface call and no allocation.
func Tracer() trace.Tracer { return globalTracer }

// otlpTracesURL normalises a configured endpoint into a full OTLP/HTTP traces
// URL. A bare host:port defaults to http, and an endpoint without a path gets
// the conventional /v1/traces suffix.
func otlpTracesURL(endpoint string) (string, error) {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return "", errors.New("tracing endpoint is required; set CLOUDPAM_TRACING_ENDPOINT")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid tracing endpoint %q: %w", endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid tracing endpoint %q: scheme must be http or https", endpoint)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid tracing endpoint %q: missing host", endpoint)
	}
	if strings.TrimSuffix(u.Path, "/") == "" {
		u.Path = "/v1/traces"
	}
	return u.String(), nil
}
