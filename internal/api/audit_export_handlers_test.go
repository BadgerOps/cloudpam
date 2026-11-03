package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

// setupAuditExportServer registers the audit routes over a memory logger
// with no auth middleware so the tests exercise the handler alone.
func setupAuditExportServer(t *testing.T) (*stdhttp.ServeMux, *audit.MemoryAuditLogger) {
	t.Helper()
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	auditLogger := audit.NewMemoryAuditLogger(audit.WithMaxEvents(5000))
	srv := NewServer(mux, storage.NewMemoryStore(), logger, nil, auditLogger)
	as := NewAuthServer(srv, auth.NewMemoryKeyStore(), auditLogger)
	as.handleOpenAPIRouteFunc("/api/v1/audit/export", as.handleAuditExport)
	as.handleOpenAPIRouteFunc("/api/v1/audit", as.handleAuditList)
	return mux, auditLogger
}

func logAuditEvents(t *testing.T, logger *audit.MemoryAuditLogger, events ...*audit.AuditEvent) {
	t.Helper()
	for _, e := range events {
		if err := logger.Log(context.Background(), e); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}
}

func getExport(t *testing.T, mux *stdhttp.ServeMux, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/audit/export"+query, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestAuditExportCSVEscapesFormulaCells(t *testing.T) {
	mux, logger := setupAuditExportServer(t)
	ts := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	logAuditEvents(t, logger, &audit.AuditEvent{
		ID: "evt-1", Timestamp: ts, Actor: "=HYPERLINK(\"http://evil.test\")", ActorType: audit.ActorTypeUser,
		Action: audit.ActionUpdate, ResourceType: audit.ResourcePool, ResourceID: "7", ResourceName: "-prod, east",
		RequestID: "@req", IPAddress: "10.0.0.1", StatusCode: 200,
		Changes: &audit.Changes{Before: map[string]any{"name": "a"}, After: map[string]any{"name": "b"}},
	})

	rr := getExport(t, mux, "?format=csv")
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment; filename=cloudpam-audit-") || !strings.HasSuffix(cd, ".csv") {
		t.Errorf("Content-Disposition = %q", cd)
	}

	rows, err := csv.NewReader(strings.NewReader(rr.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v\n%s", err, rr.Body.String())
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want header + 1", len(rows))
	}
	if strings.Join(rows[0], ",") != strings.Join(auditCSVHeader, ",") {
		t.Errorf("header = %v", rows[0])
	}
	row := rows[1]
	want := map[string]string{
		"id":            "evt-1",
		"timestamp":     "2026-02-01T10:00:00Z",
		"actor":         "'=HYPERLINK(\"http://evil.test\")",
		"resource_name": "'-prod, east",
		"request_id":    "'@req",
		"ip_address":    "10.0.0.1",
		"status_code":   "200",
		"changes":       `{"before":{"name":"a"},"after":{"name":"b"}}`,
	}
	for i, col := range auditCSVHeader {
		if exp, ok := want[col]; ok && row[i] != exp {
			t.Errorf("%s = %q, want %q", col, row[i], exp)
		}
	}
}

func TestAuditExportJSONHonoursFilters(t *testing.T) {
	mux, logger := setupAuditExportServer(t)
	ts := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	logAuditEvents(t, logger,
		&audit.AuditEvent{ID: "pool-create", Timestamp: ts, Action: audit.ActionCreate, ResourceType: audit.ResourcePool, ResourceID: "1"},
		&audit.AuditEvent{ID: "pool-delete", Timestamp: ts.Add(time.Minute), Action: audit.ActionDelete, ResourceType: audit.ResourcePool, ResourceID: "1"},
		&audit.AuditEvent{ID: "account-create", Timestamp: ts.Add(2 * time.Minute), Action: audit.ActionCreate, ResourceType: audit.ResourceAccount, ResourceID: "2"},
	)

	rr := getExport(t, mux, "?format=json&action=create")
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.HasSuffix(cd, ".json") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	var body struct {
		Events []audit.AuditEvent `json:"events"`
		Total  int                `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, rr.Body.String())
	}
	if body.Total != 2 || len(body.Events) != 2 {
		t.Fatalf("total = %d, events = %d, want 2 creates", body.Total, len(body.Events))
	}
	if body.Events[0].ID != "account-create" || body.Events[1].ID != "pool-create" {
		t.Errorf("events = %s, %s; want newest first", body.Events[0].ID, body.Events[1].ID)
	}

	rr = getExport(t, mux, "?format=json&resource_type=account&since="+url.QueryEscape(ts.Add(time.Minute).Format(time.RFC3339)))
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Total != 1 || body.Events[0].ID != "account-create" {
		t.Errorf("resource_type+since filter returned %+v", body.Events)
	}
}

func TestAuditExportWalksEveryPage(t *testing.T) {
	mux, logger := setupAuditExportServer(t)
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	for i := range auditExportPageSize + 5 {
		logAuditEvents(t, logger, &audit.AuditEvent{ID: fmt.Sprintf("evt-%04d", i), Timestamp: base.Add(time.Duration(i) * time.Second), Action: audit.ActionCreate, ResourceType: audit.ResourcePool, ResourceID: "1"})
	}

	rr := getExport(t, mux, "?format=csv")
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	rows, err := csv.NewReader(strings.NewReader(rr.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if got := len(rows) - 1; got != auditExportPageSize+5 {
		t.Fatalf("exported %d rows, want %d", got, auditExportPageSize+5)
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows[1:] {
		if seen[row[0]] {
			t.Fatalf("row %s exported twice", row[0])
		}
		seen[row[0]] = true
	}
}

func TestAuditExportDefaultsToCSVAndRejectsUnknownFormats(t *testing.T) {
	mux, _ := setupAuditExportServer(t)

	if rr := getExport(t, mux, ""); rr.Code != stdhttp.StatusOK || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/csv") {
		t.Errorf("no format: status %d, Content-Type %q; want csv", rr.Code, rr.Header().Get("Content-Type"))
	}
	rr := getExport(t, mux, "?format=xml")
	if rr.Code != stdhttp.StatusBadRequest {
		t.Fatalf("format=xml status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "invalid format") {
		t.Errorf("body = %s", rr.Body.String())
	}

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/audit/export", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
}

func TestAuditExportRequiresAuditRead(t *testing.T) {
	mux, _, adminKey := setupProtectedAuthTestServer(t)

	rr := doProtected(t, mux, stdhttp.MethodGet, "/api/v1/audit/export?format=json", "", adminKey)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("admin export status = %d: %s", rr.Code, rr.Body.String())
	}

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/audit/export", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusUnauthorized {
		t.Errorf("anonymous export status = %d, want 401", rec.Code)
	}
}

func TestAuditListAcceptsSinceAndUntilOnTheUnprotectedRoute(t *testing.T) {
	mux, logger := setupAuditExportServer(t)
	ts := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	logAuditEvents(t, logger,
		&audit.AuditEvent{ID: "old", Timestamp: ts, Action: audit.ActionCreate, ResourceType: audit.ResourcePool, ResourceID: "1"},
		&audit.AuditEvent{ID: "new", Timestamp: ts.Add(time.Hour), Action: audit.ActionCreate, ResourceType: audit.ResourcePool, ResourceID: "1"},
	)
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/audit?until="+url.QueryEscape(ts.Format(time.RFC3339)), nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	var body struct {
		Events []audit.AuditEvent `json:"events"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Events) != 1 || body.Events[0].ID != "old" {
		t.Errorf("until filter returned %+v", body.Events)
	}
}

func TestAuditExportIsInTheOpenAPISpec(t *testing.T) {
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, storage.NewMemoryStore(), logger, nil, nil)
	as := NewAuthServer(srv, auth.NewMemoryKeyStore(), srv.auditLogger)
	as.RegisterProtectedAuthRoutes(nil)

	spec := string(srv.openAPISpecYAML())
	for _, want := range []string{`"/api/v1/audit/export":`, `text/csv:`, `name: "format"`, `name: "until"`} {
		if !strings.Contains(spec, want) {
			t.Errorf("spec is missing %q", want)
		}
	}
}

func TestCSVSafeCell(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"plain":      "plain",
		"=1+1":       "'=1+1",
		"+1":         "'+1",
		"-1":         "'-1",
		"@cmd":       "'@cmd",
		"\tx":        "'\tx",
		"\rx":        "'\rx",
		"10.0.0.0/8": "10.0.0.0/8",
		"a=b":        "a=b",
	}
	for in, want := range cases {
		if got := csvSafeCell(in); got != want {
			t.Errorf("csvSafeCell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAuditExportClampsAFutureUntilToTheRequestTime(t *testing.T) {
	mux, logger := setupAuditExportServer(t)
	now := time.Now().UTC()
	logAuditEvents(t, logger,
		&audit.AuditEvent{ID: "past", Timestamp: now.Add(-time.Hour), Action: audit.ActionCreate, ResourceType: audit.ResourcePool, ResourceID: "1"},
		&audit.AuditEvent{ID: "future", Timestamp: now.Add(time.Hour), Action: audit.ActionCreate, ResourceType: audit.ResourcePool, ResourceID: "1"},
	)

	rr := getExport(t, mux, "?format=json&until="+url.QueryEscape(now.Add(2*time.Hour).Format(time.RFC3339)))
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Events []audit.AuditEvent `json:"events"`
		Total  int                `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse json: %v\n%s", err, rr.Body.String())
	}
	if body.Total != 1 || len(body.Events) != 1 || body.Events[0].ID != "past" {
		t.Errorf("a future until must not reach past the request time, got %+v", body.Events)
	}
}

// deadlineRecorder records the write deadline a handler sets through
// http.ResponseController.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadline = t
	return nil
}

func TestAuditExportExtendsTheWriteDeadlinePastTheServerTimeout(t *testing.T) {
	mux, logger := setupAuditExportServer(t)
	logAuditEvents(t, logger, &audit.AuditEvent{ID: "evt-1", Timestamp: time.Now().Add(-time.Minute), Action: audit.ActionCreate, ResourceType: audit.ResourcePool, ResourceID: "1"})

	start := time.Now()
	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	mux.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/audit/export?format=csv", nil))
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	// cmd/cloudpam/main.go gives every response 15s; a full export needs more.
	if !rec.deadline.After(start.Add(15 * time.Second)) {
		t.Errorf("write deadline = %v, want later than the server's 15s timeout from %v", rec.deadline, start)
	}
}
