//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestNew_AppliesPerConnectionPragmasToEveryConnection(t *testing.T) {
	t.Setenv("SQLITE_MAX_OPEN_CONNS", "2")
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db") + "?_fk=1&cache=shared"
	s, err := New(dsn)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	first, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatalf("first conn: %v", err)
	}
	defer first.Close()
	second, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatalf("second conn: %v", err)
	}
	defer second.Close()

	for name, c := range map[string]*sql.Conn{"first": first, "second": second} {
		var fk, busy int
		if err := c.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			t.Fatalf("%s: read foreign_keys: %v", name, err)
		}
		if err := c.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busy); err != nil {
			t.Fatalf("%s: read busy_timeout: %v", name, err)
		}
		if fk != 1 || busy != 5000 {
			t.Errorf("%s connection: foreign_keys=%d busy_timeout=%d, want 1 and 5000", name, fk, busy)
		}
	}
}
