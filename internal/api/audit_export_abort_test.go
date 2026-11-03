package api

import (
	"context"
	"encoding/csv"
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
	if opts.After != nil {
		return nil, 0, errors.New("backend went away")
	}
	return l.AuditLogger.List(ctx, opts)
}

// purgeBetweenPagesAuditLogger serves the first export page, then purges the
// oldest events the way a retention pass landing mid-export would.
type purgeBetweenPagesAuditLogger struct {
	*audit.MemoryAuditLogger
	cutoff time.Time
}

func (l purgeBetweenPagesAuditLogger) List(ctx context.Context, opts audit.ListOptions) ([]*audit.AuditEvent, int, error) {
	events, total, err := l.MemoryAuditLogger.List(ctx, opts)
	if err == nil && opts.After == nil {
		if _, err := l.DeleteOlderThan(ctx, l.cutoff); err != nil {
			return nil, 0, err
		}
	}
	return events, total, err
}

// insertBetweenPagesAuditLogger serves the first export page, then commits
// an event inside the export window, the way a slow insert would.
type insertBetweenPagesAuditLogger struct {
	*audit.MemoryAuditLogger
	late *audit.AuditEvent
}

func (l insertBetweenPagesAuditLogger) List(ctx context.Context, opts audit.ListOptions) ([]*audit.AuditEvent, int, error) {
	events, total, err := l.MemoryAuditLogger.List(ctx, opts)
	if err == nil && opts.After == nil {
		if err := l.Log(ctx, l.late); err != nil {
			return nil, 0, err
		}
	}
	return events, total, err
}

// multiPageMemoryAuditLogger holds a few more events than one export page.
func multiPageMemoryAuditLogger(t *testing.T) (*audit.MemoryAuditLogger, time.Time) {
	t.Helper()
	mem := audit.NewMemoryAuditLogger(audit.WithMaxEvents(5000))
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	for i := range auditExportPageSize + 5 {
		logAuditEvents(t, mem, &audit.AuditEvent{ID: fmt.Sprintf("evt-%04d", i), Timestamp: base.Add(time.Duration(i) * time.Second), Action: audit.ActionCreate, ResourceType: audit.ResourcePool, ResourceID: "1"})
	}
	return mem, base
}

// exportCSVRecovering runs a CSV export through the logging middleware and
// returns the recorded body with whatever the handler panicked.
func exportCSVRecovering(t *testing.T, auditLogger audit.AuditLogger) (*httptest.ResponseRecorder, any) {
	t.Helper()
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
	return rr, recovered
}

func TestAuditExportAbortsTheConnectionWhenALaterPageFails(t *testing.T) {
	mem, _ := multiPageMemoryAuditLogger(t)
	rr, recovered := exportCSVRecovering(t, secondPageFailsAuditLogger{mem})

	if recovered != stdhttp.ErrAbortHandler {
		t.Fatalf("recovered %v, want http.ErrAbortHandler so net/http closes the connection and the download fails instead of completing short", recovered)
	}
	if strings.Contains(rr.Body.String(), "internal server error") {
		t.Fatalf("a JSON error was appended to the CSV stream:\n%s", rr.Body.String()[len(rr.Body.String())-200:])
	}
}

func TestAuditExportAbortsWhenARetentionPurgeShrinksTheLogMidExport(t *testing.T) {
	mem, base := multiPageMemoryAuditLogger(t)
	_, recovered := exportCSVRecovering(t, purgeBetweenPagesAuditLogger{MemoryAuditLogger: mem, cutoff: base.Add(10 * time.Second)})

	if recovered != stdhttp.ErrAbortHandler {
		t.Fatalf("recovered %v, want http.ErrAbortHandler so a download that lost rows to the purge fails instead of completing short", recovered)
	}
}

func TestAuditExportNeitherRepeatsNorDropsRowsWhenAnEventCommitsBetweenPages(t *testing.T) {
	mem, base := multiPageMemoryAuditLogger(t)
	late := &audit.AuditEvent{ID: "evt-late", Timestamp: base.Add(-time.Hour), Action: audit.ActionCreate, ResourceType: audit.ResourcePool, ResourceID: "1"}
	rr, recovered := exportCSVRecovering(t, insertBetweenPagesAuditLogger{MemoryAuditLogger: mem, late: late})
	if recovered != nil {
		t.Fatalf("export panicked: %v", recovered)
	}

	records, err := csv.NewReader(strings.NewReader(rr.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	seen := map[string]bool{}
	for _, rec := range records[1:] {
		if seen[rec[0]] {
			t.Fatalf("event %s exported twice", rec[0])
		}
		seen[rec[0]] = true
	}
	for i := range auditExportPageSize + 5 {
		if id := fmt.Sprintf("evt-%04d", i); !seen[id] {
			t.Fatalf("event %s logged before the export is missing from it", id)
		}
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
