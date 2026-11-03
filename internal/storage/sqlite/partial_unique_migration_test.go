//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"cloudpam/internal/domain"
	migfs "cloudpam/migrations"
)

// openAtVersion builds a database with every embedded migration up to and
// including maxVersion applied and recorded, as an older release left it.
func openAtVersion(t *testing.T, dsn string, maxVersion int) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("init: %v", err)
	}
	entries, err := fs.ReadDir(migfs.Files, ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		m := migFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, err := strconv.Atoi(m[1])
		if err != nil || v > maxVersion {
			continue
		}
		b, err := migfs.Files.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if _, err := db.Exec(string(b)); err != nil {
			t.Fatalf("apply %s: %v", e.Name(), err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (?, ?, ?)`, v, e.Name(), time.Now().UTC().Format(time.RFC3339)); err != nil {
			t.Fatalf("record %s: %v", e.Name(), err)
		}
	}
	return db
}

func TestPartialUniqueMigrationKeepsChildRows(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "upgrade.db") + "?cache=shared"
	old := openAtVersion(t, dsn, 22)
	seed := []string{
		`INSERT INTO accounts (id, key, name, deleted_at) VALUES (1, 'aws:gone', 'gone', '2026-01-01T00:00:00Z')`,
		`INSERT INTO accounts (id, key, name) VALUES (2, 'aws:live', 'live')`,
		`INSERT INTO accounts (id, key, name) VALUES (3, 'aws:creds', 'creds')`,
		`INSERT INTO accounts (id, key, name) VALUES (9, 'aws:hard', 'hard')`,
		`DELETE FROM accounts WHERE id = 9`,
		`INSERT INTO pools (id, name, cidr, account_id, deleted_at) VALUES (1, 'gone', '10.0.0.0/16', 2, '2026-01-01T00:00:00Z')`,
		`INSERT INTO pools (id, name, cidr, account_id) VALUES (2, 'live', '10.1.0.0/16', 2)`,
		`INSERT INTO sync_jobs (id, account_id, created_at) VALUES ('job-1', 2, '2026-01-01T00:00:00Z')`,
		`INSERT INTO account_credentials (account_id, type, payload_encrypted, created_at, updated_at) VALUES (2, 'aws_assume_role', 'p2', 'now', 'now')`,
		`INSERT INTO account_credentials (account_id, type, payload_encrypted, created_at, updated_at) VALUES (3, 'aws_assume_role', 'p3', 'now', 'now')`,
	}
	for _, q := range seed {
		if _, err := old.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s, err := New(dsn)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()

	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	if n := count(`SELECT COUNT(1) FROM accounts`); n != 3 {
		t.Fatalf("accounts after upgrade = %d, want 3", n)
	}
	if n := count(`SELECT COUNT(1) FROM account_credentials`); n != 2 {
		t.Fatalf("account_credentials after upgrade = %d, want 2 (DROP TABLE cascaded)", n)
	}
	if n := count(`SELECT COUNT(1) FROM sync_jobs WHERE account_id = 2`); n != 1 {
		t.Fatalf("sync_jobs after upgrade = %d, want 1", n)
	}
	if n := count(`SELECT COUNT(1) FROM pragma_foreign_key_check`); n != 0 {
		t.Fatalf("foreign_key_check after upgrade reported %d violations", n)
	}
	var fk int
	if err := s.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys after migrations = %d, %v; want 1", fk, err)
	}

	reused, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "aws:gone", Name: "again"})
	if err != nil {
		t.Fatalf("CreateAccount(soft-deleted key) after upgrade: %v", err)
	}
	if reused.ID != 10 {
		t.Fatalf("new account id = %d, want 10 (AUTOINCREMENT high-water mark lost)", reused.ID)
	}
	if _, err := s.CreatePool(ctx, domain.CreatePool{Name: "again", CIDR: "10.0.0.0/16"}); err != nil {
		t.Fatalf("CreatePool(soft-deleted cidr) after upgrade: %v", err)
	}

	if ok, err := s.DeleteAccount(ctx, 3); err != nil || !ok {
		t.Fatalf("DeleteAccount(3) = %v, %v", ok, err)
	}
	if n := count(`SELECT COUNT(1) FROM account_credentials WHERE account_id = 3`); n != 0 {
		t.Fatalf("credential survived account soft delete; trigger missing after rebuild")
	}
}
