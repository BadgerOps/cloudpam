//go:build sqlite

package audit

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" // CGO-less SQLite driver
)

func newAuditTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := "file:" + filepath.Join(t.TempDir(), "audit.db") + "?_fk=1"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE audit_logs (
		id TEXT PRIMARY KEY,
		timestamp TEXT NOT NULL,
		actor TEXT,
		actor_type TEXT,
		action TEXT,
		resource_type TEXT,
		resource_id TEXT,
		resource_name TEXT,
		changes TEXT,
		request_id TEXT,
		ip_address TEXT,
		status_code INTEGER
	)`); err != nil {
		t.Fatalf("create audit_logs: %v", err)
	}
	return db
}

func TestSQLiteAuditLoggerCloseLeavesBorrowedDBOpen(t *testing.T) {
	ctx := context.Background()
	db := newAuditTestDB(t)

	logger := NewSQLiteAuditLoggerFromDB(db)
	if err := logger.Log(ctx, &AuditEvent{Actor: "tester", Action: "create"}); err != nil {
		t.Fatalf("Log: %v", err)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The caller still owns the handle, so it must remain usable.
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("borrowed db was closed by the audit logger: %v", err)
	}
	if err := logger.Log(ctx, &AuditEvent{Actor: "tester", Action: "update"}); err != nil {
		t.Fatalf("Log after Close on borrowed handle: %v", err)
	}
}

func TestSQLiteAuditLoggerCloseClosesOwnedDB(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "owned.db") + "?_fk=1"

	logger, err := NewSQLiteAuditLogger(dsn)
	if err != nil {
		t.Fatalf("NewSQLiteAuditLogger: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := logger.db.PingContext(ctx); err == nil {
		t.Fatal("expected the self-opened handle to be closed")
	}
}

// TestSQLiteGetByResourceSurvivesBusyResourceType reproduces the pre-fix
// failure: GetByResource fetched the newest 1000 rows of the resource type and
// only then matched on resource_id, so events for a quiet resource vanished
// behind a busier sibling of the same type.
func TestSQLiteGetByResourceSurvivesBusyResourceType(t *testing.T) {
	ctx := context.Background()
	db := newAuditTestDB(t)
	logger := NewSQLiteAuditLoggerFromDB(db)

	// The event we care about is the oldest of its type.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := logger.Log(ctx, &AuditEvent{
		Timestamp:    base,
		Actor:        "tester",
		Action:       "create",
		ResourceType: "pool",
		ResourceID:   "pool-of-interest",
	}); err != nil {
		t.Fatalf("Log target event: %v", err)
	}

	// Bury it under more than one page of same-type events for other resources.
	for i := range 1200 {
		if err := logger.Log(ctx, &AuditEvent{
			Timestamp:    base.Add(time.Duration(i+1) * time.Second),
			Actor:        "tester",
			Action:       "update",
			ResourceType: "pool",
			ResourceID:   "noisy-pool",
		}); err != nil {
			t.Fatalf("Log filler event %d: %v", i, err)
		}
	}

	events, err := logger.GetByResource(ctx, "pool", "pool-of-interest")
	if err != nil {
		t.Fatalf("GetByResource: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("GetByResource returned %d events, want 1 (the target was hidden behind the row limit)", len(events))
	}
	if events[0].ResourceID != "pool-of-interest" || events[0].Action != "create" {
		t.Errorf("unexpected event: %+v", events[0])
	}

	// The noisy resource is still capped at the page limit, not unbounded.
	noisy, err := logger.GetByResource(ctx, "pool", "noisy-pool")
	if err != nil {
		t.Fatalf("GetByResource(noisy): %v", err)
	}
	if len(noisy) != 1000 {
		t.Errorf("noisy resource returned %d events, want the 1000-row cap", len(noisy))
	}
}

// TestSQLiteListFiltersByResourceID checks the new ListOptions field is applied
// in SQL alongside the other filters.
func TestSQLiteListFiltersByResourceID(t *testing.T) {
	ctx := context.Background()
	db := newAuditTestDB(t)
	logger := NewSQLiteAuditLoggerFromDB(db)

	for _, id := range []string{"a", "b", "a"} {
		if err := logger.Log(ctx, &AuditEvent{
			Actor:        "tester",
			Action:       "update",
			ResourceType: "pool",
			ResourceID:   id,
		}); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	events, total, err := logger.List(ctx, ListOptions{ResourceType: "pool", ResourceID: "a"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	for _, e := range events {
		if e.ResourceID != "a" {
			t.Errorf("unexpected resource_id %q in filtered results", e.ResourceID)
		}
	}
}
