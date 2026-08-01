//go:build sqlite

package audit

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

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
