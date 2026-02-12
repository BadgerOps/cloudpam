//go:build postgres

package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	pgmigrations "cloudpam/migrations/postgres"
)

var migFileRe = regexp.MustCompile(`^(\d+)_.+\.up\.sql$`)

func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Create migration tracking tables
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_info (id INTEGER PRIMARY KEY CHECK(id=1), schema_version INTEGER NOT NULL, min_supported_schema INTEGER NOT NULL DEFAULT 1, app_version TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return fmt.Errorf("create schema_info: %w", err)
	}

	// Discover migration files from embedded FS
	useFS := fs.FS(pgmigrations.Files)

	entries, err := fs.ReadDir(useFS, ".")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	type mig struct {
		version int
		name    string
		path    string
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

	// Determine which migrations are already applied
	applied := map[int]bool{}
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
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

	// Apply pending migrations
	latest := 0
	for _, f := range files {
		if applied[f.version] {
			if f.version > latest {
				latest = f.version
			}
			continue
		}

		sqlBytes, err := fs.ReadFile(useFS, f.path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f.name, err)
		}
		stmt := strings.TrimSpace(string(sqlBytes))
		if stmt == "" {
			continue
		}

		// Execute migration in a transaction
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for migration %s: %w", f.name, err)
		}
		if _, err := tx.Exec(ctx, stmt); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s failed: %w", f.name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES($1, $2, $3)`, f.version, f.name, time.Now().UTC()); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", f.name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", f.name, err)
		}
		if f.version > latest {
			latest = f.version
		}
	}

	// Update schema_info
	if latest == 0 {
		var max int
		_ = pool.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&max)
		latest = max
	}
	appVersion := os.Getenv("APP_VERSION")
	if appVersion == "" {
		appVersion = "dev"
	}
	_, _ = pool.Exec(ctx, `
		INSERT INTO schema_info(id, schema_version, min_supported_schema, app_version, applied_at)
		VALUES(1, $1, COALESCE((SELECT min_supported_schema FROM schema_info WHERE id=1),1), $2, $3)
		ON CONFLICT(id) DO UPDATE SET schema_version=EXCLUDED.schema_version, app_version=EXCLUDED.app_version, applied_at=EXCLUDED.applied_at`,
		latest, appVersion, time.Now().UTC())

	return nil
}

// Status returns a summary of the migration state for the given connection string.
func Status(connStr string) (string, error) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return "", err
	}
	defer pool.Close()

	var latest int
	_ = pool.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&latest)

	var schemaVersion, minSupported int
	var appVersion string
	var appliedAt time.Time
	_ = pool.QueryRow(ctx, `SELECT schema_version, min_supported_schema, app_version, applied_at FROM schema_info WHERE id=1`).Scan(&schemaVersion, &minSupported, &appVersion, &appliedAt)

	var count int
	_ = pool.QueryRow(ctx, `SELECT COUNT(1) FROM schema_migrations`).Scan(&count)

	return fmt.Sprintf("schema_version=%d applied=%d latest=%d app_version=%s applied_at=%s min_supported=%d",
		schemaVersion, count, latest, appVersion, appliedAt.Format(time.RFC3339), minSupported), nil
}
