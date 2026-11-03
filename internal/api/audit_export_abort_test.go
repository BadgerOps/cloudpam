package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

// secondPageFailsAuditLogger serves the first export page and fails every
// later one, the way a backend that drops mid-export would.
type secondPageFailsAuditLogger struct {
	audit.AuditLogger
}

func (l secondPageFailsAuditLogger) List(ctx context.Context, opts audit.ListOptions) ([]*audit.AuditEvent, int, error) {
	if opts.Offset > 0 {
		return nil, 0, errors.New("backend went away")
	}
	return l.AuditLogger.List(ctx, opts)
}

func TestAuditExportAbortsTheConnectionWhenALaterPageFails(t *testing.T) {
	mem := audit.NewMemoryAuditLogger(audit.WithMaxEvents(5000))
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	for i := range auditExportPageSize + 5 {
		logAuditEvents(t, mem, &audit.AuditEvent{ID: fmt.Sprintf("evt-%04d", i), Timestamp: base.Add(time.Duration(i) * time.Second), Action: audit.ActionCreate, ResourceType: audit.ResourcePool, ResourceID: "1"})
	}
	auditLogger := secondPageFailsAuditLogger{mem}
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, storage.NewMemoryStore(), logger, nil, auditLogger)
	as := NewAuthServer(srv, auth.NewMemoryKeyStore(), auditLogger)
	as.handleOpenAPIRouteFunc("/api/v1/audit/export", as.handleAuditExport)
	handler := LoggingMiddleware(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/audit/export?format=csv", nil)
	rr := httptest.NewRecorder()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(rr, req)
	}()

	if recovered != stdhttp.ErrAbortHandler {
		t.Fatalf("recovered %v, want http.ErrAbortHandler so net/http closes the connection and the download fails instead of completing short", recovered)
	}
	if strings.Contains(rr.Body.String(), "internal server error") {
		t.Fatalf("a JSON error was appended to the CSV stream:\n%s", rr.Body.String()[len(rr.Body.String())-200:])
	}
}

func TestLoggingMiddlewareLetsErrAbortHandlerThrough(t *testing.T) {
	handler := LoggingMiddleware(slog.New(slog.NewTextHandler(io.Discard, nil)))(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = io.WriteString(w, "partial")
		panic(stdhttp.ErrAbortHandler)
	}))
	rr := httptest.NewRecorder()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(rr, httptest.NewRequest(stdhttp.MethodGet, "/stream", nil))
	}()
	if recovered != stdhttp.ErrAbortHandler {
		t.Fatalf("recovered %v, want http.ErrAbortHandler to reach net/http", recovered)
	}
	if rr.Body.String() != "partial" {
		t.Fatalf("body = %q, want only what the handler wrote", rr.Body.String())
	}
}
