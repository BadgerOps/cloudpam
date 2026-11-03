//go:build sqlite

package sqlitedb

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpen_EveryPooledConnectionCarriesTheSettings(t *testing.T) {
	db, err := Open("file:" + filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(2)

	ctx := context.Background()
	first, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("first conn: %v", err)
	}
	defer first.Close()
	second, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("second conn: %v", err)
	}
	defer second.Close()

	for name, c := range map[string]*sql.Conn{"first": first, "second": second} {
		var fk, busy int
		var journal string
		if err := c.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			t.Fatalf("%s: read foreign_keys: %v", name, err)
		}
		if err := c.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busy); err != nil {
			t.Fatalf("%s: read busy_timeout: %v", name, err)
		}
		if err := c.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal); err != nil {
			t.Fatalf("%s: read journal_mode: %v", name, err)
		}
		if fk != 1 || busy != 5000 || journal != "wal" {
			t.Errorf("%s connection: foreign_keys=%d busy_timeout=%d journal_mode=%q, want 1, 5000, wal", name, fk, busy, journal)
		}
	}
}
