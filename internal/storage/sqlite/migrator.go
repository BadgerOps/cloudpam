//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	migfs "cloudpam/migrations"
)

var migFileRe = regexp.MustCompile(`^(\d+)_.+\.sql$`)

func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_info (id INTEGER PRIMARY KEY CHECK(id=1), schema_version INTEGER NOT NULL, min_supported_schema INTEGER NOT NULL DEFAULT 1, app_version TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	// Always the migrations compiled into the binary. Reading a migrations/
	// directory found from the working directory let a binary run inside an
	// older or unrelated checkout apply that checkout's migrations instead.
	entries, err := fs.ReadDir(migfs.Files, ".")
	if err != nil {
		return err
	}
	type mig struct {
		version    int
		name, path string
	}
	var files []mig
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		m := migFileRe.FindStringSubmatch(name)
		if len(m) == 0 {
			continue
		}
		v := 0
		fmt.Sscanf(m[1], "%d", &v)
		files = append(files, mig{version: v, name: name, path: name})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	applied := map[int]bool{}
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	latest := 0
	for _, f := range files {
		if applied[f.version] {
			continue
		}
		sqlBytes, err := fs.ReadFile(migfs.Files, f.path)
		if err != nil {
			return err
		}
		stmt := strings.TrimSpace(string(sqlBytes))
		if stmt == "" {
			continue
		}
		if err := applyMigration(db, f.version, f.name, stmt); err != nil {
			return err
		}
		if f.version > latest {
			latest = f.version
		}
	}
	// Update schema_info
	if latest == 0 {
		// compute current by reading max(version)
		_ = db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&latest)
	}
	appVersion := os.Getenv("APP_VERSION")
	if appVersion == "" {
		appVersion = "dev"
	}
	// upsert id=1
	_, _ = db.Exec(`INSERT INTO schema_info(id, schema_version, min_supported_schema, app_version, applied_at)
                    VALUES(1, ?, COALESCE((SELECT min_supported_schema FROM schema_info WHERE id=1),1), ?, ?)
                    ON CONFLICT(id) DO UPDATE SET schema_version=excluded.schema_version, app_version=excluded.app_version, applied_at=excluded.applied_at`,
		latest, appVersion, time.Now().UTC().Format(time.RFC3339))
	return nil
}

// applyMigration runs one migration in a transaction with foreign keys off, the
// way SQLite's table-rebuild procedure requires: with them on, DROP TABLE on a
// parent table fails on RESTRICT children and fires ON DELETE CASCADE. The pragma
// is a no-op inside a transaction and is per connection, so the migration runs on
// one pinned connection and the prior setting is restored before it is released.
func applyMigration(db *sql.DB, version int, name, stmt string) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	var fk int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA foreign_keys=%d`, fk)) }()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(stmt); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migration %s failed: %w", name, err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, name, applied_at) VALUES(?, ?, ?)`, version, name, time.Now().UTC().Format(time.RFC3339)); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
