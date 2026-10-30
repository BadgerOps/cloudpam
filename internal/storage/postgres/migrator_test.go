//go:build postgres

package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	pgmigrations "cloudpam/migrations/postgres"
)

// freshDatabase creates an empty database on the test server, so every migration is
// pending, and returns its connection string. The database is dropped at cleanup.
func freshDatabase(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("cloudpam_mig_%d", time.Now().UnixNano())
	if _, err := testDB.pool.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testDB.pool.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})
	u, err := url.Parse(testDB.connStr)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

func embeddedMigrationCount(t *testing.T) int {
	t.Helper()
	entries, err := fs.ReadDir(pgmigrations.Files, ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	n := 0
	for _, e := range entries {
		if migFileRe.MatchString(e.Name()) {
			n++
		}
	}
	return n
}

// TestNewConcurrentReplicasMigrateOnce opens several stores against one fresh database at
// the same time, as replicas of a rollout do. Every open must succeed and the schema must
// end up migrated exactly once.
func TestNewConcurrentReplicasMigrateOnce(t *testing.T) {
	connStr := freshDatabase(t)
	const replicas = 4

	start := make(chan struct{})
	errs := make(chan error, replicas)
	for i := 0; i < replicas; i++ {
		go func() {
			<-start
			st, err := New(connStr)
			if err == nil {
				_ = st.Close()
			}
			errs <- err
		}()
	}
	close(start)
	for i := 0; i < replicas; i++ {
		if err := <-errs; err != nil {
			t.Errorf("replica open: %v", err)
		}
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	var applied int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if want := embeddedMigrationCount(t); applied != want {
		t.Errorf("schema_migrations has %d rows, want %d (one per migration)", applied, want)
	}
}
