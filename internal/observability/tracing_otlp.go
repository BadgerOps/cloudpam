package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// otlpHTTPExporter exports spans with OTLP over HTTP using the JSON encoding.
//
// The official otlptracehttp exporter links protobuf, gRPC and grpc-gateway,
// which adds tens of megabytes to a binary that ships to users for a feature
// that is off by default. OTLP/HTTP+JSON is part of the OTLP specification and
// is accepted by the OpenTelemetry Collector on /v1/traces, so a small
// hand-rolled encoder keeps the dependency set to the OTel API and SDK.
type otlpHTTPExporter struct {
	url     string
	client  *http.Client
	stopped atomic.Bool
}

// newOTLPHTTPExporter builds an exporter for cfg. It performs no I/O: the
// collector is only contacted when spans are actually exported, so a collector
// that is down or misconfigured cannot prevent the server from starting.
func newOTLPHTTPExporter(cfg TracingConfig) (*otlpHTTPExporter, error) {
	endpoint, err := otlpTracesURL(cfg.Endpoint)
	if err != nil {
		return nil, err
	}

	timeout := cfg.ExportTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	return &otlpHTTPExporter{
		url:    endpoint,
		client: &http.Client{Timeout: timeout},
	}, nil
}

// ExportSpans implements sdktrace.SpanExporter.
func (e *otlpHTTPExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if e.stopped.Load() || len(spans) == 0 {
		return nil
	}

	body, err := json.Marshal(buildOTLPPayload(spans))
	if err != nil {
		return fmt.Errorf("encode spans: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build export request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("export spans to %s: %w", e.url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain a bounded amount so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("export spans to %s: collector returned status %d", e.url, resp.StatusCode)
	}
	return nil
}

// Shutdown implements sdktrace.SpanExporter.
func (e *otlpHTTPExporter) Shutdown(context.Context) error {
	e.stopped.Store(true)
	e.client.CloseIdleConnections()
	return nil
}

// OTLP/JSON wire types. Field names follow the protobuf JSON mapping: 64-bit
// integers are encoded as strings, and trace and span IDs as hex.
type otlpPayload struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpKeyValue `json:"attributes,omitempty"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type otlpSpan struct {
	TraceID           string         `json:"traceId"`
	SpanID            string         `json:"spanId"`
	ParentSpanID      string         `json:"parentSpanId,omitempty"`
	Name              string         `json:"name"`
	Kind              int            `json:"kind"`
	StartTimeUnixNano string         `json:"startTimeUnixNano"`
	EndTimeUnixNano   string         `json:"endTimeUnixNano"`
	Attributes        []otlpKeyValue `json:"attributes,omitempty"`
	Events            []otlpEvent    `json:"events,omitempty"`
	Status            otlpStatus     `json:"status"`
}

type otlpEvent struct {
	TimeUnixNano string         `json:"timeUnixNano"`
	Name         string         `json:"name"`
	Attributes   []otlpKeyValue `json:"attributes,omitempty"`
}

type otlpStatus struct {
	Message string `json:"message,omitempty"`
	Code    int    `json:"code,omitempty"`
}

type otlpKeyValue struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	StringValue *string         `json:"stringValue,omitempty"`
	BoolValue   *bool           `json:"boolValue,omitempty"`
	IntValue    *string         `json:"intValue,omitempty"`
	DoubleValue *float64        `json:"doubleValue,omitempty"`
	ArrayValue  *otlpArrayValue `json:"arrayValue,omitempty"`
}

type otlpArrayValue struct {
	Values []otlpValue `json:"values"`
}

// buildOTLPPayload converts SDK spans into an OTLP ExportTraceServiceRequest.
// All spans from one export share a provider and therefore a resource, so they
// are grouped under a single resourceSpans entry, split by instrumentation
// scope.
func buildOTLPPayload(spans []sdktrace.ReadOnlySpan) otlpPayload {
	if len(spans) == 0 {
		return otlpPayload{}
	}

	var (
		order  []otlpScope
		byName = make(map[otlpScope][]otlpSpan)
	)
	for _, span := range spans {
		scope := otlpScope{
			Name:    span.InstrumentationScope().Name,
			Version: span.InstrumentationScope().Version,
		}
		if _, seen := byName[scope]; !seen {
			order = append(order, scope)
		}
		byName[scope] = append(byName[scope], convertSpan(span))
	}

	scopeSpans := make([]otlpScopeSpans, 0, len(order))
	for _, scope := range order {
		scopeSpans = append(scopeSpans, otlpScopeSpans{Scope: scope, Spans: byName[scope]})
	}

	var resAttrs []otlpKeyValue
	if res := spans[0].Resource(); res != nil {
		resAttrs = convertAttributes(res.Attributes())
	}

	return otlpPayload{
		ResourceSpans: []otlpResourceSpans{{
			Resource:   otlpResource{Attributes: resAttrs},
			ScopeSpans: scopeSpans,
		}},
	}
}

func convertSpan(span sdktrace.ReadOnlySpan) otlpSpan {
	sc := span.SpanContext()
	out := otlpSpan{
		TraceID:           sc.TraceID().String(),
		SpanID:            sc.SpanID().String(),
		Name:              span.Name(),
		Kind:              int(span.SpanKind()),
		StartTimeUnixNano: unixNano(span.StartTime()),
		EndTimeUnixNano:   unixNano(span.EndTime()),
		Attributes:        convertAttributes(span.Attributes()),
		Status: otlpStatus{
			Code:    otlpStatusCode(span.Status().Code),
			Message: span.Status().Description,
		},
	}
	if parent := span.Parent(); parent.HasSpanID() {
		out.ParentSpanID = parent.SpanID().String()
	}
	for _, event := range span.Events() {
		out.Events = append(out.Events, otlpEvent{
			TimeUnixNano: unixNano(event.Time),
			Name:         event.Name,
			Attributes:   convertAttributes(event.Attributes),
		})
	}
	return out
}

func unixNano(t time.Time) string {
	return strconv.FormatInt(t.UnixNano(), 10)
}

// otlpStatusCode maps OTel status codes onto the OTLP enum, which orders OK and
// ERROR differently: OTLP is UNSET=0, OK=1, ERROR=2.
func otlpStatusCode(code codes.Code) int {
	switch code {
	case codes.Ok:
		return 1
	case codes.Error:
		return 2
	default:
		return 0
	}
}

func convertAttributes(attrs []attribute.KeyValue) []otlpKeyValue {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]otlpKeyValue, 0, len(attrs))
	for _, attr := range attrs {
		out = append(out, otlpKeyValue{
			Key:   string(attr.Key),
			Value: convertAttributeValue(attr.Value),
		})
	}
	return out
}

func convertAttributeValue(v attribute.Value) otlpValue {
	switch v.Type() {
	case attribute.BOOL:
		b := v.AsBool()
		return otlpValue{BoolValue: &b}
	case attribute.INT64:
		i := strconv.FormatInt(v.AsInt64(), 10)
		return otlpValue{IntValue: &i}
	case attribute.FLOAT64:
		f := v.AsFloat64()
		return otlpValue{DoubleValue: &f}
	case attribute.STRING:
		s := v.AsString()
		return otlpValue{StringValue: &s}
	case attribute.BOOLSLICE:
		items := v.AsBoolSlice()
		values := make([]otlpValue, 0, len(items))
		for i := range items {
			values = append(values, otlpValue{BoolValue: &items[i]})
		}
		return otlpValue{ArrayValue: &otlpArrayValue{Values: values}}
	case attribute.INT64SLICE:
		items := v.AsInt64Slice()
		values := make([]otlpValue, 0, len(items))
		for _, item := range items {
			s := strconv.FormatInt(item, 10)
			values = append(values, otlpValue{IntValue: &s})
		}
		return otlpValue{ArrayValue: &otlpArrayValue{Values: values}}
	case attribute.FLOAT64SLICE:
		items := v.AsFloat64Slice()
		values := make([]otlpValue, 0, len(items))
		for i := range items {
			values = append(values, otlpValue{DoubleValue: &items[i]})
		}
		return otlpValue{ArrayValue: &otlpArrayValue{Values: values}}
	case attribute.STRINGSLICE:
		items := v.AsStringSlice()
		values := make([]otlpValue, 0, len(items))
		for i := range items {
			values = append(values, otlpValue{StringValue: &items[i]})
		}
		return otlpValue{ArrayValue: &otlpArrayValue{Values: values}}
	default:
		s := v.String()
		return otlpValue{StringValue: &s}
	}
}
